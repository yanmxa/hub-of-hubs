# Cluster Lifecycle Events Improvement Proposal

## Executive Summary

This proposal addresses the lack of high-level lifecycle events for OpenShift cluster provisioning and destruction managed by Hive. Currently, critical lifecycle milestones are only visible through Pod and Job events, making it difficult to track cluster lifecycle without deep knowledge of Hive's implementation details.

**Problem**: cluster2 lifecycle tracking requires monitoring multiple low-level resources (Pods, Jobs) instead of the primary resource (ClusterDeployment).

**Proposal**: Emit ClusterDeployment-level events for key lifecycle milestones to provide clear, high-level cluster lifecycle visibility.

---

## Current State Analysis

### What We Have: Event Distribution by Resource Type

Based on cluster2's lifecycle events (05:29:54 - 07:30:16):

| Resource Type | Event Count | Phase | Visibility Level |
|---------------|-------------|-------|------------------|
| **ManagedCluster** | 12 | Import/Detach | ✅ High-level (good) |
| **Pod** | 28+ | Provision/Destroy | ❌ Low-level (detailed but noisy) |
| **Job** | 5 | Provision/Destroy | ⚠️ Mid-level (better but indirect) |
| **ClusterDeployment** | **0** | **ALL** | ❌ **Missing entirely** |

### Problem Statement

#### ❌ Problem 1: ClusterDeployment Has NO Events

```yaml
# What we observed for cluster2 ClusterDeployment
oc describe clusterdeployment cluster2 -n cluster2

Events: <none>  # ← This is the problem!
```

**Impact**:
- Users must understand Hive's internal architecture (imageset jobs, provision pods, uninstall pods)
- No clear indication of cluster lifecycle state in the primary resource
- Difficult to troubleshoot without knowing implementation details

#### ❌ Problem 2: Critical Milestones Only Visible in Pod Events

**Current Event Flow for Provision:**

```
05:29:54 - ManagedCluster: WaitForImporting          ← ACM level (good)
05:29:57 - Job: cluster2-imageset created             ← Implementation detail
05:30:05 - Job: cluster2-imageset completed           ← Implementation detail
05:30:05 - Job: cluster2-0-bvpxh-provision created    ← Implementation detail
05:30:34 - Pod: installer container started           ← Deep implementation detail
06:17:05 - Job: cluster2-0-bvpxh-provision completed  ← Implementation detail
06:17:06 - ManagedCluster: Importing                  ← ACM level (good)
```

**What's Missing:**
```
? - ClusterDeployment: ProvisionStarted     ← Should exist
? - ClusterDeployment: Installing           ← Should exist
? - ClusterDeployment: ProvisionCompleted   ← Should exist
```

#### ❌ Problem 3: Destroy Phase Has Almost No Visibility

**Current Event Flow for Destroy:**

```
07:30:16 - ManagedCluster: Detaching               ← ACM level
07:30:16 - Job: cluster2-uninstall created         ← Implementation detail
07:30:16 - Pod: cluster2-uninstall-h7k5b scheduled ← Implementation detail
?        - [No events for actual destruction]      ← Black box
?        - [No completion event]                   ← Don't know when done
```

**What's Missing:**
```
? - ClusterDeployment: DeprovisionStarted    ← Should exist
? - ClusterDeployment: DeletingInfrastructure ← Should exist
? - ClusterDeployment: DeprovisionCompleted  ← Should exist
? - ClusterDeployment: DeprovisionFailed     ← Should exist for errors
```

---

## Proposed Solution

### Emit ClusterDeployment-Level Events for Key Lifecycle Milestones

#### Proposed Event Timeline

