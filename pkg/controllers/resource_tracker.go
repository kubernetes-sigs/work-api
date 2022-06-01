package controllers

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	workapi "sigs.k8s.io/work-api/pkg/apis/v1alpha1"
)

type appliedResourceTracker struct {
	hubClient          client.Client
	spokeClient        client.Client
	spokeDynamicClient dynamic.Interface
	restMapper         meta.RESTMapper
}

// Reconcile the difference between the work status/appliedWork status/what is on the member cluster
// work.status represents what should be on the member cluster (it cannot be empty, we will reject empty work)
// appliedWork.status represents what was on the member cluster (it's okay for it to be empty)
// Objects in the appliedWork.status but not in the work.status should be removed from the member cluster.
// We then go through all the work.status manifests whose condition is successfully applied
// For each of them, we check if the object exists in the member cluster, if not, we recreate it according to the original manifest
// We insert it into the new appliedWork.Status

func (r *appliedResourceTracker) fetchWorks(ctx context.Context, nsWorkName types.NamespacedName) (*workapi.Work, *workapi.AppliedWork, error) {
	work := &workapi.Work{}
	appliedWork := &workapi.AppliedWork{}

	// fetch work CR from the member cluster
	err := r.hubClient.Get(ctx, nsWorkName, work)
	switch {
	case errors.IsNotFound(err):
		klog.InfoS("work does not exist", "item", nsWorkName)
		work = nil
	case err != nil:
		klog.ErrorS(err, "failed to get work", "item", nsWorkName)
		return nil, nil, err
	default:
		klog.V(8).InfoS("work exists in the hub cluster", "item", nsWorkName)
	}

	// fetch appliedWork CR from the member cluster
	err = r.spokeClient.Get(ctx, nsWorkName, appliedWork)
	switch {
	case errors.IsNotFound(err):
		klog.InfoS("appliedWork does not exist", "item", nsWorkName)
		appliedWork = nil
	case err != nil:
		klog.ErrorS(err, "failed to get appliedWork", "item", nsWorkName)
		return nil, nil, err
	default:
		klog.V(8).InfoS("appliedWork exists in the member cluster", "item", nsWorkName)
	}

	if err := checkConsistentExist(work, appliedWork, nsWorkName); err != nil {
		klog.ErrorS(err, "applied/work object existence not consistent", "item", nsWorkName)
		return nil, nil, err
	}

	return work, appliedWork, nil
}

func checkConsistentExist(work *workapi.Work, appliedWork *workapi.AppliedWork, workName types.NamespacedName) error {
	// work already deleted
	if work == nil && appliedWork != nil {
		return fmt.Errorf("work finalizer didn't delete the appliedWork %s", workName)
	}
	// we are triggered by appliedWork change or work update so the appliedWork should already be here
	if work != nil && appliedWork == nil {
		return fmt.Errorf("work controller didn't create the appliedWork %s", workName)
	}
	if work == nil && appliedWork == nil {
		klog.InfoS("both applied and work are garbage collected", "item", workName)
	}
	return nil
}

func (r *appliedResourceTracker) decodeUnstructured(manifest workapi.Manifest) (schema.GroupVersionResource, *unstructured.Unstructured, error) {
	unstructuredObj := &unstructured.Unstructured{}
	err := unstructuredObj.UnmarshalJSON(manifest.Raw)
	if err != nil {
		return schema.GroupVersionResource{}, nil, fmt.Errorf("Failed to decode object: %w", err)
	}
	mapping, err := r.restMapper.RESTMapping(unstructuredObj.GroupVersionKind().GroupKind(), unstructuredObj.GroupVersionKind().Version)
	if err != nil {
		return schema.GroupVersionResource{}, nil, fmt.Errorf("Failed to find gvr from restmapping: %w", err)
	}

	return mapping.Resource, unstructuredObj, nil
}
