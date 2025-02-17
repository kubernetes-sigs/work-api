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

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	workv1alpha1 "sigs.k8s.io/work-api/pkg/apis/v1alpha1"
)

// FinalizeWorkReconciler reconciles a Work object for finalization
type FinalizeWorkReconciler struct {
	client             client.Client
	spokeDynamicClient dynamic.Interface
	restMapper         meta.RESTMapper
	log                logr.Logger
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

	// cleanup finalizer and resources
	if !work.DeletionTimestamp.IsZero() {
		// TODO add clean resource logic
		if controllerutil.ContainsFinalizer(work, workFinalizer) {
			controllerutil.RemoveFinalizer(work, workFinalizer)
		}
		return ctrl.Result{}, r.client.Update(ctx, work, &client.UpdateOptions{})
	}

	// don't add finalizer to instances that already have it
	if controllerutil.ContainsFinalizer(work, workFinalizer) {
		return ctrl.Result{}, nil
	}

	// if this conflicts, we'll simply try again later
	work.Finalizers = append(work.Finalizers, workFinalizer)
	return ctrl.Result{}, r.client.Update(ctx, work, &client.UpdateOptions{})
}

// SetupWithManager wires up the controller.
func (r *FinalizeWorkReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("finalize-controller").
		For(&workv1alpha1.Work{}).Complete(r)
}