```
┌─────────────────────────────────────────────────────────────────┐
│                    PROVISION PHASE                               │
└─────────────────────────────────────────────────────────────────┘

05:29:54 - ClusterDeployment: Created
           Message: ClusterDeployment cluster2 created, preparing to provision
           Reason: ClusterDeploymentCreated
           Type: Normal

05:29:57 - ClusterDeployment: ValidatingImage
           Message: Validating release image 4.19.19
           Reason: ImageValidation
           Type: Normal

05:30:05 - ClusterDeployment: ImageValidated
           Message: Release image validated successfully
           Reason: ImageValidated
           Type: Normal

05:30:05 - ClusterDeployment: ProvisionStarted
           Message: Starting cluster provision on AWS in us-east-1
           Reason: ProvisionStarted
           Type: Normal

05:30:34 - ClusterDeployment: Installing
           Message: OpenShift installer running, creating infrastructure
           Reason: Installing
           Type: Normal

05:34:55 - ClusterDeployment: ProvisionProgressing
           Message: Provision in progress, bootstrap complete, waiting for cluster operators
           Reason: Progressing
           Type: Normal

06:17:05 - ClusterDeployment: ProvisionCompleted
           Message: Cluster successfully provisioned, API server available
           Reason: ProvisionCompleted
           Type: Normal

┌─────────────────────────────────────────────────────────────────┐
│                      DESTROY PHASE                               │
└─────────────────────────────────────────────────────────────────┘

07:30:16 - ClusterDeployment: DeprovisionStarted
           Message: Starting cluster deprovision, cleaning up resources
           Reason: DeprovisionStarted
           Type: Normal

07:30:20 - ClusterDeployment: DeletingInfrastructure
           Message: Deleting AWS infrastructure (VPC, instances, load balancers)
           Reason: DeletingInfrastructure
           Type: Normal

07:35:45 - ClusterDeployment: InfrastructureDeleted
           Message: All AWS resources deleted successfully
           Reason: InfrastructureDeleted
           Type: Normal

07:35:50 - ClusterDeployment: DeprovisionCompleted
           Message: Cluster deprovision completed successfully
           Reason: DeprovisionCompleted
           Type: Normal
```

---

## Detailed Event Specifications

### Provision Phase Events

#### 1. ClusterDeploymentCreated

```yaml
type: Normal
reason: ClusterDeploymentCreated
message: "ClusterDeployment {name} created, preparing to provision OpenShift {version} on {platform}"
involvedObject:
  apiVersion: hive.openshift.io/v1
  kind: ClusterDeployment
  name: cluster2
  namespace: cluster2
```

**When**: ClusterDeployment CR created
**Controller**: hive-controller (ClusterDeployment reconciler)

---

#### 2. ImageValidation

```yaml
type: Normal
reason: ImageValidation
message: "Validating release image {image}"
involvedObject:
  kind: ClusterDeployment
  name: cluster2
```

**When**: ImageSet job starts
**Controller**: hive-controller

---

#### 3. ImageValidated

```yaml
type: Normal
reason: ImageValidated
message: "Release image {version} validated successfully"
involvedObject:
  kind: ClusterDeployment
  name: cluster2
```

**When**: ImageSet job completes
**Controller**: hive-controller

---

#### 4. ProvisionStarted

```yaml
type: Normal
reason: ProvisionStarted
message: "Starting cluster provision on {platform} in {region}"
involvedObject:
  kind: ClusterDeployment
  name: cluster2
```

**When**: Provision job created
**Controller**: hive-controller

---

#### 5. Installing

```yaml
type: Normal
reason: Installing
message: "OpenShift installer running, creating infrastructure and deploying cluster"
involvedObject:
  kind: ClusterDeployment
  name: cluster2
```

**When**: Installer container starts
**Controller**: provision-job-controller

---

#### 6. ProvisionProgressing

```yaml
type: Normal
reason: Progressing
message: "Provision in progress: {stage_info}"
involvedObject:
  kind: ClusterDeployment
  name: cluster2
```

**When**: Periodic updates during installation (e.g., every 5 minutes or on stage changes)
**Controller**: provision-job-controller
**Examples**:
- "Bootstrap complete, installing control plane"
- "Control plane ready, installing workers"
- "Waiting for cluster operators to stabilize"

---

#### 7. ProvisionCompleted

```yaml
type: Normal
reason: ProvisionCompleted
message: "Cluster successfully provisioned, API server available at {api_url}"
involvedObject:
  kind: ClusterDeployment
  name: cluster2
```

**When**: Provision job completes successfully
**Controller**: hive-controller

---

#### 8. ProvisionFailed

```yaml
type: Warning
reason: ProvisionFailed
message: "Cluster provision failed: {error_summary}"
involvedObject:
  kind: ClusterDeployment
  name: cluster2
```

