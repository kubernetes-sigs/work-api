# Sync applied workload resource status

## Release Signoff Checklist

- [ ] Enhancement is `implementable`
- [ ] Design details are appropriately documented from clear requirements
- [ ] Test plan is defined
- [ ] Graduation criteria for dev preview, tech preview, GA
- [ ] User-facing documentation is created in [website](https://github.com/kubernetes-sigs/work-api)

## Summary

In most cases, the user or operand on the hub cluster wants to know the real time status of an applied workload resource in the managed clusters. Use case examples:

 1. A user would like to know the status of a workload resource applied to the managed cluster without having to directly access the managed cluster, e.g. the user wants to know how many replicas are running in a deployment applied in the managed cluster using just the hub cluster.
 2. A higher level controller would require the resource status to be collected and synced back to the hub cluster `Work` CR. Then it can aggregate the collected statuses and return a status summary to the user of the platform.
 3. A workload orchestrator would dispatch multiple dependent jobs as a pipeline or DAG across multiple managed clusters. It needs to collect the statuses of the deployed jobs on the hub cluster to decide the next step.

 This proposal provides a common approach for users or controllers on the hub cluster to collect the workload resources statuses applied by the `Work` API.

## Motivation

The current `Work` API lacks the ability to collect resource status of an applied workload resource on the managed cluster and sync it back to the hub cluster.

The straightforward approach is to return the entire status of an applied workload resource to the hub cluster by adding a `runtime.RawExtension` field to store the status. However, it is not easy to manage such an untyped structure object. Also, the size of the whole resource status field can be quite large, which will eventually lead to scalability problems.

In most use cases, the users or controllers on the hub cluster only care about certain fields of an applied resource status. It may make more sense to explicitly specify the fields of the status in the `Work` API spec, and the work controller will only return the value of these fields.

### Goals

- Update the `Work` API to optionally enable the resource status sync.
- Update the `Work` API to optionally store the resource status sync values.

### Non-Goals

## Proposal

We propose to update the `Work` API so the user can optionally specify the status sync rules of the resources applied by the work controller. The work controller will sync status fields based on the sync rules back to the `Work` CR.

### Design Details

#### API change  

Add and update the follow:

```go
// WorkSpec defines the desired state of Work
type WorkSpec struct {
  ...
	// WorkloadConfig represents the configuration of workload defined in workload field.
	// +optional
	WorkloadConfig WorkloadConfiguration `json:"workloadConfig,omitempty"`
}

// WorkloadConfiguration represents the the configuration of workload defined in workload field.
type WorkloadConfiguration struct {
	// ManifestConfigs represents the configuration of manifest defined in workload field.
	// +optional
	ManifestConfigs []ManifestConfigOption `json:"manifestConfigs,omitempty"`
}

// ManifestConfigOption represents the configuration of a manifest defined in workload field.
type ManifestConfigOption struct {
	// ResourceIdentifier represents the group, resource, name and namespace of a resoure.
	// If this references a resource not created by this work,
	// then the related configurations will not be applied.
	// +kubebuilder:validation:Required
	// +required
	ResourceIdentifier ResourceIdentifier `json:"resourceIdentifier"`

	// StatusSyncRules defines what resource status field should be returned.
	// +kubebuilder:validation:Required
	// +required
	StatusSyncRules []StatusSyncRule `json:"statusSyncRules"`
}

// StatusSyncRule represents a resource status field should be returned.
type StatusSyncRule struct {
	// Type defines the option of how status can be returned.
	// It can be JSONPaths or CommonFields.
	// If the type is JSONPaths, user should specify the jsonPaths field
	// If the type is CommonFields, certain common fields of status defined by a rule only
	// for types in in k8s.io/api will be reported,
	// If these status fields do not exist, no values will be reported.
	// +kubebuilder:validation:Required
	// +required
	Type SyncType `json:"type"`

	// JsonPaths defines the json path under status field to be synced.
	// +optional
	JsonPaths []JsonPath `json:"jsonPaths,omitempty"`
}

// SyncType represents the option of how status can be returned.
// +kubebuilder:validation:Enum=CommonFields;JSONPaths
type SyncType string

const (
	// CommonFieldsType represents that values of some common status fields will be returned, which
	// is reflected with a hardcoded rule only for types in k8s.io/api.
	CommonFieldsType SyncType = "CommonFields"

	// JSONPathsType represents that values of status fields with certain json paths specified will be
	// returned
	JSONPathsType SyncType = "JSONPaths"
)

// JsonPath represents a status field to be synced for a manifest using json path.
type JsonPath struct {
	// Name represents the alias name for this field
	// +kubebuilder:validation:Required
	// +required
	Name string `json:"name"`

	// Version is the version of the Kubernetes resource.
	// If it is not specified, the resource with the semantically latest version is
	// used to resolve the path.
	// +optional
	Version string `json:"version,omitempty"`

	// Path represents the json path of the field under status.
	// The path must point to a field with single value in the type of integer, bool or string.
	// If the path points to a non-existing field, no value will be returned.
	// If the path points to a structure, map or slice, no value will be returned and the status conddition
	// of 'StatusSynced' will be set as false.
	// Ref to https://kubernetes.io/docs/reference/kubectl/jsonpath/ on how to write a jsonPath.
	// +kubebuilder:validation:Required
	// +required
	Path string `json:"path"`
}
```

An example of `Work` CR to deploy a deployment and sync rules to return resource status data for:
- `isAvailable` value using`JSONPaths` sync rule type with custom json path.
- `ReadyReplicas`, `Replicas`, and `AvailableReplicas` values using `CommonFields` pre-defined sync rule type.

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
  workloadConfig:
    manifestConfigs:
      - resourceIdentifier:
          group: apps
          kind: Deployment
          version: v1
        statusSyncRules:
        - type: CommonFields
        - type: JSONPaths
          jsonPaths:
          - name: isAvailable
            path: '.status.conditions[?(@.type=="Available")].status'
```

The status of `Work` will be updated to include:

```go
// ManifestCondition represents the conditions of the resources deployed on a
// managed cluster.
type ManifestCondition struct {
...
	// StatusSync represents the values of the field synced back defined in statusSyncRules
	// +optional
	StatusSync StatusSyncResult `json:"statusSync,omitempty"`
}

// StatusSyncResult represents the values of the field synced back defined in statusSyncRules
type StatusSyncResult struct {
	// Values represents the synced value of the interested field.
	// +listType:=map
	// +listMapKey:=name
	// +optional
	Values []SyncValue `json:"values,omitempty"`
}

// SyncValue represents the synced value of the sync field.
type SyncValue struct {
	// Name represents the alias name for this field. It is the same as what is specified
	// in StatusSyncRule in the spec.
	// +kubebuilder:validation:Required
	// +required
	Name string `json:"name"`

	// Value is the value of the status field.
	// The value of the status field can only be integer, string or boolean.
	// +kubebuilder:validation:Required
	// +required
	Value FieldValue `json:"fieldValue"`
}

// FieldValues represents the value of the field
// The value of the status field can only be integer, string or boolean.
type FieldValue struct {
	// Type represents the type of the value, it can be integer, string or boolean.
	// +kubebuilder:validation:Required
	// +required
	Type ValueType `json:"type"`

	// Integer is the integer value when type is integer.
	// +optional
	Integer *int64 `json:"integer,omitempty"`

	// String is the string value when when type is string.
	// +optional
	String *string `json:"string,omitempty"`

	// Boolean is bool value when type is boolean.
	// +optional
	Boolean *bool `json:"boolean,omitempty"`
}

// Type represents the type of the value, it can by integer, string or bool
// +kubebuilder:validation:Enum=Integer;String;Boolean
type ValueType string

const (
	Integer ValueType = "Integer"
	String  ValueType = "String"
	Boolean ValueType = "Boolean"
)
```

An example of a return applied workload resource sync status by the work controller:

```yaml
  manifestConditions:
  - conditions:
    - lastTransitionTime: "2022-04-29T19:39:46Z"
      message: Apply manifest complete
      observedGeneration: 1
      reason: AppliedManifestComplete
      status: "True"
      type: Applied
    - lastTransitionTime: "2022-04-29T19:39:50Z"
      message: Resource is available
      reason: ResourceAvailable
      status: "True"
      type: Available
    - lastTransitionTime: "2022-04-29T19:39:50Z"
      message: ""
      reason: StatusSynced
      status: "True"
      type: StatusSynced
    identifier:
      group: apps
      kind: Deployment
      name: test-nginx
      namespace: default
      resource: deployments
      version: v1
    statusSync:
      values:
      - fieldValue:
          integer: 2
          type: Integer
        name: ReadyReplicas
      - fieldValue:
          integer: 2
          type: Integer
        name: Replicas
      - fieldValue:
          integer: 2
          type: Integer
        name: AvailableReplicas
      - fieldValue:
          string: "True"
          type: String
        name: isAvailable
```

The returned value must be a scalar value, and the work controller should check the type of the returned value. The work controller should treat the status of a value "IsNotFound" or "TypeMismatch" separately. If the path of the `syncValue` is not found in the status of the resource, then the value should be ignored. If the path of the `syncValue` is valid, but the type of the value is not scalar, e.g a list or map, the condition of "StatusSyncAvailable" should be set false and a message should be added to indicate that which `syncValue` cannot be obtained.

#### Status update frequency

Ideally, the work controller should have an informer for each applied resource to update the `syncValue`, but this will cause the controller to manage too many informers and will also result in frequent updates on status of `Work`.
To compromise, we will start with a periodic update. User can specify the update frequency of `syncValues` by setting a
`--status-update-frequency` flag on the work controller. we should consider making it configurable for different resources in the API spec in the future.

### Test Plan

E2E tests will be added to cover cases including:
- Invalid status fields.
- Status field updates.
- Add, update or remove an sync field

### Graduation Criteria
N/A

### Upgrade Strategy
It will need an upgrade on `Work` CRD on the hub cluster, and upgrade of the work controller on managed cluster.

### Version Skew Strategy
- The `StatusSync` field is optional, and if it is not set, the `Work` CR can be correctly treated by the work controller with the older version.
- The older version work controller will ignore the `StatusSync` field.

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
```

The advantage of this approach is that it might be more user friendly since the status data will be the same as the status of the applied workload resource.
The disadvantage of this approach is mainly scalability concerns. This implementation might generate many large data payloads on the hub cluster and create many update calls. It may also result in many unnecessary control loops in the controllers on the hub cluster when watching the `Work` CR.

### Directly access apiserver of managed cluster.

User or operand can directly access the API server of the managed cluster or access via proxy (e.g. [clusternet](https://github.com/clusternet/clusternet)) to get the applied workload resource status. However, it will require the controllers on the hub to watch multiple API servers on the managed clusters. It also needs the hub cluster to maintain credentials for each managed cluster, and watching resources across multiple clusters might also lead to scalability problems as well.

### References
Some of the ideas in this proposal are taken from the following sources:

1. [OCM ManifestWork API Status Sync Enhancement](https://github.com/open-cluster-management-io/enhancements/tree/main/enhancements/sig-architecture/29-manifestwork-status-sync)
2. [Manifest Status Collection (By @RainbowMango)](https://docs.google.com/document/d/1cWcdB40pGg3KS1eSyb9Q6SIRvWVI8dEjFp9RI0Gk0vg/edit#heading=h.s0elqtz875mn)
