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
	"time"

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
	"sigs.k8s.io/work-api/pkg/utils"
)

const (
	messageAppliedWorkFinalizerNotFound = "AppliedWork finalizer object does not exist yet, it will be created"
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
	klog.InfoS("Work finalize controller reconcile loop triggered.", "item", req.NamespacedName)

	if !r.Joined {
		klog.InfoS("finalize controller is not started yet")
		return ctrl.Result{RequeueAfter: time.Second * 5}, fmt.Errorf("finalize controller is not started yet")
	}

	work := &workv1alpha1.Work{}
	err := r.client.Get(ctx, types.NamespacedName{Name: req.Name, Namespace: req.Namespace}, work)
	switch {
	case errors.IsNotFound(err):
		return ctrl.Result{}, nil
	case err != nil:
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
		if err != nil {
			if errors.IsNotFound(err) {
				klog.ErrorS(err, messageAppliedWorkFinalizerNotFound, "AppliedWork", kLogObjRef.Name)
			} else {
				klog.ErrorS(err, utils.MessageResourceRetrieveFailed, "AppliedWork", kLogObjRef.Name)
				return ctrl.Result{}, err
			}
		} else {
			// everything is fine, don't need to do anything
			return ctrl.Result{}, nil
		}
	}

	klog.InfoS(messageAppliedWorkFinalizerNotFound, "AppliedWork", kLogObjRef.Name)
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
		klog.ErrorS(err, utils.MessageResourceCreateFailed, "AppliedWork", kLogObjRef.Name)
		return ctrl.Result{}, err
	}
	r.recorder.Event(appliedWork, corev1.EventTypeNormal, utils.EventReasonAppliedWorkCreated, utils.MessageResourceCreateSucceeded)

	r.recorder.Event(work, corev1.EventTypeNormal, utils.EventReasonAppliedWorkCreated, "AppliedWork resource was created")
	work.Finalizers = append(work.Finalizers, workFinalizer)

	if err = r.client.Update(ctx, work, &client.UpdateOptions{}); err == nil {
		r.recorder.Eventf(
			work,
			corev1.EventTypeNormal,
			utils.EventReasonFinalizerAdded,
			utils.MessageResourceFinalizerAdded+", finalizer=%s",
			workFinalizer)
	}

	return ctrl.Result{}, r.client.Update(ctx, work, &client.UpdateOptions{})
}

// garbageCollectAppliedWork deletes the applied work
func (r *FinalizeWorkReconciler) garbageCollectAppliedWork(ctx context.Context, work *workv1alpha1.Work) (ctrl.Result, error) {
	if controllerutil.ContainsFinalizer(work, workFinalizer) {
		deletePolicy := metav1.DeletePropagationForeground
		appliedWork := workv1alpha1.AppliedWork{}
		err := r.spokeClient.Get(ctx, types.NamespacedName{
			Name: work.Name,
		}, &appliedWork)
		if err != nil {
			klog.ErrorS(err, utils.MessageResourceRetrieveFailed, "AppliedWork", work.Name)
			return ctrl.Result{}, err
		}
		err = r.spokeClient.Delete(ctx, &appliedWork, &client.DeleteOptions{PropagationPolicy: &deletePolicy})
		if err != nil {
			klog.ErrorS(err, utils.MessageResourceDeleteFailed, "AppliedWork", work.Name)
			return ctrl.Result{}, err
		}

		r.recorder.Eventf(work, corev1.EventTypeNormal, utils.EventReasonAppliedWorkDeleted, utils.MessageResourceDeleteSucceeded+", AppliedWork=%s", work.Name)
		klog.InfoS(utils.MessageResourceDeleteSucceeded, "AppliedWork", work.Name)

		controllerutil.RemoveFinalizer(work, workFinalizer)
		r.recorder.Eventf(work, corev1.EventTypeNormal, utils.EventReasonFinalizerRemoved, utils.MessageResourceFinalizerRemoved+", finalizer="+workFinalizer)
	}

	return ctrl.Result{}, r.client.Update(ctx, work, &client.UpdateOptions{})
}

// SetupWithManager wires up the controller.
func (r *FinalizeWorkReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).For(&workv1alpha1.Work{},
		builder.WithPredicates(predicate.GenerationChangedPredicate{})).Complete(r)
}