**When**: Provision job fails
**Controller**: hive-controller

---

### Destroy Phase Events

#### 9. DeprovisionStarted

```yaml
type: Normal
reason: DeprovisionStarted
message: "Starting cluster deprovision, cleaning up all resources"
involvedObject:
  kind: ClusterDeployment
  name: cluster2
```

**When**: ClusterDeployment deletion detected, uninstall job created
**Controller**: hive-controller

---

#### 10. DeletingInfrastructure

```yaml
type: Normal
reason: DeletingInfrastructure
message: "Deleting cloud infrastructure on {platform}"
involvedObject:
  kind: ClusterDeployment
  name: cluster2
```

**When**: Uninstall pod starts running `openshift-install destroy`
**Controller**: deprovision-job-controller

---

#### 11. InfrastructureDeleted

```yaml
type: Normal
reason: InfrastructureDeleted
message: "All cloud infrastructure deleted successfully"
involvedObject:
  kind: ClusterDeployment
  name: cluster2
```

**When**: `openshift-install destroy` completes successfully
**Controller**: deprovision-job-controller

---

#### 12. DeprovisionCompleted

```yaml
type: Normal
reason: DeprovisionCompleted
message: "Cluster deprovision completed successfully, all resources cleaned up"
involvedObject:
  kind: ClusterDeployment
  name: cluster2
```

**When**: Uninstall job completes, before ClusterDeployment finalizer removal
**Controller**: hive-controller

---

#### 13. DeprovisionFailed

```yaml
type: Warning
reason: DeprovisionFailed
message: "Cluster deprovision failed: {error_summary}. Manual cleanup may be required."
involvedObject:
  kind: ClusterDeployment
  name: cluster2
```

**When**: Uninstall job fails
**Controller**: hive-controller

---

## Comparison: Current vs. Proposed

### Current State: cluster2 Provision Events (Excerpt)

```
oc get events -n cluster2 --field-selector involvedObject.kind=ClusterDeployment

No resources found  ← Current state!
```

```
oc get events -n cluster2 | grep -E "(imageset|provision)"

cluster2-imageset.187ada45cd3cdea0              Created pod: cluster2-imageset-gflgk
cluster2-imageset.187ada47c8760ed5              Job completed
cluster2-0-bvpxh-provision.187ada47cd842b78     Created pod: cluster2-0-bvpxh-provision-ckxgl
cluster2-0-bvpxh-provision.187adcd8605c9da8     Job completed
```

**Problems**:
- No ClusterDeployment events
- Must parse Job/Pod names to understand they're related to cluster2
- No indication of what phase we're in
- No progress updates during 46-minute installation

---

### Proposed State: What Users Would See

```
oc get events -n cluster2 --field-selector involvedObject.kind=ClusterDeployment

LAST SEEN   TYPE      REASON                  OBJECT                        MESSAGE
47m         Normal    ClusterDeploymentCreated clusterdeployment/cluster2   ClusterDeployment cluster2 created...
47m         Normal    ImageValidation          clusterdeployment/cluster2   Validating release image 4.19.19
47m         Normal    ImageValidated           clusterdeployment/cluster2   Release image validated successfully
47m         Normal    ProvisionStarted         clusterdeployment/cluster2   Starting cluster provision on AWS in us-east-1
46m         Normal    Installing               clusterdeployment/cluster2   OpenShift installer running...
42m         Normal    Progressing              clusterdeployment/cluster2   Bootstrap complete, installing control plane
30m         Normal    Progressing              clusterdeployment/cluster2   Control plane ready, installing workers
15m         Normal    Progressing              clusterdeployment/cluster2   Waiting for cluster operators
1m          Normal    ProvisionCompleted       clusterdeployment/cluster2   Cluster successfully provisioned
```

**Benefits**:
- ✅ Clear high-level view of cluster lifecycle
- ✅ Progress visibility during long-running operations
- ✅ No need to understand Hive internals
- ✅ Easy to debug issues by looking at ClusterDeployment events first

---

## Implementation Guidance

### Where to Emit Events

#### 1. hive-controller (ClusterDeployment Reconciler)

**File**: `pkg/controller/clusterdeployment/clusterdeployment_controller.go`

