package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
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

	klusterletManifest := map[string]interface{}{
		"apiVersion": "operator.open-cluster-management.io/v1",
		"kind":       "Klusterlet",
		"metadata": map[string]interface{}{
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
