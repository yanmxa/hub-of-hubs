# Labels

List all of the labels that are used by the multicluster global hub.

| Label                                                                                                | Description                                                                                                                                                                                                                                                  |
| ---------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| global-hub.open-cluster-management.io/managed-by=`global-hub-operator\|global-hub\|global-hub-agent` | If the value is `global-hub-operator`, the resources are created by the global hub operator. The global hub operator watches the resources based on this label.                                                                                     |
| global-hub.open-cluster-management.io/global-resource=                                               | This label is added when creating the global resources. It is used to identify the resource that transport needs to propagate to the managed hub clusters.                                                                                               |
| global-hub.open-cluster-management.io/hub-cluster-install=                                           | This label is used on ManagedCluster. If this label exists, the global hub operator installs Red Hat Advanced Cluster Management on a managed cluster. If the label is not included, Red Hat Advanced Cluster Management is not installed on the managed cluster by the global hub operator.                                          |
| global-hub.open-cluster-management.io/metrics-resource=`strimzi\|postgres` | It's used to identify the resource owned by enableMetrics, if the enableMetrics is disabled, these resources should be removed. |
| global-hub.open-cluster-management.io/deploy-mode = `hosted\| default`                  | This label is used on ManagedCluster.<br>`hosted` means the klusterlet and global hub agent is deployed on Hosting cluster.<br>`default` means the klusterlet and global hub agent is deployed on managed cluster.|

# Annotations

List all annotations are used by multicluster global hub.

| Annotation                                                       | Description                                                                                                                                                        |
| ---------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| global-hub.open-cluster-management.io/managed-by=                | This annotation is used to identify which managed cluster is managed by which managed hub cluster.                                                                  |
| global-hub.open-cluster-management.io/origin-ownerreference-uid= | This annotation is used to identify that the resource is from the global hub cluster. The global hub agent is only handled with the resource which has this annotation. |
| mgh-image-repository=                                            | This annotation is used on the MCGH/MGH custom resource to identify a custom image repository.                                                                                      |
| global-hub.open-cluster-management.io/with-inventory                | This annotation is used to identify the common inventory is deployed.                                                                  |
| global-hub.open-cluster-management.io/with-stackrox-integration | This annotation enables the experimental integration with [Stackrox](https://github.com/stackrox).|
| global-hub.open-cluster-management.io/kafka-cluster-id | This annotation save the kafka cluster id in transport-config secret in agent part, it is used to identify if the kafka cluster changed.|
| global-hub.open-cluster-management.io/strimzi-catalog-source-name | This annotation is used to set catalog source name to strimzi subscription |
| global-hub.open-cluster-management.io/strimzi-catalog-source-namespace | This annotation is used to set catalog source namespace to strimzi subscription |
| global-hub.open-cluster-management.io/strimzi-subscription-package-name | This annotation is used to set package name to strimzi subscription |
| global-hub.open-cluster-management.io/strimzi-subscription-channel | This annotation is used to set channel to strimzi subscription|
# Finalizer

List all of the finalizers that are used by the multicluster global hub.

| Finalizer                                              | Description                                                         |
| ------------------------------------------------------ | ------------------------------------------------------------------- |
| global-hub.open-cluster-management.io/resource-cleanup | This is the finalizer that is used by the multicluster global hub. |

# ClusterClaim 

List the ClusterClaim generated by the global hub agent on the managed hub clusters.

| Name                               | Description                                                                                                                                                                                                                                                                      |
| ---------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| version.open-cluster-management.io | The value is the version of Red Hat Advanced Cluster Management.                                                                                                                                                                                                                                                 |
| hub.open-cluster-management.io     | The value is the Red Hat Advanced Cluster Management Hub installation mode.<br> `NotInstalled`: The Red Hat Advanced Cluster Management Hub is not installed on the cluster.<br>`InstalledByUser`: The Red Hat Advanced Cluster Management (or Red Hat OpenShift Cluster Manager) Hub has been installed before the global hub is deployed.<br>`InstalledByGlobalHub`: The Red Hat Advanced Cluster Management Hub was installed by Global Hub. |