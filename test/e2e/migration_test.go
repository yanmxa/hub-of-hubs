package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	coordinationv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	operatorv1 "open-cluster-management.io/api/operator/v1"
	workv1 "open-cluster-management.io/api/work/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	migrationv1alpha1 "github.com/stolostron/multicluster-global-hub/operator/api/migration/v1alpha1"
)

const (
	migrationNamespace    = "multicluster-global-hub"
	migrationTimeout      = 10 * time.Minute
	migrationPollInterval = 5 * time.Second
)

var _ = Describe("Migration E2E", Label("e2e-test-migration"), Ordered, func() {
	var (
		sourceHubName        string
		targetHubName        string
		clusterToMigrate     string
		migrationName        string
		sourceHubClient      client.Client
		targetHubClient      client.Client
		managedClusterClient client.Client
	)

	BeforeAll(func() {
		// Use hub1 as source and hub2 as target
		Expect(len(managedHubNames)).To(BeNumerically(">=", 2))
		sourceHubName = managedHubNames[0]        // hub1
		targetHubName = managedHubNames[1]        // hub2
		clusterToMigrate = managedClusterNames[0] // hub1-cluster1
		migrationName = fmt.Sprintf("migrate-%s", clusterToMigrate)

		var err error
		sourceHubClient, err = testClients.RuntimeClient(sourceHubName, agentScheme)
		Expect(err).NotTo(HaveOccurred())
		targetHubClient, err = testClients.RuntimeClient(targetHubName, agentScheme)
		Expect(err).NotTo(HaveOccurred())
		// Get managed cluster client for verifying resources on the managed cluster
		managedClusterClient, err = testClients.RuntimeClient(clusterToMigrate, agentScheme)
		Expect(err).NotTo(HaveOccurred())

		By(fmt.Sprintf("Migrating %s from %s to %s", clusterToMigrate, sourceHubName, targetHubName))

		// Ensure managed cluster lease is fresh and status is available
		// In KinD e2e, there's no work-agent so we need to mock these
		By("Creating/updating managed cluster lease to keep it fresh")
		lease := &coordinationv1.Lease{}
		leaseName := types.NamespacedName{
			Name:      "managed-cluster-lease",
			Namespace: clusterToMigrate, // hub1-cluster1 namespace
		}
		err = sourceHubClient.Get(ctx, leaseName, lease)
		if errors.IsNotFound(err) {
			// Create the lease if it doesn't exist
			holderIdentity := "registration-agent"
			leaseDurationSeconds := int32(300) // 5 minutes
			now := metav1.NewMicroTime(time.Now())
			lease = &coordinationv1.Lease{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "managed-cluster-lease",
					Namespace: clusterToMigrate,
				},
				Spec: coordinationv1.LeaseSpec{
					HolderIdentity:       &holderIdentity,
					LeaseDurationSeconds: &leaseDurationSeconds,
					AcquireTime:          &now,
					RenewTime:            &now,
				},
			}
			Expect(sourceHubClient.Create(ctx, lease)).To(Succeed())
		} else {
			Expect(err).NotTo(HaveOccurred())
			now := metav1.NewMicroTime(time.Now())
			lease.Spec.RenewTime = &now
			Expect(sourceHubClient.Update(ctx, lease)).To(Succeed())
		}

		By("Waiting for registration controller to update managed cluster status")
		time.Sleep(10 * time.Second)

		By("Patching managed cluster status to Available")
		mc := &clusterv1.ManagedCluster{}
		err = sourceHubClient.Get(ctx, types.NamespacedName{Name: clusterToMigrate}, mc)
		Expect(err).NotTo(HaveOccurred())

		// Update the ManagedClusterConditionAvailable to True
		conditionFound := false
		for i, cond := range mc.Status.Conditions {
			if cond.Type == "ManagedClusterConditionAvailable" {
				mc.Status.Conditions[i].Status = metav1.ConditionTrue
				mc.Status.Conditions[i].Reason = "ManagedClusterAvailable"
				mc.Status.Conditions[i].Message = "Managed cluster is available"
				mc.Status.Conditions[i].LastTransitionTime = metav1.Now()
				conditionFound = true
				break
			}
		}
		// Add the condition if it doesn't exist
		if !conditionFound {
			mc.Status.Conditions = append(mc.Status.Conditions, metav1.Condition{
				Type:               "ManagedClusterConditionAvailable",
				Status:             metav1.ConditionTrue,
				Reason:             "ManagedClusterAvailable",
				Message:            "Managed cluster is available",
				LastTransitionTime: metav1.Now(),
			})
		}
		Expect(sourceHubClient.Status().Update(ctx, mc)).To(Succeed())

		By("Restarting global-hub-agent on source hub to force cache refresh")
		agentNamespace := "multicluster-global-hub-agent"
		podList := &corev1.PodList{}
		listOpts := []client.ListOption{
			client.InNamespace(agentNamespace),
			client.MatchingLabels{"name": "multicluster-global-hub-agent"},
		}
		if err := sourceHubClient.List(ctx, podList, listOpts...); err == nil {
			for _, pod := range podList.Items {
				_ = sourceHubClient.Delete(ctx, &pod)
			}
		}

		By("Waiting for agent pods to be deleted")
		time.Sleep(5 * time.Second)

		By("Refreshing lease before agent starts")
		// Update lease again right before agent starts syncing
		lease = &coordinationv1.Lease{}
		err = sourceHubClient.Get(ctx, leaseName, lease)
		if err == nil {
			now := metav1.NewMicroTime(time.Now())
			lease.Spec.RenewTime = &now
			_ = sourceHubClient.Update(ctx, lease)
		}

		By("Re-patching managed cluster status after pod deletion")
		mc = &clusterv1.ManagedCluster{}
		err = sourceHubClient.Get(ctx, types.NamespacedName{Name: clusterToMigrate}, mc)
		Expect(err).NotTo(HaveOccurred())
		conditionFound = false
		for i, cond := range mc.Status.Conditions {
			if cond.Type == "ManagedClusterConditionAvailable" {
				mc.Status.Conditions[i].Status = metav1.ConditionTrue
				mc.Status.Conditions[i].Reason = "ManagedClusterAvailable"
				mc.Status.Conditions[i].Message = "Managed cluster is available"
				mc.Status.Conditions[i].LastTransitionTime = metav1.Now()
				conditionFound = true
				break
			}
		}
		if !conditionFound {
			mc.Status.Conditions = append(mc.Status.Conditions, metav1.Condition{
				Type:               "ManagedClusterConditionAvailable",
				Status:             metav1.ConditionTrue,
				Reason:             "ManagedClusterAvailable",
				Message:            "Managed cluster is available",
				LastTransitionTime: metav1.Now(),
			})
		}
		_ = sourceHubClient.Status().Update(ctx, mc)

		By("Waiting for agent to restart and sync cache")
		time.Sleep(20 * time.Second)
	})

	AfterAll(func() {
		// Cleanup migration CR if exists
		mcm := &migrationv1alpha1.ManagedClusterMigration{
			ObjectMeta: metav1.ObjectMeta{
				Name:      migrationName,
				Namespace: migrationNamespace,
			},
		}
		_ = globalHubClient.Delete(ctx, mcm)
	})

	Context("Migration from source hub to target hub", func() {
		It("should verify cluster exists on source hub before migration", func() {
			mc := &clusterv1.ManagedCluster{}
			err := sourceHubClient.Get(ctx, types.NamespacedName{Name: clusterToMigrate}, mc)
			Expect(err).NotTo(HaveOccurred())
			Expect(mc.Spec.HubAcceptsClient).To(BeTrue())
		})

		It("should verify cluster does not exist on target hub before migration", func() {
			mc := &clusterv1.ManagedCluster{}
			err := targetHubClient.Get(ctx, types.NamespacedName{Name: clusterToMigrate}, mc)
			Expect(errors.IsNotFound(err)).To(BeTrue())
		})

		It("should setup managed-serviceaccount addon mock on global hub", func() {
			By("Creating managed-serviceaccount ManagedClusterAddOn in target hub namespace")
			// The migration controller looks for this addon in the target hub's namespace on global hub
			addOn := &addonapiv1alpha1.ManagedClusterAddOn{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "managed-serviceaccount",
					Namespace: targetHubName, // hub2 namespace on global hub
				},
				Spec: addonapiv1alpha1.ManagedClusterAddOnSpec{
					InstallNamespace: "open-cluster-management-agent-addon",
				},
			}
			err := globalHubClient.Create(ctx, addOn)
			if !errors.IsAlreadyExists(err) {
				Expect(err).NotTo(HaveOccurred())
			}

			By("Creating mock token secret for migration in target hub namespace")
			// The migration controller expects a secret with the migration name in the target hub namespace
			tokenSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("migrate-%s", clusterToMigrate),
					Namespace: targetHubName,
				},
				Data: map[string][]byte{
					"token":  []byte("mock-token"),
					"ca.crt": []byte("mock-ca-cert"),
				},
			}
			err = globalHubClient.Create(ctx, tokenSecret)
			if !errors.IsAlreadyExists(err) {
				Expect(err).NotTo(HaveOccurred())
			}

			By("Creating bootstrap ClusterRole on target hub for migration")
			// The agent migration syncer requires this ClusterRole to exist on the target hub
			bootstrapClusterRoleName := "open-cluster-management:managedcluster:bootstrap:agent-registration"
			bootstrapClusterRole := &rbacv1.ClusterRole{
				ObjectMeta: metav1.ObjectMeta{
					Name: bootstrapClusterRoleName,
				},
			}
			err = targetHubClient.Create(ctx, bootstrapClusterRole)
			if !errors.IsAlreadyExists(err) {
				Expect(err).NotTo(HaveOccurred())
			}

			By("Creating bootstrap ClusterRoleBinding on target hub for migration")
			// The agent migration syncer looks for this ClusterRoleBinding
			bootstrapClusterRoleBinding := &rbacv1.ClusterRoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name: bootstrapClusterRoleName,
				},
				RoleRef: rbacv1.RoleRef{
					APIGroup: "rbac.authorization.k8s.io",
					Kind:     "ClusterRole",
					Name:     bootstrapClusterRoleName,
				},
			}
			err = targetHubClient.Create(ctx, bootstrapClusterRoleBinding)
			if !errors.IsAlreadyExists(err) {
				Expect(err).NotTo(HaveOccurred())
			}
		})

		It("should create ManagedClusterMigration CR", func() {
			mcm := &migrationv1alpha1.ManagedClusterMigration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      migrationName,
					Namespace: migrationNamespace,
				},
				Spec: migrationv1alpha1.ManagedClusterMigrationSpec{
					IncludedManagedClusters: []string{clusterToMigrate},
					From:                    sourceHubName,
					To:                      targetHubName,
				},
			}
			err := globalHubClient.Create(ctx, mcm)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should wait for Initializing phase and mock klusterlet configuration", func() {
			By("Waiting for migration to reach Initializing phase")
			Eventually(func() string {
				mcm := &migrationv1alpha1.ManagedClusterMigration{}
				if err := globalHubClient.Get(ctx, types.NamespacedName{
					Name:      migrationName,
					Namespace: migrationNamespace,
				}, mcm); err != nil {
					return ""
				}
				return string(mcm.Status.Phase)
			}, 2*time.Minute, migrationPollInterval).Should(
				Or(Equal("Initializing"), Equal("Deploying"), Equal("Registering")))

			By("Checking for klusterlet-config annotation on managed cluster")
			Eventually(func() bool {
				mc := &clusterv1.ManagedCluster{}
				if err := sourceHubClient.Get(ctx, types.NamespacedName{Name: clusterToMigrate}, mc); err != nil {
					return false
				}
				_, hasAnnotation := mc.Annotations["agent.open-cluster-management.io/klusterlet-config"]
				return hasAnnotation
			}, 2*time.Minute, migrationPollInterval).Should(BeTrue())

			By("Mock: Creating ManifestWork to deploy bootstrap secret and patch klusterlet on managed cluster")
			mockKlusterletMigration(ctx, sourceHubClient, managedClusterClient, clusterToMigrate, targetHubName)
		})

		It("should verify bootstrap secrets and klusterlet are configured on managed cluster", func() {
			targetBootstrapSecretName := fmt.Sprintf("bootstrap-%s", targetHubName)
			sourceHubSecretName := "hub-kubeconfig-secret"

			By("Verifying target hub bootstrap secret exists on managed cluster")
			Eventually(func() error {
				secret := &corev1.Secret{}
				return managedClusterClient.Get(ctx, types.NamespacedName{
					Name:      targetBootstrapSecretName,
					Namespace: "open-cluster-management-agent",
				}, secret)
			}, 2*time.Minute, migrationPollInterval).Should(Succeed())

			By("Verifying source hub kubeconfig secret exists on managed cluster")
			Eventually(func() error {
				secret := &corev1.Secret{}
				return managedClusterClient.Get(ctx, types.NamespacedName{
					Name:      sourceHubSecretName,
					Namespace: "open-cluster-management-agent",
				}, secret)
			}, 2*time.Minute, migrationPollInterval).Should(Succeed())

			By("Verifying klusterlet has MultipleHubs feature gate enabled")
			Eventually(func() bool {
				klusterlet := &operatorv1.Klusterlet{}
				if err := managedClusterClient.Get(ctx, types.NamespacedName{Name: "klusterlet"}, klusterlet); err != nil {
					return false
				}
				// Check if MultipleHubs feature gate is enabled
				if klusterlet.Spec.RegistrationConfiguration == nil {
					return false
				}
				for _, fg := range klusterlet.Spec.RegistrationConfiguration.FeatureGates {
					if fg.Feature == "MultipleHubs" && fg.Mode == operatorv1.FeatureGateModeTypeEnable {
						return true
					}
				}
				return false
			}, 2*time.Minute, migrationPollInterval).Should(BeTrue())

			By("Verifying klusterlet has bootstrap kubeconfig secrets configured")
			Eventually(func() bool {
				klusterlet := &operatorv1.Klusterlet{}
				if err := managedClusterClient.Get(ctx, types.NamespacedName{Name: "klusterlet"}, klusterlet); err != nil {
					return false
				}
				if klusterlet.Spec.RegistrationConfiguration == nil ||
					klusterlet.Spec.RegistrationConfiguration.BootstrapKubeConfigs.LocalSecrets.KubeConfigSecrets == nil {
					return false
				}
				// Check if the target hub bootstrap secret is in the list
				for _, secret := range klusterlet.Spec.RegistrationConfiguration.BootstrapKubeConfigs.LocalSecrets.KubeConfigSecrets {
					if secret.Name == targetBootstrapSecretName {
						return true
					}
				}
				return false
			}, 2*time.Minute, migrationPollInterval).Should(BeTrue())
		})

		It("should wait for Registering phase and mock klusterlet status ManifestWork", func() {
			By("Waiting for migration to reach Registering phase")
			Eventually(func() string {
				mcm := &migrationv1alpha1.ManagedClusterMigration{}
				if err := globalHubClient.Get(ctx, types.NamespacedName{
					Name:      migrationName,
					Namespace: migrationNamespace,
				}, mcm); err != nil {
					return ""
				}
				return string(mcm.Status.Phase)
			}, 5*time.Minute, migrationPollInterval).Should(Equal("Registering"))

			By("Mock: Creating klusterlet status ManifestWork on target hub")
			mockKlusterletStatusManifestWork(ctx, targetHubClient, clusterToMigrate)

			By("Mock: Simulating cluster registration on target hub")
			mockClusterRegistration(ctx, targetHubClient, clusterToMigrate)
		})

		It("should complete migration successfully", func() {
			By("Waiting for migration to complete")
			Eventually(func() string {
				mcm := &migrationv1alpha1.ManagedClusterMigration{}
				if err := globalHubClient.Get(ctx, types.NamespacedName{
					Name:      migrationName,
					Namespace: migrationNamespace,
				}, mcm); err != nil {
					return ""
				}
				return string(mcm.Status.Phase)
			}, migrationTimeout, migrationPollInterval).Should(Equal("Completed"))
		})

		It("should verify cluster no longer exists on source hub", func() {
			Eventually(func() bool {
				mc := &clusterv1.ManagedCluster{}
				err := sourceHubClient.Get(ctx, types.NamespacedName{Name: clusterToMigrate}, mc)
				return errors.IsNotFound(err)
			}, 2*time.Minute, migrationPollInterval).Should(BeTrue())
		})

		It("should verify cluster exists on target hub", func() {
			mc := &clusterv1.ManagedCluster{}
			err := targetHubClient.Get(ctx, types.NamespacedName{Name: clusterToMigrate}, mc)
			Expect(err).NotTo(HaveOccurred())
			Expect(mc.Spec.HubAcceptsClient).To(BeTrue())
		})
	})
})

