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

	corev1 "k8s.io/api/core/v1"
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
	"sigs.k8s.io/work-api/pkg/client/clientset/versioned"
)

const (
	eventReasonAppliedWorkCreated = "AppliedWorkCreated"
	eventReasonAppliedWorkDeleted = "AppliedWorkDeleted"
	eventReasonFinalizerAdded     = "FinalizerAdded"
	eventReasonFinalizerRemoved   = "FinalizerRemoved"
)

// FinalizeWorkReconciler reconciles a Work object for finalization
type FinalizeWorkReconciler struct {
	client      client.Client
	spokeClient versioned.Interface
	recorder    record.EventRecorder
}

// Reconcile implement the control loop logic for finalizing Work object.
func (r *FinalizeWorkReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	work := &workv1alpha1.Work{}
	err := r.client.Get(ctx, types.NamespacedName{Name: req.Name, Namespace: req.Namespace}, work)
	switch {
	case errors.IsNotFound(err):
		return ctrl.Result{}, nil
	case err != nil:
		return ctrl.Result{}, err
	}

	klog.InfoS("Finalize work reconcile loop triggered", "item", req.NamespacedName)

	// cleanup finalizer and resources
	if !work.DeletionTimestamp.IsZero() {
		return r.garbageCollectAppliedWork(ctx, work)
	}

	var appliedWork *workv1alpha1.AppliedWork
	if controllerutil.ContainsFinalizer(work, workFinalizer) {
		_, err = r.spokeClient.MulticlusterV1alpha1().AppliedWorks().Get(ctx, req.Name, metav1.GetOptions{})
		if err != nil {
			if errors.IsNotFound(err) {
				klog.ErrorS(err, "the finalizer appliedWork object doesn't exist, we will add it back", "name", req.Name)
			} else {
				klog.ErrorS(err, "failed to get the  finalizer appliedWork", "name", req.Name)
				return ctrl.Result{}, err
			}
		} else {
			// everything is fine, don't need to do anything
			return ctrl.Result{}, nil
		}
	}

	klog.InfoS("appliedWork finalizer does not exist yet, we will create it", "item", req.NamespacedName)
	appliedWork = &workv1alpha1.AppliedWork{
		ObjectMeta: metav1.ObjectMeta{
			Name: req.Name,
		},
		Spec: workv1alpha1.AppliedWorkSpec{
			WorkName:      req.Name,
			WorkNamespace: req.Namespace,
		},
	}
	_, err = r.spokeClient.MulticlusterV1alpha1().AppliedWorks().Create(ctx, appliedWork, metav1.CreateOptions{})
	if err != nil && !errors.IsAlreadyExists(err) {
		// if this conflicts, we'll simply try again later
		klog.ErrorS(err, "failed to create the appliedWork", "name", req.Name)
		return ctrl.Result{}, err
	}

	r.recorder.Event(work, corev1.EventTypeNormal, eventReasonAppliedWorkCreated, "AppliedWork resource was created")
	work.Finalizers = append(work.Finalizers, workFinalizer)

	if err = r.client.Update(ctx, work, &client.UpdateOptions{}); err == nil {
		r.recorder.Event(work, corev1.EventTypeNormal, eventReasonFinalizerAdded, "Work resource's finalizer added: "+workFinalizer)
	}

	return ctrl.Result{}, r.client.Update(ctx, work, &client.UpdateOptions{})
}

// garbageCollectAppliedWork deletes the applied work
func (r *FinalizeWorkReconciler) garbageCollectAppliedWork(ctx context.Context, work *workv1alpha1.Work) (ctrl.Result, error) {
	if controllerutil.ContainsFinalizer(work, workFinalizer) {
		deletePolicy := metav1.DeletePropagationForeground
		err := r.spokeClient.MulticlusterV1alpha1().AppliedWorks().Delete(ctx, work.Name,
			metav1.DeleteOptions{PropagationPolicy: &deletePolicy})
		if err != nil {
			klog.ErrorS(err, "failed to delete the applied Work", work.Name)
			return ctrl.Result{}, err
		}

		r.recorder.Event(work, corev1.EventTypeNormal, eventReasonAppliedWorkDeleted, "AppliedWork was deleted")
		klog.Infof("Removed the applied Work %s", work.Name)

		controllerutil.RemoveFinalizer(work, workFinalizer)
		r.recorder.Event(work, corev1.EventTypeNormal, eventReasonFinalizerRemoved, "Work resource's finalizer removed: "+workFinalizer)
	}

	return ctrl.Result{}, r.client.Update(ctx, work, &client.UpdateOptions{})
}

// SetupWithManager wires up the controller.
func (r *FinalizeWorkReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).For(&workv1alpha1.Work{},
		builder.WithPredicates(predicate.GenerationChangedPredicate{})).Complete(r)
}
