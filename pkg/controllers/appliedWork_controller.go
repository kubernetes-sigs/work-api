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
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	workapi "sigs.k8s.io/work-api/pkg/apis/v1alpha1"
)

// AppliedWorkReconciler reconciles an AppliedWork object
type AppliedWorkReconciler struct {
	appliedResourceTracker
	clusterNameSpace string
}

// Reconcile implement the control loop logic for AppliedWork object.
func (r *AppliedWorkReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	klog.InfoS("applied work reconcile loop triggered", "item", req.NamespacedName)
	nsWorkName := req.NamespacedName
	nsWorkName.Namespace = r.clusterNameSpace
	_, appliedWork, err := r.fetchWorks(ctx, nsWorkName)
	if err != nil {
		return ctrl.Result{}, err
	}
	// work has been garbage collected, stop the periodic check if it's gone
	if appliedWork == nil {
		return ctrl.Result{}, nil
	}

	_, err = r.collectDisappearedWorks(ctx, appliedWork)
	if err != nil {
		klog.ErrorS(err, "failed to delete all the stale work", "work", req.NamespacedName)
		// we can't proceed to update the applied
		return ctrl.Result{}, err
	}

	// we want to periodically check if what we've applied matches what is recorded
	return ctrl.Result{RequeueAfter: time.Minute}, nil
}

// collectDisappearedWorks returns the list of resource that does not exist in the appliedWork
func (r *AppliedWorkReconciler) collectDisappearedWorks(
	ctx context.Context, appliedWork *workapi.AppliedWork) ([]workapi.AppliedResourceMeta, error) {
	var errs []error
	var disappearedWorks, newRes []workapi.AppliedResourceMeta
	workUIDChanged := false
	for _, resourceMeta := range appliedWork.Status.AppliedResources {
		gvr := schema.GroupVersionResource{
			Group:    resourceMeta.Group,
			Version:  resourceMeta.Version,
			Resource: resourceMeta.Resource,
		}
		obj, err := r.spokeDynamicClient.Resource(gvr).Namespace(resourceMeta.Namespace).Get(ctx, resourceMeta.Name, metav1.GetOptions{})
		if err != nil {
			if errors.IsNotFound(err) {
				klog.InfoS("found a disappeared work", "work", resourceMeta)
				disappearedWorks = append(disappearedWorks, resourceMeta)
			} else {
				klog.ErrorS(err, "failed to get a work", "work", resourceMeta)
				errs = append(errs, err)
			}
		} else {
			if resourceMeta.UID != obj.GetUID() {
				workUIDChanged = true
				if len(resourceMeta.UID) != 0 {
					klog.InfoS("found a re-created work", "work",
						resourceMeta, "old UID", resourceMeta.UID, "new UID", obj.GetUID())
				} else {
					klog.InfoS("attach to a newly created work", "work",
						resourceMeta, "new UID", obj.GetUID())
				}
			}
			// set the UID back
			resourceMeta.UID = obj.GetUID()
			newRes = append(newRes, resourceMeta)
		}
	}

	if workUIDChanged && len(errs) == 0 {
		appliedWork.Status.AppliedResources = newRes
		klog.InfoS("Update an appliedWork status with new object UID", "work", appliedWork.GetName())
		if err := r.spokeClient.Status().Update(ctx, appliedWork, &client.UpdateOptions{}); err != nil {
			klog.ErrorS(err, "update appliedWork status failed", "appliedWork", appliedWork.GetName())
			return disappearedWorks, err
		}
	}

	return disappearedWorks, utilerrors.NewAggregate(errs)

}

// SetupWithManager wires up the controller.
func (r *AppliedWorkReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).For(&workapi.AppliedWork{}).Complete(r)
}
