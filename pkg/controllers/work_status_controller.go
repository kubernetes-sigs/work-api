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
	"github.com/pkg/errors"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"time"

	workapi "sigs.k8s.io/work-api/pkg/apis/v1alpha1"
	"sigs.k8s.io/work-api/pkg/utils"
)

// WorkStatusReconciler reconciles a Work object when its status changes
type WorkStatusReconciler struct {
	appliedResourceTracker
	recorder    record.EventRecorder
	concurrency int
	Joined      bool
}

func NewWorkStatusReconciler(hubClient client.Client, spokeDynamicClient dynamic.Interface, spokeClient client.Client, restMapper meta.RESTMapper, recorder record.EventRecorder, concurrency int, joined bool) *WorkStatusReconciler {
	return &WorkStatusReconciler{
		appliedResourceTracker: appliedResourceTracker{
			hubClient:          hubClient,
			spokeClient:        spokeClient,
			spokeDynamicClient: spokeDynamicClient,
			restMapper:         restMapper,
		},
		recorder:    recorder,
		concurrency: concurrency,
		Joined:      joined,
	}
}

// Reconcile implement the control loop logic for Work Status.
func (r *WorkStatusReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	klog.InfoS("Work Status controller reconcile loop triggered", "item", req.NamespacedName)

	if !r.Joined {
		klog.InfoS("work status controller is not started yet")
		return ctrl.Result{RequeueAfter: time.Second * 5}, fmt.Errorf("work status controller is not started yet")
	}

	work, appliedWork, err := r.fetchWorks(ctx, req.NamespacedName)
	if err != nil {
		return ctrl.Result{}, err
	}
	// work has been garbage collected
	if work == nil {
		return ctrl.Result{}, nil
	}
	kLogObjRef := klog.KObj(work)

	// from now on both work objects should exist
	newRes, staleRes := r.calculateNewAppliedWork(work, appliedWork)
	if err = r.deleteStaleWork(ctx, staleRes); err != nil {
		klog.ErrorS(err, utils.MessageResourceGarbageCollectionIncomplete, work.Kind, kLogObjRef)
		r.recorder.Event(work, v1.EventTypeWarning, utils.EventReasonResourceGarbageCollectionIncomplete, utils.MessageResourceGarbageCollectionIncomplete)

		// we can't proceed to update the applied
		return ctrl.Result{}, err
	} else if len(staleRes) > 0 && err == nil {
		// TODO: Specify which manifest was deleted.
		klog.InfoS(utils.MessageResourceGarbageCollectionComplete, work.Kind, kLogObjRef)
		r.recorder.Event(work, v1.EventTypeNormal, utils.EventReasonResourceGarbageCollectionComplete, utils.MessageResourceGarbageCollectionComplete)
	}

	// update the appliedWork with the new work
	appliedWork.Status.AppliedResources = newRes
	if err = r.spokeClient.Status().Update(ctx, appliedWork, &client.UpdateOptions{}); err != nil {
		klog.ErrorS(err, utils.MessageResourceStatusUpdateFailed, appliedWork.Kind, appliedWork.GetName())
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// calculateNewAppliedWork check the difference between what is supposed to be applied  (tracked by the work CR status)
// and what was applied in the member cluster (tracked by the appliedWork CR).
// What is in the `appliedWork` but not in the `work` should be deleted from the member cluster
// What is in the `work` but not in the `appliedWork` should be added to the appliedWork status
func (r *WorkStatusReconciler) calculateNewAppliedWork(work *workapi.Work, appliedWork *workapi.AppliedWork) ([]workapi.AppliedResourceMeta, []workapi.AppliedResourceMeta) {
	var staleRes, newRes []workapi.AppliedResourceMeta

	for _, resourceMeta := range appliedWork.Status.AppliedResources {
		resStillExist := false
		for _, manifestCond := range work.Status.ManifestConditions {
			if isSameResource(resourceMeta, manifestCond.Identifier) {
				resStillExist = true
				break
			}
		}
		if !resStillExist {
			klog.V(3).InfoS(utils.MessageResourceIsOrphan,
				"parent GVK", work.GetObjectKind().GroupVersionKind(),
				"parent resource", work.GetName(),
				"orphan resource", resourceMeta)

			staleRes = append(staleRes, resourceMeta)
		}
	}

	for _, manifestCond := range work.Status.ManifestConditions {
		ac := meta.FindStatusCondition(manifestCond.Conditions, ConditionTypeApplied)
		if ac == nil {
			err := errors.New(utils.MessageResourceStateInvalid)
			klog.ErrorS(err, utils.MessageResourceIsMissingCondition,
				"resource", manifestCond.Identifier,
				"missing condition", ConditionTypeApplied)
			continue
		}
		// we only add the applied one to the appliedWork status
		if ac.Status == metav1.ConditionTrue {
			resRecorded := false
			// we keep the existing resourceMeta since it has the UID
			for _, resourceMeta := range appliedWork.Status.AppliedResources {
				if isSameResource(resourceMeta, manifestCond.Identifier) {
					resRecorded = true
					newRes = append(newRes, resourceMeta)
					break
				}
			}
			if !resRecorded {
				klog.V(5).InfoS(utils.MessageResourceDiscovered,
					"parent GVK", work.GetObjectKind().GroupVersionKind(),
					"parent Work", work.GetName(),
					"discovered resource", manifestCond.Identifier)

				newRes = append(newRes, workapi.AppliedResourceMeta{
					ResourceIdentifier: manifestCond.Identifier,
				})
			}
		}
	}

	return newRes, staleRes
}

func (r *WorkStatusReconciler) deleteStaleWork(ctx context.Context, staleWorks []workapi.AppliedResourceMeta) error {
	var errs []error

	for _, staleWork := range staleWorks {
		gvr := schema.GroupVersionResource{
			Group:    staleWork.Group,
			Version:  staleWork.Version,
			Resource: staleWork.Resource,
		}
		err := r.spokeDynamicClient.Resource(gvr).Namespace(staleWork.Namespace).
			Delete(ctx, staleWork.Name, metav1.DeleteOptions{})
		if err != nil && !apierrors.IsGone(err) {
			klog.ErrorS(err, utils.MessageResourceDeleteFailed, "resource", staleWork)
			errs = append(errs, err)
		}
	}
	return utilerrors.NewAggregate(errs)
}

// isSameResource checks if an appliedMeta is referring to the same resource that a resourceId is pointing to
func isSameResource(appliedMeta workapi.AppliedResourceMeta, resourceId workapi.ResourceIdentifier) bool {
	return appliedMeta.Resource == resourceId.Resource && appliedMeta.Version == resourceId.Version &&
		appliedMeta.Group == resourceId.Group && appliedMeta.Namespace == resourceId.Namespace &&
		appliedMeta.Name == resourceId.Name
}

// SetupWithManager wires up the controller.
func (r *WorkStatusReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: r.concurrency,
		}).
		For(&workapi.Work{}, builder.WithPredicates(UpdateOnlyPredicate{}, predicate.ResourceVersionChangedPredicate{})).
		Complete(r)
}

// We only need to process the update event
type UpdateOnlyPredicate struct {
	predicate.Funcs
}

func (UpdateOnlyPredicate) Create(event.CreateEvent) bool {
	return false
}

func (UpdateOnlyPredicate) Delete(event.DeleteEvent) bool {
	return false
}
