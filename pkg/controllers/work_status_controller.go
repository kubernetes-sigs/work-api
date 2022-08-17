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
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	workapi "sigs.k8s.io/work-api/pkg/apis/v1alpha1"
	"time"
)

// WorkStatusReconciler reconciles a Work object when its status changes
type WorkStatusReconciler struct {
	appliedResourceTracker
	recorder    record.EventRecorder
	concurrency int
	joined      bool
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
		joined:      joined,
	}
}

// Reconcile implement the control loop logic for Work Status.
func (r *WorkStatusReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	if !r.joined {
		klog.V(3).InfoS("workStatus controller is not started yet, will requeue the request")
		return ctrl.Result{RequeueAfter: time.Second * 5}, nil
	}
	klog.InfoS("workStatus controller reconcile loop is triggered", "item", req.NamespacedName)

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
	newRes, staleRes, genErr := r.generateDiff(ctx, work, appliedWork)
	if genErr != nil {
		klog.ErrorS(err, "failed to generate the diff between work status and appliedWork status", work.Kind, kLogObjRef)
		return ctrl.Result{}, err
	}
	// delete all the manifests that should not be in the cluster.
	if err = r.deleteStaleManifest(ctx, staleRes); err != nil {
		klog.ErrorS(err, "resource garbage-collection incomplete; some Work owned resources could not be deleted", work.Kind, kLogObjRef)
		// we can't proceed to update the applied
		return ctrl.Result{}, err
	} else if len(staleRes) > 0 {
		klog.V(3).InfoS("successfully garbage-collected all stale manifests", work.Kind, kLogObjRef, "number of GCed res", len(staleRes))
		for _, res := range staleRes {
			klog.V(5).InfoS("successfully garbage-collected a stale manifest", work.Kind, kLogObjRef, "res", res)
		}
	}

	// update the appliedWork with the new work after the stales are deleted
	appliedWork.Status.AppliedResources = newRes
	if err = r.spokeClient.Status().Update(ctx, appliedWork, &client.UpdateOptions{}); err != nil {
		klog.ErrorS(err, "failed to update appliedWork status", appliedWork.Kind, appliedWork.GetName())
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// generateDiff check the difference between what is supposed to be applied  (tracked by the work CR status)
// and what was applied in the member cluster (tracked by the appliedWork CR).
// What is in the `appliedWork` but not in the `work` should be deleted from the member cluster
// What is in the `work` but not in the `appliedWork` should be added to the appliedWork status
func (r *WorkStatusReconciler) generateDiff(ctx context.Context, work *workapi.Work, appliedWork *workapi.AppliedWork) ([]workapi.AppliedResourceMeta, []workapi.AppliedResourceMeta, error) {
	var staleRes, newRes []workapi.AppliedResourceMeta
	// for every resource applied in cluster, check if it's still in the work's manifest condition
	for _, resourceMeta := range appliedWork.Status.AppliedResources {
		resStillExist := false
		for _, manifestCond := range work.Status.ManifestConditions {
			if isSameResource(resourceMeta, manifestCond.Identifier) {
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
				if isSameResource(resourceMeta, manifestCond.Identifier) {
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

func (r *WorkStatusReconciler) deleteStaleManifest(ctx context.Context, staleManifests []workapi.AppliedResourceMeta) error {
	var errs []error

	for _, staleManifest := range staleManifests {
		gvr := schema.GroupVersionResource{
			Group:    staleManifest.Group,
			Version:  staleManifest.Version,
			Resource: staleManifest.Resource,
		}
		err := r.spokeDynamicClient.Resource(gvr).Namespace(staleManifest.Namespace).
			Delete(ctx, staleManifest.Name, metav1.DeleteOptions{})
		if err != nil && !apierrors.IsNotFound(err) {
			klog.ErrorS(err, "failed to delete the staled manifest", "manifest", staleManifest)
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

// UpdateOnlyPredicate is used since we only need to process the update event
type UpdateOnlyPredicate struct {
	predicate.Funcs
}

func (UpdateOnlyPredicate) Create(event.CreateEvent) bool {
	return false
}

func (UpdateOnlyPredicate) Delete(event.DeleteEvent) bool {
	return false
}

func (UpdateOnlyPredicate) Update(e event.UpdateEvent) bool {
	if e.ObjectOld == nil {
		return false
	}
	if e.ObjectNew == nil {
		return false
	}

	return e.ObjectNew.GetResourceVersion() != e.ObjectOld.GetResourceVersion()
}