// mockKlusterletMigration creates a ManifestWork on source hub to deploy bootstrap secret
// and patch klusterlet on the managed cluster. This mocks the KlusterletConfig behavior in ACM.
func mockKlusterletMigration(ctx context.Context, sourceHubClient, managedClusterClient client.Client, clusterName, targetHub string) {
	// Get bootstrap secret from multicluster-engine namespace
	bootstrapSecret := &corev1.Secret{}
	bootstrapSecretName := fmt.Sprintf("bootstrap-%s", targetHub)
	err := sourceHubClient.Get(ctx, types.NamespacedName{
		Name:      bootstrapSecretName,
		Namespace: "multicluster-engine",
	}, bootstrapSecret)
	if err != nil {
		// If bootstrap secret not found, skip mock (migration might handle it differently)
		return
	}

	// Create bootstrap secret manifest for managed cluster
	targetBootstrapSecret := map[string]any{
		"apiVersion": "v1",
		"kind":       "Secret",
		"metadata": map[string]any{
			"name":      bootstrapSecretName,
			"namespace": "open-cluster-management-agent",
		},
		"data": bootstrapSecret.Data,
		"type": "Opaque",
	}

	// Get existing klusterlet from managed cluster to merge configuration
	existingKlusterlet := &operatorv1.Klusterlet{}
	err = managedClusterClient.Get(ctx, types.NamespacedName{Name: "klusterlet"}, existingKlusterlet)
	Expect(err).NotTo(HaveOccurred())

	// Merge: Add MultipleHubs feature gate if not already present
	if existingKlusterlet.Spec.RegistrationConfiguration == nil {
		existingKlusterlet.Spec.RegistrationConfiguration = &operatorv1.RegistrationConfiguration{}
	}
	hasMultipleHubs := false
	for _, fg := range existingKlusterlet.Spec.RegistrationConfiguration.FeatureGates {
		if fg.Feature == "MultipleHubs" {
			hasMultipleHubs = true
			break
		}
	}
	if !hasMultipleHubs {
		existingKlusterlet.Spec.RegistrationConfiguration.FeatureGates = append(
			existingKlusterlet.Spec.RegistrationConfiguration.FeatureGates,
			operatorv1.FeatureGate{Feature: "MultipleHubs", Mode: operatorv1.FeatureGateModeTypeEnable},
		)
	}

	// Merge: Configure bootstrap kubeconfigs with both target and source hub secrets
	existingKlusterlet.Spec.RegistrationConfiguration.BootstrapKubeConfigs = operatorv1.BootstrapKubeConfigs{
		Type: operatorv1.LocalSecrets,
		LocalSecrets: &operatorv1.LocalSecretsConfig{
			HubConnectionTimeoutSeconds: 180,
			KubeConfigSecrets: []operatorv1.KubeConfigSecret{
				{Name: bootstrapSecretName},
				{Name: "hub-kubeconfig-secret"},
			},
		},
	}

	// Ensure TypeMeta is set for serialization (controller-runtime objects don't include it by default)
	existingKlusterlet.TypeMeta = metav1.TypeMeta{
		APIVersion: "operator.open-cluster-management.io/v1",
		Kind:       "Klusterlet",
	}

	// Serialize the merged klusterlet
	klusterletBytes, _ := json.Marshal(existingKlusterlet)
	secretBytes, _ := json.Marshal(targetBootstrapSecret)

	manifestWork := &workv1.ManifestWork{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-migration-mock", clusterName),
			Namespace: clusterName,
		},
		Spec: workv1.ManifestWorkSpec{
			Workload: workv1.ManifestsTemplate{
				Manifests: []workv1.Manifest{
					{RawExtension: runtime.RawExtension{Raw: secretBytes}},
					{RawExtension: runtime.RawExtension{Raw: klusterletBytes}},
				},
			},
		},
	}

	// Create or update the ManifestWork
	existing := &workv1.ManifestWork{}
	err = sourceHubClient.Get(ctx, client.ObjectKeyFromObject(manifestWork), existing)
	if errors.IsNotFound(err) {
		Expect(sourceHubClient.Create(ctx, manifestWork)).To(Succeed())
	}

	// Since there's no work-agent in Kind e2e environment, directly apply resources to managed cluster
	// This simulates what the work-agent would do when it applies the ManifestWork

	// Create bootstrap secret on managed cluster
	managedClusterBootstrapSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      bootstrapSecretName,
			Namespace: "open-cluster-management-agent",
		},
		Data: bootstrapSecret.Data,
		Type: corev1.SecretTypeOpaque,
	}
	existingSecret := &corev1.Secret{}
	err = managedClusterClient.Get(ctx, client.ObjectKeyFromObject(managedClusterBootstrapSecret), existingSecret)
	if errors.IsNotFound(err) {
		Expect(managedClusterClient.Create(ctx, managedClusterBootstrapSecret)).To(Succeed())
	}

	// Update klusterlet on managed cluster (remove TypeMeta for update)
	existingKlusterlet.TypeMeta = metav1.TypeMeta{}
	Expect(managedClusterClient.Update(ctx, existingKlusterlet)).To(Succeed())
}

