# New AppliedWork internal API

## Release Signoff Checklist

- [ ] Enhancement is `implementable`
- [ ] Design details are appropriately documented from clear requirements
- [ ] Test plan is defined
- [ ] Graduation criteria for dev preview, tech preview, GA
- [ ] User-facing documentation is created in [website](https://github.com/kubernetes-sigs/work-api)

## Summary

The proposed work would define a new AppliedWork internal API to represents a namespace scope workload that is applied on a managed cluster.

## Motivation

The AppliedWork CR is to be an anchor on the managed cluster to ease garbage collection of the applied workload.
Each applied workload resource will have a owner reference pointing at the AppliedWork CR.

By leveraging this AppliedWork API:
- Provide a view on the managed cluster of the work that has been applied.
- When the AppliedWork CR is deleted, all related applied workload resources will be cascade deleted.

The AppliedWork API is not meant for external use and it's only meant for internal use to ease garbage collection of applied workload on the managed cluster.

### Goals

- Define a new namespace scope AppliedWork internal API.

### Non-Goals

- Handle cluster scope workloads.
- Handle applied resource status feedback.

## Proposal

We propose to introduce a new `AppliedWork` API 

### Design Details

#### API change  

Add a new `AppliedWork` internal API  

```go
// AppliedWork represents an applied work on managed cluster that is placed
// on a managed cluster. An appliedwork links to a work on a hub recording resources
// deployed in the managed cluster.
// When the agent is removed from managed cluster, cluster-admin on managed cluster
// can delete the appliedwork to remove resources deployed by the agent.
// The name of the appliedwork must be the same as {work name}
// The namespace of the appliedwork should be the same as the resource applied on
// the managed cluster.
type AppliedWork struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec represents the desired configuration of AppliedWork.
	Spec AppliedWorkSpec `json:"spec"`
}

// AppliedWorkList contains a list of AppliedWork
type AppliedWorkList struct {
	metav1.TypeMeta `json:",inline"`
	// Standard list metadata.
	metav1.ListMeta `json:"metadata,omitempty"`
	// List of works.
	Items []AppliedWork `json:"items"`
}

// AppliedWorkSpec represents the desired configuration of AppliedWork
type AppliedWorkSpec struct {
	// WorkName represents the name of the related work on the hub.
	WorkName string `json:"workName"`
	// WorkNamespace represents the namespace of the related work on the hub.
	WorkNamespace string `json:"workNamespace"`
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
```

The applied workload resource will have a owner reference pointing at the associated AppliedWork. For example:

```yaml
apiVersion: v1
kind: Pod
metadata:
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