```go
// Emit event when ClusterDeployment is created
func (r *ReconcileClusterDeployment) Reconcile(request reconcile.Request) {
    cd := &hivev1.ClusterDeployment{}
    // ... fetch ClusterDeployment

    if cd.Status.Installed == false && cd.Status.ProvisionRef == nil {
        r.eventRecorder.Event(cd, corev1.EventTypeNormal,
            "ClusterDeploymentCreated",
            fmt.Sprintf("ClusterDeployment %s created, preparing to provision OpenShift %s on %s",
                cd.Name, cd.Spec.Provisioning.ImageSetRef.Name, cd.Spec.Platform))
    }

    // Emit ImageValidation event when imageset job starts
    if imageSetJobStarted && !imageSetEventEmitted {
        r.eventRecorder.Event(cd, corev1.EventTypeNormal,
            "ImageValidation",
            fmt.Sprintf("Validating release image %s", imageName))
    }

    // Emit ImageValidated when imageset job completes
    if imageSetJobCompleted {
        r.eventRecorder.Event(cd, corev1.EventTypeNormal,
            "ImageValidated",
            "Release image validated successfully")
    }

    // Emit ProvisionStarted when provision job is created
    if provisionJobCreated && !provisionStartedEventEmitted {
        r.eventRecorder.Event(cd, corev1.EventTypeNormal,
            "ProvisionStarted",
            fmt.Sprintf("Starting cluster provision on %s in %s",
                platform, region))
    }

    // Emit ProvisionCompleted when installation succeeds
    if cd.Status.Installed == true && !provisionCompletedEventEmitted {
        r.eventRecorder.Event(cd, corev1.EventTypeNormal,
            "ProvisionCompleted",
            fmt.Sprintf("Cluster successfully provisioned, API server available at %s",
                cd.Status.APIURL))
    }

    // Emit ProvisionFailed on failure
    if provisionFailed {
        r.eventRecorder.Event(cd, corev1.EventTypeWarning,
            "ProvisionFailed",
            fmt.Sprintf("Cluster provision failed: %s", errorMessage))
    }
}
```

#### 2. provision-job-controller (Install Job Monitor)

**New Component**: Monitor provision pod logs and emit progress events

```go
// Watch provision pod and parse installer logs
func (r *ProvisionJobController) monitorInstallProgress(cd *hivev1.ClusterDeployment, podName string) {
    // Parse installer logs for progress indicators
    // Emit events for major milestones:

    if installerStarted {
        r.eventRecorder.Event(cd, corev1.EventTypeNormal,
            "Installing",
            "OpenShift installer running, creating infrastructure and deploying cluster")
    }

    if bootstrapComplete {
        r.eventRecorder.Event(cd, corev1.EventTypeNormal,
            "Progressing",
            "Bootstrap complete, installing control plane")
    }

    if controlPlaneReady {
        r.eventRecorder.Event(cd, corev1.EventTypeNormal,
            "Progressing",
            "Control plane ready, installing workers")
    }

    if waitingForOperators {
        r.eventRecorder.Event(cd, corev1.EventTypeNormal,
            "Progressing",
            "Waiting for cluster operators to stabilize")
    }
}
```

#### 3. deprovision-job-controller (Uninstall Job Monitor)

**File**: `pkg/controller/clusterdeprovision/clusterdeprovision_controller.go` (or new)

```go
func (r *DeprovisionController) Reconcile(request reconcile.Request) {
    cd := &hivev1.ClusterDeployment{}
    // ... fetch ClusterDeployment being deleted

    if cd.DeletionTimestamp != nil && !deprovisionStartedEventEmitted {
        r.eventRecorder.Event(cd, corev1.EventTypeNormal,
            "DeprovisionStarted",
            "Starting cluster deprovision, cleaning up all resources")
    }

    // Monitor uninstall pod logs
    if uninstallPodRunning {
        r.eventRecorder.Event(cd, corev1.EventTypeNormal,
            "DeletingInfrastructure",
            fmt.Sprintf("Deleting cloud infrastructure on %s", platform))
    }

    if infrastructureDeleted {
        r.eventRecorder.Event(cd, corev1.EventTypeNormal,
            "InfrastructureDeleted",
            "All cloud infrastructure deleted successfully")
    }

    if uninstallJobCompleted {
        r.eventRecorder.Event(cd, corev1.EventTypeNormal,
            "DeprovisionCompleted",
            "Cluster deprovision completed successfully, all resources cleaned up")
    }

    if uninstallJobFailed {
        r.eventRecorder.Event(cd, corev1.EventTypeWarning,
            "DeprovisionFailed",
            fmt.Sprintf("Cluster deprovision failed: %s. Manual cleanup may be required.",
                errorMessage))
    }
}
```

