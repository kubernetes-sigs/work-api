package utils

const (
	WorkFieldManagerName = "work-api-agent"

	EventReasonAppliedWorkCreated                  = "AppliedWorkCreated"
	EventReasonAppliedWorkDeleted                  = "AppliedWorkDeleted"
	EventReasonFinalizerAdded                      = "FinalizerAdded"
	EventReasonFinalizerRemoved                    = "FinalizerRemoved"
	EventReasonReconcileComplete                   = "ReconciliationComplete"
	EventReasonReconcileIncomplete                 = "ReconciliationIncomplete"
	EventReasonResourceGarbageCollectionComplete   = "ResourceGarbageCollectionComplete"
	EventReasonResourceGarbageCollectionIncomplete = "ResourceGarbageCollectionIncomplete"

	MessageManifestApplyFailed                 = "manifest apply failed"
	MessageManifestApplyComplete               = "manifest apply completed"
	MessageManifestApplyIncomplete             = "manifest apply incomplete; the respective Work will be queued again for reconciliation"
	MessageManifestApplySucceeded              = "manifest apply succeeded"
	MessageManifestApplyUnwarranted            = "manifest apply unwarranted; the spec has not changed"
	MessageResourceFinalizerRemoved            = "resource's finalizer removed"
	MessageResourceCreateSucceeded             = "resource create succeeded"
	MessageResourceCreateFailed                = "resource create failed"
	MessageResourceDeleteSucceeded             = "resource delete succeeded"
	MessageResourceDeleting                    = "resource is in the process of being deleted"
	MessageResourceDeleteFailed                = "resource delete failed"
	MessageResourceDiscovered                  = "resource discovered"
	MessageResourceFinalizerAdded              = "resource finalizer added"
	MessageResourceGarbageCollectionComplete   = "resource garbage-collection complete"
	MessageResourceGarbageCollectionIncomplete = "resource garbage-collection incomplete; some Work owned resources could not be deleted"
	MessageResourceIsOrphan                    = "resource is an orphan"
	MessageResourceIsMissingCondition          = "resource is missing condition"
	MessageResourceJSONMarshalFailed           = "resource JSON marshaling failed"
	MessageResourceNotOwnedByWorkAPI           = "resource not owned by Work-API"
	MessageResourcePatchFailed                 = "resource patch failed"
	MessageResourcePatchSucceeded              = "resource patch succeeded"
	MessageResourceRetrieveFailed              = "resource retrieval failed"
	MessageResourceStateInvalid                = "resource state is invalid"
	MessageResourceSpecModified                = "resource spec modified"
	MessageResourceStatusUpdateFailed          = "resource status update failed"
	MessageResourceStatusUpdateSucceeded       = "resource status update succeeded"
)