// mockKlusterletStatusManifestWork creates a ReadOnly ManifestWork on target hub
// to collect klusterlet status. This mocks Issue #5.
func mockKlusterletStatusManifestWork(ctx context.Context, targetHubClient client.Client, clusterName string) {
	// First, ensure the cluster namespace exists on target hub
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: clusterName,
		},
	}
	_ = targetHubClient.Create(ctx, ns)

	klusterletManifest := map[string]any{
		"apiVersion": "operator.open-cluster-management.io/v1",
		"kind":       "Klusterlet",
		"metadata": map[string]any{
			"name": "klusterlet",
		},
	}
	klusterletBytes, _ := json.Marshal(klusterletManifest)

	manifestWork := &workv1.ManifestWork{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-klusterlet", clusterName),
			Namespace: clusterName,
		},
		Spec: workv1.ManifestWorkSpec{
			Workload: workv1.ManifestsTemplate{
				Manifests: []workv1.Manifest{
					{RawExtension: runtime.RawExtension{Raw: klusterletBytes}},
				},
			},
			ManifestConfigs: []workv1.ManifestConfigOption{
				{
					ResourceIdentifier: workv1.ResourceIdentifier{
						Group:    "operator.open-cluster-management.io",
						Resource: "klusterlets",
						Name:     "klusterlet",
					},
					FeedbackRules: []workv1.FeedbackRule{
						{Type: workv1.WellKnownStatusType},
						{
							Type: workv1.JSONPathsType,
							JsonPaths: []workv1.JsonPath{
								{
									Name: "isAvailable",
									Path: `.status.conditions[?(@.type=="Available")].status`,
								},
							},
						},
					},
					UpdateStrategy: &workv1.UpdateStrategy{
						Type: workv1.UpdateStrategyTypeReadOnly,
					},
				},
			},
		},
	}

	// Create or update the ManifestWork
	existing := &workv1.ManifestWork{}
	err := targetHubClient.Get(ctx, client.ObjectKeyFromObject(manifestWork), existing)
	if errors.IsNotFound(err) {
		Expect(targetHubClient.Create(ctx, manifestWork)).To(Succeed())
	}

	// Mock the ManifestWork status to show WorkApplied = True
	Eventually(func() error {
		work := &workv1.ManifestWork{}
		if err := targetHubClient.Get(ctx, client.ObjectKeyFromObject(manifestWork), work); err != nil {
			return err
		}
		work.Status.Conditions = []metav1.Condition{
			{
				Type:               workv1.WorkApplied,
				Status:             metav1.ConditionTrue,
				LastTransitionTime: metav1.Now(),
				Reason:             "AppliedManifestComplete",
				Message:            "Apply manifest work complete",
			},
		}
		// Mock feedback showing klusterlet is available
		work.Status.ResourceStatus = workv1.ManifestResourceStatus{
			Manifests: []workv1.ManifestCondition{
				{
					ResourceMeta: workv1.ManifestResourceMeta{
						Group:    "operator.open-cluster-management.io",
						Resource: "klusterlets",
						Name:     "klusterlet",
					},
					StatusFeedbacks: workv1.StatusFeedbackResult{
						Values: []workv1.FeedbackValue{
							{
								Name: "isAvailable",
								Value: workv1.FieldValue{
									Type:   workv1.String,
									String: ptrString("True"),
								},
							},
						},
					},
					Conditions: []metav1.Condition{
						{
							Type:               "Applied",
							Status:             metav1.ConditionTrue,
							LastTransitionTime: metav1.Now(),
							Reason:             "AppliedManifestWorkComplete",
							Message:            "Apply manifest work complete",
						},
					},
				},
			},
		}
		return targetHubClient.Status().Update(ctx, work)
	}, time.Minute, migrationPollInterval).Should(Succeed())
}