---

## Benefits

### 1. Improved User Experience

**Before** (Current):
```bash
# User wants to check cluster provision status
$ oc get clusterdeployment cluster2 -n cluster2
NAME       INFRAID          PLATFORM   REGION      VERSION   CLUSTERTYPE   PROVISIONSTATUS   POWERSTATE   AGE
cluster2   cluster2-sp82l   aws        us-east-1                           Provisioning                   5m

# Need to check multiple resources to understand what's happening
$ oc get jobs -n cluster2
NAME                            COMPLETIONS   DURATION   AGE
cluster2-imageset               1/1           8s         5m
cluster2-0-bvpxh-provision      0/1           4m50s      4m50s

$ oc get pods -n cluster2
NAME                                  READY   STATUS    RESTARTS   AGE
cluster2-0-bvpxh-provision-ckxgl      1/1     Running   0          4m50s

# Still no idea what stage the installation is in (could be anywhere in 46-minute process)
```

**After** (Proposed):
```bash
# User wants to check cluster provision status
$ oc get clusterdeployment cluster2 -n cluster2
NAME       INFRAID          PLATFORM   REGION      VERSION   CLUSTERTYPE   PROVISIONSTATUS   POWERSTATE   AGE
cluster2   cluster2-sp82l   aws        us-east-1                           Provisioning                   5m

# Check events to see exactly what's happening
$ oc get events -n cluster2 --field-selector involvedObject.name=cluster2,involvedObject.kind=ClusterDeployment
LAST SEEN   TYPE      REASON              OBJECT                        MESSAGE
5m          Normal    ProvisionStarted    clusterdeployment/cluster2   Starting cluster provision on AWS in us-east-1
4m50s       Normal    Installing          clusterdeployment/cluster2   OpenShift installer running, creating infrastructure
2m          Normal    Progressing         clusterdeployment/cluster2   Bootstrap complete, installing control plane

# Clear understanding: bootstrap done, control plane installing
```

### 2. Simplified Troubleshooting

**Scenario**: Provision fails during installation

**Before**:
```bash
# Check ClusterDeployment - no useful info
$ oc describe clusterdeployment cluster2 -n cluster2
Events: <none>

# Check provision job - need to know this exists
$ oc get jobs -n cluster2
NAME                            COMPLETIONS   DURATION   AGE
cluster2-0-bvpxh-provision      0/1           20m        20m

# Check pod logs - very verbose
$ oc logs cluster2-0-bvpxh-provision-ckxgl -n cluster2 | tail -100
[Many lines of installer output...]
```

**After**:
```bash
# Check ClusterDeployment events - immediately see the problem
$ oc get events -n cluster2 --field-selector involvedObject.kind=ClusterDeployment
LAST SEEN   TYPE      REASON              OBJECT                        MESSAGE
20m         Normal    ProvisionStarted    clusterdeployment/cluster2   Starting cluster provision...
15m         Normal    Installing          clusterdeployment/cluster2   OpenShift installer running...
5m          Normal    Progressing         clusterdeployment/cluster2   Waiting for cluster operators
1m          Warning   ProvisionFailed     clusterdeployment/cluster2   Cluster provision failed: timeout waiting for cluster operators

# Immediately know: provision failed due to operator timeout
# Can then dive into specific logs if needed
```

### 3. Better Monitoring and Alerting

