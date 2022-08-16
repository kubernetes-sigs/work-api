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
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	workv1alpha1 "sigs.k8s.io/work-api/pkg/apis/v1alpha1"
)

// FinalizeWorkReconciler reconciles a Work object for finalization
type FinalizeWorkReconciler struct {
	client      client.Client
	spokeClient client.Client
	recorder    record.EventRecorder
	Joined      bool
}

func NewFinalizeWorkReconciler(hubClient client.Client, spokeClient client.Client, recorder record.EventRecorder, joined bool) *FinalizeWorkReconciler {
	return &FinalizeWorkReconciler{
		client:      hubClient,
		spokeClient: spokeClient,
		recorder:    recorder,
		Joined:      joined,
	}
}

// Reconcile implement the control loop logic for finalizing Work object.
func (r *FinalizeWorkReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	if !r.Joined {
		klog.InfoS("finalize controller is not started yet", "work", req.NamespacedName)
		return ctrl.Result{RequeueAfter: time.Second * 5}, nil
	}
	klog.InfoS("Work finalize controller reconcile loop triggered.", "work", req.NamespacedName)

	work := &workv1alpha1.Work{}
	err := r.client.Get(ctx, types.NamespacedName{Name: req.Name, Namespace: req.Namespace}, work)
	switch {
	case errors.IsNotFound(err):
		return ctrl.Result{}, nil
	case err != nil:
		klog.ErrorS(err, "fail to retrieve  the work resource", "work", req.NamespacedName)
		return ctrl.Result{}, err
	}

	kLogObjRef := klog.KObj(work)

	// cleanup finalizer and resources
	if !work.DeletionTimestamp.IsZero() {
		return r.garbageCollectAppliedWork(ctx, work)
	}

	appliedWork := &workv1alpha1.AppliedWork{}
	if controllerutil.ContainsFinalizer(work, workFinalizer) {
		err = r.spokeClient.Get(ctx, req.NamespacedName, appliedWork)
		switch {
		case errors.IsNotFound(err):
			klog.ErrorS(err, "appliedWork finalizer resource does not exist yet, it will be created", "AppliedWork", kLogObjRef.Name)
		case err != nil:
			klog.ErrorS(err, "failed to retrieve the appliedWork", "AppliedWork", kLogObjRef.Name)
			return ctrl.Result{}, err
		}
	}

	klog.InfoS("create the appliedWork resource", "AppliedWork", kLogObjRef.Name)
	appliedWork = &workv1alpha1.AppliedWork{
		ObjectMeta: metav1.ObjectMeta{
			Name: req.Name,
		},
		Spec: workv1alpha1.AppliedWorkSpec{
			WorkName:      req.Name,
			WorkNamespace: req.Namespace,
		},
	}
	err = r.spokeClient.Create(ctx, appliedWork)
	if err != nil && !errors.IsAlreadyExists(err) {
		// if this conflicts, we'll simply try again later
		klog.ErrorS(err, "appliedWork create failed", "AppliedWork", kLogObjRef.Name)
		return ctrl.Result{}, err
	}
	work.Finalizers = append(work.Finalizers, workFinalizer)

	return ctrl.Result{}, r.client.Update(ctx, work, &client.UpdateOptions{})
}

// garbageCollectAppliedWork deletes the applied work
func (r *FinalizeWorkReconciler) garbageCollectAppliedWork(ctx context.Context, work *workv1alpha1.Work) (ctrl.Result, error) {
	if controllerutil.ContainsFinalizer(work, workFinalizer) {
		deletePolicy := metav1.DeletePropagationForeground
		appliedWork := workv1alpha1.AppliedWork{}
		err := r.spokeClient.Get(ctx, types.NamespacedName{Name: work.Name}, &appliedWork)
		if err != nil {
			klog.ErrorS(err, "failed to retrieve the appliedWork", "AppliedWork", work.Name)
			return ctrl.Result{}, err
		}
		err = r.spokeClient.Delete(ctx, &appliedWork, &client.DeleteOptions{PropagationPolicy: &deletePolicy})
		if err != nil {
			klog.ErrorS(err, "failed to delete the appliedWork", "AppliedWork", work.Name)
			return ctrl.Result{}, err
		}
		klog.InfoS("successfully deleted the appliedWork", "AppliedWork", work.Name)

		controllerutil.RemoveFinalizer(work, workFinalizer)
	}

	return ctrl.Result{}, r.client.Update(ctx, work, &client.UpdateOptions{})
}

// SetupWithManager wires up the controller.
func (r *FinalizeWorkReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).For(&workv1alpha1.Work{},
		builder.WithPredicates(predicate.GenerationChangedPredicate{})).Complete(r)
}