// mockClusterRegistration simulates cluster registration on the target hub
func mockClusterRegistration(ctx context.Context, targetHubClient client.Client, clusterName string) {
	// Create namespace for the cluster
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: clusterName,
		},
	}
	_ = targetHubClient.Create(ctx, ns)

	// Create ManagedCluster on target hub (simulating agent registration)
	mc := &clusterv1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: clusterName,
		},
		Spec: clusterv1.ManagedClusterSpec{
			HubAcceptsClient:     true,
			LeaseDurationSeconds: 60,
		},
	}

	existing := &clusterv1.ManagedCluster{}
	err := targetHubClient.Get(ctx, types.NamespacedName{Name: clusterName}, existing)
	if errors.IsNotFound(err) {
		Expect(targetHubClient.Create(ctx, mc)).To(Succeed())
	}

	// Update status to show cluster is available
	Eventually(func() error {
		mc := &clusterv1.ManagedCluster{}
		if err := targetHubClient.Get(ctx, types.NamespacedName{Name: clusterName}, mc); err != nil {
			return err
		}
		mc.Status.Conditions = []metav1.Condition{
			{
				Type:               clusterv1.ManagedClusterConditionAvailable,
				Status:             metav1.ConditionTrue,
				LastTransitionTime: metav1.Now(),
				Reason:             "ManagedClusterAvailable",
				Message:            "Managed cluster is available",
			},
			{
				Type:               clusterv1.ManagedClusterConditionJoined,
				Status:             metav1.ConditionTrue,
				LastTransitionTime: metav1.Now(),
				Reason:             "ManagedClusterJoined",
				Message:            "Managed cluster joined",
			},
		}
		return targetHubClient.Status().Update(ctx, mc)
	}, time.Minute, migrationPollInterval).Should(Succeed())
}

func ptrString(s string) *string {
	return &s
}
