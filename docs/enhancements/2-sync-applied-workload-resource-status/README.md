# Sync applied workload resource status

## Release Signoff Checklist

- [ ] Enhancement is `implementable`
- [ ] Design details are appropriately documented from clear requirements
- [ ] Test plan is defined
- [ ] Graduation criteria for dev preview, tech preview, GA
- [ ] User-facing documentation is created in [website](https://github.com/kubernetes-sigs/work-api)

## Summary

In most cases, the user or operand on the hub cluster wants to know the real time status of an applied workload resource in the managed clusters. Use case examples:

 1. A user would like to know the status of a workload resource applied to the managed cluster without having to directly accessing the managed cluster, e.g. the user wants to know how many replicas are running in a deployment applied in the managed cluster using just the hub cluster.
 2. A higher level controller would require the resource status to be collected and synced back to the hub cluster `Work` CR. Then it can aggregate the collected statuses and return a status summary to the user of the platform.
 3. A workload orchestrator would dispatch multiple dependent jobs as a pipeline or DAG across multiple managed clusters. It needs to collect the statuses of the deployed jobs on the hub cluster to decide the next step.

## Motivation

The current `Work` API lacks the ability to collect resource status of an applied workload resource on the managed cluster and sync it back to the hub cluster. This proposal provides a common approach for users or controllers on the hub cluster to collect the workload resources statues applied by the `Work` API.

The straightforward approach is to return status of an arbitrary resource to the hub cluster by using the `runtime.RawExtension` type to capture the entire status. However, it is not easy to manage such an untyped structure object. Also, the size of the whole resource status field can be quite large to be put in the `Work` CR which will eventually leads to scalability problems.

In most use cases, the users or controllers on the hub cluster only care about certain fields of an applied resource status. It may make more sense to explicitly specify the fields of the status in `Work` API spec, and the work agent will only return the value of these fields.

### Goals

## Proposal

We propose to update `Work` API so the user can specify the status fields of the resources applied by `Work` CR. The work-agent will sync these status fields back to the `Work` CR.

### Design Details

#### API change  

Add a new field under `Manifest`:

```go
// Manifest represents a resource to be deployed on spoke cluster
type Manifest struct {
	...

	// FeedBackRules defines what resource status field should be returned.
	// +optional
	StatusFeedbackRules  []StatusFeedbackRule `json:"statusFeedbackRules"`
}

type StatusFeedbackRule struct {
	// Type defines the option of how status can be returned.
	// It can be 'JsonPaths' only or 'CommonFields'.
	// If the type is 'JsonPaths', user should specify the jsonPaths field
	// If the type is CommonFields, certain common fields of status will be reported,
	// including 'Replicas', 'ReadyReplicas', and 'AvailableReplicas'. If these status fields
	// do not exist, no values will be reported.
	// +required
	Type string `json:"type"`

	// JsonPaths defines the status fields to be synced for a manifest
	// A jsonPaths will be updated in certain manifest status of the work.status.
	JsonPaths []JsonPath `json:"jsonPaths,omitempty"`
}

type JsonPath struct {
	// name represents the alias name for this field
	// +required
	Name string `json:"name"`
	// JsonPaths represents the json path of the field under status.
	// The path must point to a field with single value in the type of 'integer', 'bool' or 'string'.
	// If the path points to a structure, map or slice, no value will be returned in Work.
	// Refer to https://kubernetes.io/docs/reference/kubectl/jsonpath/ for more information.
	// +required
	Path string `json:"path"`
}
```

An example of `Work` CR to deploy a deployment and return `availableReplica` and `available` condition:

```yaml
apiVersion: multicluster.x-k8s.io/v1alpha1
kind: Work
metadata:
  name: test-work
  namespace: default
spec:
  workload:
    manifests:
    - apiVersion: apps/v1
      kind: Deployment
      metadata:
        name: test-nginx
        namespace: default
      spec:
        replicas: 2
        selector:
          matchLabels:
            app: test-nginx
        strategy: {}
        template:
          metadata:
            creationTimestamp: null
            labels:
              app: test-nginx
          spec:
            containers:
            - image: nginx:1.14.2
              name: nginx
              ports:
              - containerPort: 80
              resources: {}
      statusFeedbackRules:
      - type: CommonFields
      - type: JsonPaths
        jsonPaths:
        - name: availableCondition
          path: .conditions[?(@.type=="Available")].status   
```

The status of `Work` will be updated to include:

```go
// ManifestCondition represents the conditions of the resources deployed on a
// managed cluster.
type ManifestCondition struct {
...
	// StatusFeedback represents the values of the feild synced back defined in statusFeedbackRules
	// +optional
	StatusFeedbacks StatusFeedbackResult `json:"statusFeedback"`
}

type StatusFeedbackResult struct {
  // Values represents the synced value of the feedback field.
	// +listType:=map
	// +listMapKey:=name
	// +optional
	Values []FeedbackValue `json:"values,omitempty"`
}

type FeedbackValue struct {
    // name represents the aliase name for this field
	// +required
	Name string `json:"name"`

    // value represents the value of the field
	Value FieldValue `json:"fieldValue"`
}

type FieldValue struct {
  // Type is the type of the value, it can by integer, string or boolean
	Type ValueType `json:"type"`

	// +optional
	Integer *int64 `json:"integer,omitempty"`

	// +optional
	String *string `json:"string,omitempty"`

	// +optional
	Boolean *bool `json:"boolean,omitempty"`
}
```

An example of a return applied workload resource feedback status by the work agent:

```yaml
manifestCondition:
  - identifier:
      group: apps
      kind: Deployment
      name: test-nginx
      namespace: default
	  version: v1
    statusFeeback:
      values:
      - fieldValue:
          integer: 1
          type: integer
        name: availableReplica
      - fieldValue:
          integer: 1
          type: integer
        name: readyReplica
      - fieldValue:
          integer: 1
          type: integer
        name: replica
      - fieldValue:
          string: "True"
          type: string
        name: availableCondition
```

The returned value must be a scalar value, and the work agent should check the type of the returned value. The work agent should treat the status that a value "IsNotFound" or "TypeMismatch" separately. If the path of the `feedbackValue` is not found in the status of the resource, then the value should be ignored. If the path of the `feedbackValue` is valid, but the type of the value is not scalar, e.g a list or map, the condition of "StatusFeedbackAvailable" should be set false and a message shoud be added to indicate that which `feedbackValue` cannot be obtained.

#### Status update frequency

Ideally, the work agent should have na informer for each applied resource to update the `feedbackValue`, but this will cause the agent to manage too many informers and will also result in frequent update on status of `Work`.
To compromise, we will start with a periodic update. User can specify the update frequency of `feedbackValues` by setting a
`--status-update-frequency` flag on the work agent. we should consider make it configurable for different resource in the API spec in the future.

### Test Plan

E2E tests will be added to cover cases including:
- Invalid status fields.
- Status field updates.
- Add, update or remove an feedback field

### Graduation Criteria
N/A

### Upgrade Strategy
It will need upgrade on CRD of Work on hub cluster, and upgrade of work agent on managed cluster.

### Version Skew Strategy
- The StatusFeedback field is optional, and if it is not set, the `Work` CR can be correctly treated by work agent with elder version.
- The elder version work agent will ignore the `StatusFeedback` field.

## Alternatives

### Return raw status data of the applied resource.
Add a `Status` field with the type `runtime.RawExtension` to return the entire status of an applied workload resource status.

```go
// ManifestCondition represents the conditions of the resources deployed on
// spoke cluster
type ManifestCondition struct {
	...

	// Status reflects the running status of the current manifest.
	// +kubebuilder:pruning:PreserveUnknownFields
	Status runtime.RawExtension `json:",inline"`
}
```

An example, using the new `Status` field in `ManifestCondition`

```yaml
...
manifestCondition:
  - identifier:
      group: apps
      kind: Deployment
      name: nginx
      namespace: default
      version: v1
    status:
      availableReplicas: 1
      conditions:
      - lastTransitionTime: "2021-01-29T03:41:58Z"
        lastUpdateTime: "2021-01-29T03:41:58Z"
        message: Deployment has minimum availability.
        reason: MinimumReplicasAvailable
        status: "True"
        type: Available
      - lastTransitionTime: "2021-01-29T03:41:48Z"
        lastUpdateTime: "2021-01-29T03:41:58Z"
        message: ReplicaSet "nginx-6799fc88d8" has successfully progressed.
        reason: NewReplicaSetAvailable
        status: "True"
        type: Progressing
      observedGeneration: 1
      readyReplicas: 1
      replicas: 1
      updatedReplicas: 1
...
```

The advantage of this approach is that it might be more user friendly since the status data will be the same as status of the applied workload resource.
The disadvantage of this approach is mainly scalability concerns. This implementation might generate many large data payload on the hub cluster and creates many update calls. It may also result in many unnecessary control loop in the controllers on the hub cluster when watching the `Work` CR.

### Directly access apiserver of managed cluster.

User or operand can directly access the API server the of managed cluster or access via proxy (e.g. [clusternet](https://github.com/clusternet/clusternet)) to get the applied workload resource status. However, it will requires the controllers on the hub to watch multiple API servers on the managed clusters. It also needs the hub cluster to maintain credentials for each managed cluster, and watching resources across multiple cluster might also leads to scalability problems as well.

### References
Some of the ideas in this proposal are taken from the following sources:

1. [OCM ManifestWork API Status Feedback Enhancement](https://github.com/open-cluster-management-io/enhancements/tree/main/enhancements/sig-architecture/29-manifestwork-status-feedback)
2. [Manifest Status Collection (By @RainbowMango)](https://docs.google.com/document/d/1cWcdB40pGg3KS1eSyb9Q6SIRvWVI8dEjFp9RI0Gk0vg/edit#heading=h.s0elqtz875mn)