```yaml
# Prometheus AlertRule example
apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: clusterdeployment-alerts
spec:
  groups:
  - name: cluster-lifecycle
    rules:
    - alert: ClusterProvisionStuck
      expr: |
        (time() - kube_event_last_timestamp{
          reason="Installing",
          involved_object_kind="ClusterDeployment"
        }) > 3600
      annotations:
        message: "ClusterDeployment {{ $labels.involved_object_name }} stuck in Installing state for >1h"

    - alert: ClusterProvisionFailed
      expr: |
        kube_event_count{
          reason="ProvisionFailed",
          involved_object_kind="ClusterDeployment",
          type="Warning"
        } > 0
      annotations:
        message: "ClusterDeployment {{ $labels.involved_object_name }} provision failed"

    - alert: ClusterDeprovisionFailed
      expr: |
        kube_event_count{
          reason="DeprovisionFailed",
          involved_object_kind="ClusterDeployment",
          type="Warning"
        } > 0
      annotations:
        message: "ClusterDeployment {{ $labels.involved_object_name }} deprovision failed - manual cleanup may be needed"
```

### 4. Consistent with Kubernetes Patterns

Other Kubernetes resources emit events for their lifecycle:

```bash
# Deployment events
$ oc get events --field-selector involvedObject.kind=Deployment
LAST SEEN   TYPE      REASON              OBJECT                    MESSAGE
5m          Normal    ScalingReplicaSet   deployment/my-app         Scaled up replica set to 3

# Pod events
$ oc get events --field-selector involvedObject.kind=Pod
LAST SEEN   TYPE      REASON      OBJECT          MESSAGE
2m          Normal    Scheduled   pod/my-pod      Successfully assigned to node-1
2m          Normal    Pulled      pod/my-pod      Container image pulled
2m          Normal    Started     pod/my-pod      Started container
```

ClusterDeployment should follow the same pattern!

---

## Migration and Backward Compatibility

### Phase 1: Add New Events (Non-Breaking)

- Emit new ClusterDeployment events alongside existing Job/Pod events
- No changes to existing events
- Users can start using new events immediately
- Existing monitoring/tooling continues to work

### Phase 2: Documentation and Adoption

- Update Hive documentation to recommend ClusterDeployment events
- Update troubleshooting guides
- Provide example queries and alerts

### Phase 3: Potential Cleanup (Future)

- Consider reducing verbosity of Pod-level events (optional)
- Could mark some Job events as deprecated (optional)
- Keep backward compatibility for at least 2-3 releases

---

## Alternative Approaches Considered

### Alternative 1: Use ClusterDeployment Status Conditions

**Approach**: Add status conditions instead of events

```yaml
status:
  conditions:
  - type: ImageValidated
    status: "True"
    lastTransitionTime: "2025-11-24T05:30:05Z"
  - type: Provisioning
    status: "True"
    lastTransitionTime: "2025-11-24T05:30:05Z"
  - type: Ready
    status: "False"
    reason: Installing
    message: "OpenShift installer running"
```

**Pros**:
- Always available in status
- Easy to query programmatically

**Cons**:
- No historical record (only current state)
- Limited to current conditions, hard to see progression over time
- Doesn't follow Kubernetes event patterns
- Can't see "Progressing" updates during 46-minute installation

**Recommendation**: Use both! Conditions for current state, Events for history.

### Alternative 2: Structured Logging Only

**Approach**: Just improve logs, no events

**Pros**:
- Detailed information in logs

**Cons**:
- Requires log aggregation system
- Not accessible via `kubectl get events`
- Doesn't integrate with Kubernetes event stream
- Harder to alert on

**Recommendation**: Rejected. Events are more accessible.

### Alternative 3: Custom CRD for Lifecycle Events

**Approach**: Create `ClusterLifecycleEvent` CRD

**Pros**:
- More structured than events
- Can have custom fields

**Cons**:
- Overly complex
- Doesn't integrate with standard Kubernetes tooling
- Users expect to use `kubectl get events`

**Recommendation**: Rejected. Standard Events are sufficient.

---

## Success Metrics

### Before Implementation (Current State - cluster2 Example)

❌ **ClusterDeployment events**: 0
❌ **Clear provision progress visibility**: No (must check pod logs)
❌ **Destroy phase visibility**: Minimal (only job/pod creation)
❌ **Time to understand cluster state**: 5+ minutes (check multiple resources)
❌ **Events useful for monitoring**: No (too low-level)

### After Implementation (Target State)

