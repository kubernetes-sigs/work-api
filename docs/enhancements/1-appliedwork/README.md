# New AppliedWork internal API

## Release Signoff Checklist

- [ ] Enhancement is `implementable`
- [ ] Design details are appropriately documented from clear requirements
- [ ] Test plan is defined
- [ ] Graduation criteria for dev preview, tech preview, GA
- [ ] User-facing documentation is created in [website](https://github.com/kubernetes-sigs/work-api)

## Summary

This proposal would define a new cluster scoped AppliedWork internal API to represents a workload and/or cluster wide configurations that are applied on a managed cluster.

## Motivation

The AppliedWork CR is to be an anchor on the managed cluster to ease garbage collection of the applied workload and or the applied cluster scoped resources.
Each applied workload resource will have a resource identifier reference stored in the AppliedWork status.
Additionally, each namespace scoped applied workload resource will have a owner reference pointing at the AppliedWork CR.

By leveraging this AppliedWork API:
- Provide a view on the managed cluster of the work and configurations that has been applied.
- When the AppliedWork CR is deleted, all related applied workload resources will be cascade deleted.

The AppliedWork API is not meant for external use and it's only meant for internal use to ease garbage collection of applied workload on the managed cluster.

### Goals

- Define a new cluster scoped AppliedWork internal API.

### Non-Goals

- Handle applied resource status feedback.

## Proposal

We propose to introduce a new `AppliedWork` API 

### Design Details

#### API change  

Add a new `AppliedWork` internal API:

```go
// AppliedWork is a cluster scoped resource that 
// represents an applied work and or cluster wide configurations that is placed
// on a managed cluster. An appliedwork links to a work on a hub recording resources
// deployed in the managed cluster.
// When the agent is removed from managed cluster, cluster-admin on managed cluster
// can delete the appliedwork to remove resources deployed by the agent.
// The name of the appliedwork must be unique since the appledwork is cluster scoped.
type AppliedWork struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec represents the desired configuration of AppliedWork.
	// +required
	Spec AppliedWorkSpec `json:"spec"`

    // Status represents the current status of AppliedWork.
	// +optional
	Status AppliedtWorkStatus `json:"status,omitempty"`
}

// AppliedWorkList contains a list of AppliedWork
type AppliedWorkList struct {
	metav1.TypeMeta `json:",inline"`
	// Standard list metadata.
	// +optional
	metav1.ListMeta `json:"metadata,omitempty"`
	// List of works.
	// +listType=set
	Items []AppliedWork `json:"items"`
}

// AppliedWorkSpec represents the desired configuration of AppliedWork
type AppliedWorkSpec struct {
	// WorkName represents the name of the related work on the hub.
	// +required
	WorkName string `json:"workName"`
	// WorkNamespace represents the namespace of the related work on the hub.
	// +required
	WorkNamespace string `json:"workNamespace"`
}

// AppliedtWorkStatus represents the current status of AppliedWork
type AppliedtWorkStatus struct {
	// AppliedResources represents a list of resources defined within the Work that are applied.
	// +optional
	AppliedResources []AppliedResourceMeta `json:"appliedResources,omitempty"`
}

// AppliedResourceMeta represents the group, version, resource, name and namespace of a resource.
// Since these resources have been created, they must have valid group, version, resource, namespace, and name.
type AppliedResourceMeta struct {
	// ResourceIdentifier provides the identifiers needed to interact with any arbitrary object.
	// +required
    ResourceIdentifier `json:",inline"`

	// UID is set on successful deletion of the Kubernetes resource by controller. The
	// resource might be still visible on the managed cluster after this field is set.
	// It is not directly settable by a client.
	// +optional
	UID types.UID `json:"uid,omitempty"`
}
```

An example of `AppliedWork`:

```yaml
apiVersion: multicluster.x-k8s.io/v1alpha1
kind: AppliedWork
metadata:
  name: applied-work-01
  namespace: default
spec:
  workName: work-01
  workNamespace: cluster1
status:
  appliedResources:
  - group: apps
    name: nginx-deployment
    namespace: default
    resource: deployments
    uid: fcfe11e4-c658-4c6e-9985-d98b7a059d14
    version: v1
```

In addition, if the workload contains namespace scoped resources then
they will have owner references pointing to their associated AppliedWork.
For example:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: nginx-deployment
  namespace: default
  ownerReferences:
  - apiVersion: multicluster.x-k8s.io/v1alpha1
    kind: AppliedWork
    name: applied-work-01
    uid: 9664526d-c5ec-4f35-9f89-0b6411452385
...
```

### Test Plan

- Unit tests will cover the functionality of the controllers.
- Unit tests will cover the new API.

E2E tests will be added to cover cases including:
- Create/update/delete an AppliedWork;
- Create/update/delete a Work on hub cluster and verify AppliedWork on managed cluster ;

### Graduation Criteria

#### Alpha
1. The new API is reviewed and accepted;
2. Implementation is completed to support the functionalities;
3. Develop test cases to demonstrate that the above use cases work correctly;

#### Beta
1. At lease one component uses the new API.

#### GA
1. Pass the performance/scalability testing;

### Upgrade Strategy
AppliedWork CRD will need to be created on the managed cluster. The work agent on managed cluster will need to be upgraded.

### Version Skew Strategy
N/A
