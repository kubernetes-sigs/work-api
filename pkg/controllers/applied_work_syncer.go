/*
Copyright 2021 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/klog/v2"

	workapi "sigs.k8s.io/work-api/pkg/apis/v1alpha1"
)

// generateDiff check the difference between what is supposed to be applied  (tracked by the work CR status)
// and what was applied in the member cluster (tracked by the appliedWork CR).
// What is in the `appliedWork` but not in the `work` should be deleted from the member cluster
// What is in the `work` but not in the `appliedWork` should be added to the appliedWork status
func (r *ApplyWorkReconciler) generateDiff(ctx context.Context, work *workapi.Work, appliedWork *workapi.AppliedWork) ([]workapi.AppliedResourceMeta, []workapi.AppliedResourceMeta, error) {
	var staleRes, newRes []workapi.AppliedResourceMeta
	// for every resource applied in cluster, check if it's still in the work's manifest condition
	for _, resourceMeta := range appliedWork.Status.AppliedResources {
		resStillExist := false
		for _, manifestCond := range work.Status.ManifestConditions {
			if resourceMeta.ResourceIdentifier == manifestCond.Identifier {
				resStillExist = true
				break
			}
		}
		if !resStillExist {
			klog.V(5).InfoS("find an orphaned resource in the member cluster",
				"parent resource", work.GetName(), "orphaned resource", resourceMeta.ResourceIdentifier)
			staleRes = append(staleRes, resourceMeta)
		}
	}
	// add every resource in the work's manifest condition that is applied successfully back to the appliedWork status
	for _, manifestCond := range work.Status.ManifestConditions {
		ac := meta.FindStatusCondition(manifestCond.Conditions, ConditionTypeApplied)
		if ac == nil {
			// should not happen
			klog.ErrorS(fmt.Errorf("resource is missing  applied condition"), "applied condition missing", "resource", manifestCond.Identifier)
			continue
		}
		// we only add the applied one to the appliedWork status
		if ac.Status == metav1.ConditionTrue {
			resRecorded := false
			// we keep the existing resourceMeta since it has the UID
			for _, resourceMeta := range appliedWork.Status.AppliedResources {
				if resourceMeta.ResourceIdentifier == manifestCond.Identifier {
					resRecorded = true
					newRes = append(newRes, resourceMeta)
					break
				}
			}
			if !resRecorded {
				klog.V(5).InfoS("discovered a new resource",
					"parent Work", work.GetName(), "discovered resource", manifestCond.Identifier)
				obj, err := r.spokeDynamicClient.Resource(schema.GroupVersionResource{
					Group:    manifestCond.Identifier.Group,
					Version:  manifestCond.Identifier.Version,
					Resource: manifestCond.Identifier.Resource,
				}).Namespace(manifestCond.Identifier.Namespace).Get(ctx, manifestCond.Identifier.Name, metav1.GetOptions{})
				switch {
				case apierrors.IsNotFound(err):
					klog.V(4).InfoS("the manifest resource is deleted", "manifest", manifestCond.Identifier)
					continue
				case err != nil:
					klog.ErrorS(err, "failed to retrieve the manifest", "manifest", manifestCond.Identifier)
					return nil, nil, err
				}
				newRes = append(newRes, workapi.AppliedResourceMeta{
					ResourceIdentifier: manifestCond.Identifier,
					UID:                obj.GetUID(),
				})
			}
		}
	}
	return newRes, staleRes, nil
}

func (r *ApplyWorkReconciler) deleteStaleManifest(ctx context.Context, staleManifests []workapi.AppliedResourceMeta, owner metav1.OwnerReference) error {
	var errs []error

	for _, staleManifest := range staleManifests {
		gvr := schema.GroupVersionResource{
			Group:    staleManifest.Group,
			Version:  staleManifest.Version,
			Resource: staleManifest.Resource,
		}
		uObj, err := r.spokeDynamicClient.Resource(gvr).Namespace(staleManifest.Namespace).
			Get(ctx, staleManifest.Name, metav1.GetOptions{})
		existingOwners := uObj.GetOwnerReferences()
		newOwners := make([]metav1.OwnerReference, 0)
		found := false
		for index, r := range existingOwners {
			if isReferSameObject(r, owner) {
				found = true
				newOwners = append(newOwners, existingOwners[:index]...)
				newOwners = append(newOwners, existingOwners[index+1:]...)
			}
		}
		if !found {
			klog.ErrorS(err, "the stale manifest is not owned by this work, skip", "manifest", staleManifest, "owner", owner)
			continue
		}
		if len(newOwners) == 0 {
			klog.V(2).InfoS("delete the staled manifest", "manifest", staleManifest, "owner", owner)
			err := r.spokeDynamicClient.Resource(gvr).Namespace(staleManifest.Namespace).
				Delete(ctx, staleManifest.Name, metav1.DeleteOptions{})
			if err != nil && !apierrors.IsNotFound(err) {
				klog.ErrorS(err, "failed to delete the staled manifest", "manifest", staleManifest, "owner", owner)
				errs = append(errs, err)
			}
		} else {
			klog.V(2).InfoS("remove the owner reference from the staled manifest", "manifest", staleManifest, "owner", owner)
			uObj.SetOwnerReferences(newOwners)
			_, err := r.spokeDynamicClient.Resource(gvr).Namespace(staleManifest.Namespace).Update(ctx, uObj, metav1.UpdateOptions{FieldManager: workFieldManagerName})
			if err != nil {
				klog.ErrorS(err, "failed to remove the owner reference from manifest", "manifest", staleManifest, "owner", owner)
				errs = append(errs, err)
			}
		}
	}
	return utilerrors.NewAggregate(errs)
}