✅ **ClusterDeployment events**: 8-10 per lifecycle
✅ **Clear provision progress visibility**: Yes (events every major stage)
✅ **Destroy phase visibility**: Complete (start, progress, completion)
✅ **Time to understand cluster state**: <30 seconds (single event query)
✅ **Events useful for monitoring**: Yes (can alert on specific events)

---

## Appendix: Real-World Example

### cluster2 Lifecycle with Proposed Events

```bash
# Timeline of cluster2 with proposed events

# T+0s: 05:29:54 - Cluster Created
oc get events -n cluster2 --field-selector involvedObject.name=cluster2,involvedObject.kind=ClusterDeployment
LAST SEEN   TYPE      REASON                      OBJECT                        MESSAGE
0s          Normal    ClusterDeploymentCreated    clusterdeployment/cluster2   ClusterDeployment cluster2 created, preparing to provision OpenShift 4.19.19 on AWS

# T+3s: 05:29:57 - Image Validation
LAST SEEN   TYPE      REASON                      OBJECT                        MESSAGE
0s          Normal    ImageValidation             clusterdeployment/cluster2   Validating release image 4.19.19

# T+11s: 05:30:05 - Image Validated, Provision Starting
LAST SEEN   TYPE      REASON                      OBJECT                        MESSAGE
6s          Normal    ImageValidated              clusterdeployment/cluster2   Release image validated successfully
0s          Normal    ProvisionStarted            clusterdeployment/cluster2   Starting cluster provision on AWS in us-east-1

# T+40s: 05:30:34 - Installation Running
LAST SEEN   TYPE      REASON                      OBJECT                        MESSAGE
29s         Normal    Installing                  clusterdeployment/cluster2   OpenShift installer running, creating infrastructure

# T+5m: 05:34:55 - Bootstrap Complete
LAST SEEN   TYPE      REASON                      OBJECT                        MESSAGE
4m21s       Normal    Progressing                 clusterdeployment/cluster2   Bootstrap complete, installing control plane

# T+20m: 05:49:54 - Control Plane Ready
LAST SEEN   TYPE      REASON                      OBJECT                        MESSAGE
15m         Normal    Progressing                 clusterdeployment/cluster2   Control plane ready, installing workers

# T+40m: 06:09:54 - Operators Installing
LAST SEEN   TYPE      REASON                      OBJECT                        MESSAGE
20m         Normal    Progressing                 clusterdeployment/cluster2   Waiting for cluster operators to stabilize

# T+47m11s: 06:17:05 - Provision Complete
LAST SEEN   TYPE      REASON                      OBJECT                        MESSAGE
0s          Normal    ProvisionCompleted          clusterdeployment/cluster2   Cluster successfully provisioned, API server available

# ... 1h11m of operation ...

# T+2h22s: 07:30:16 - Destroy Initiated
LAST SEEN   TYPE      REASON                      OBJECT                        MESSAGE
0s          Normal    DeprovisionStarted          clusterdeployment/cluster2   Starting cluster deprovision, cleaning up all resources
5s          Normal    DeletingInfrastructure      clusterdeployment/cluster2   Deleting AWS infrastructure

# T+2h6m: 07:36:16 - Destruction Complete
LAST SEEN   TYPE      REASON                      OBJECT                        MESSAGE
1m          Normal    InfrastructureDeleted       clusterdeployment/cluster2   All AWS resources deleted successfully
0s          Normal    DeprovisionCompleted        clusterdeployment/cluster2   Cluster deprovision completed successfully
```

---

## Conclusion

Adding ClusterDeployment-level events will significantly improve the observability and user experience of cluster lifecycle management in Hive. This proposal:

✅ Addresses real pain points observed in cluster2's lifecycle
✅ Follows Kubernetes event patterns
✅ Backward compatible (adds events, doesn't change existing behavior)
✅ Provides immediate value for troubleshooting and monitoring
✅ Low implementation complexity (event emission at key reconciliation points)

**Recommendation**: Implement this proposal to bring ClusterDeployment event visibility in line with other Kubernetes resources and ACM's ManagedCluster events.

---

**Document Version**: 1.0
**Date**: 2025-11-24
**Based on**: cluster2 lifecycle analysis (events from cluster2-namespace-events-*.yaml)
**Related**: cluster-lifecycle-events.md, cluster-provision-events.md
