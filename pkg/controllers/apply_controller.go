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
	"crypto/sha256"
	"encoding/json"
	"fmt"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/dynamic"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	workv1alpha1 "sigs.k8s.io/work-api/pkg/apis/v1alpha1"
)

// ApplyWorkReconciler reconciles a Work object
type ApplyWorkReconciler struct {
	client             client.Client
	spokeDynamicClient dynamic.Interface
	log                logr.Logger
	restMapper         meta.RESTMapper
}

type applyResult struct {
	identifier workv1alpha1.ResourceIdentifier
	generation int64
	updated    bool
	err        error
}

// Reconcile implement the control loop logic for Work object.
func (r *ApplyWorkReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	work := &workv1alpha1.Work{}
	err := r.client.Get(ctx, types.NamespacedName{Name: req.Name, Namespace: req.Namespace}, work)
	switch {
	case errors.IsNotFound(err):
		return ctrl.Result{}, nil
	case err != nil:
		return ctrl.Result{}, err
	}

	// do nothing if the finalizer is not present
	// it ensures all maintained resources will be cleaned once work is deleted
	found := false
	for i := range work.Finalizers {
		if work.Finalizers[i] == workFinalizer {
			found = true
			break
		}
	}
	if !found {
		return ctrl.Result{}, nil
	}

	results := r.applyManifests(work.Spec.Workload.Manifests, work.Status.ManifestConditions)
	errs := []error{}

	// Update manifestCondition based on the results
	manifestConditions := []workv1alpha1.ManifestCondition{}
	for _, result := range results {
		if result.err != nil {
			errs = append(errs, result.err)
		}
		appliedCondition := buildAppliedStatusCondition(result.err, result.generation)
		manifestCondition := workv1alpha1.ManifestCondition{
			Identifier: result.identifier,
			Conditions: []metav1.Condition{appliedCondition},
		}
		foundmanifestCondition := findManifestConditionByIdentifier(result.identifier, work.Status.ManifestConditions)
		if foundmanifestCondition != nil {
			manifestCondition.Conditions = foundmanifestCondition.Conditions
			meta.SetStatusCondition(&manifestCondition.Conditions, appliedCondition)
		}
		manifestConditions = append(manifestConditions, manifestCondition)
	}

	work.Status.ManifestConditions = manifestConditions

	// Update status condition of work
	workCond := generateWorkAppliedStatusCondition(manifestConditions, work.Generation)
	meta.SetStatusCondition(&work.Status.Conditions, workCond)

	err = r.client.Status().Update(ctx, work, &client.UpdateOptions{})
	if err != nil {
		errs = append(errs, err)
	}

	if len(errs) != 0 {
		return ctrl.Result{}, utilerrors.NewAggregate(errs)
	}

	return ctrl.Result{}, nil
}

func (r *ApplyWorkReconciler) applyManifests(manifests []workv1alpha1.Manifest, manifestConditions []workv1alpha1.ManifestCondition) []applyResult {
	results := []applyResult{}

	for index, manifest := range manifests {
		result := applyResult{
			identifier: workv1alpha1.ResourceIdentifier{Ordinal: index},
		}
		gvr, required, err := r.decodeUnstructured(manifest)
		if err != nil {
			result.err = err
		} else {
			var obj *unstructured.Unstructured
			result.identifier = buildResourceIdentifier(index, required, gvr)
			observedGeneration := findObservedGenerationOfManifest(result.identifier, manifestConditions)
			obj, result.updated, result.err = r.applyUnstructrued(gvr, required, observedGeneration)
			if obj != nil {
				result.generation = obj.GetGeneration()
			}
		}
		results = append(results, result)
	}
	return results
}

func (r *ApplyWorkReconciler) decodeUnstructured(manifest workv1alpha1.Manifest) (schema.GroupVersionResource, *unstructured.Unstructured, error) {
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

func (r *ApplyWorkReconciler) applyUnstructrued(
	gvr schema.GroupVersionResource,
	required *unstructured.Unstructured,
	observedGeneration int64) (*unstructured.Unstructured, bool, error) {

	err := setSpecHashAnnotation(required)
	if err != nil {
		return nil, false, err
	}

	existing, err := r.spokeDynamicClient.
		Resource(gvr).
		Namespace(required.GetNamespace()).
		Get(context.TODO(), required.GetName(), metav1.GetOptions{})
	if errors.IsNotFound(err) {
		actual, err := r.spokeDynamicClient.Resource(gvr).Namespace(required.GetNamespace()).Create(
			context.TODO(), required, metav1.CreateOptions{})
		return actual, true, err
	}
	if err != nil {
		return nil, false, err
	}

	// Compare and update the unstrcuctured.
	if isManifestModified(observedGeneration, gvr, existing, required) {
		required.SetResourceVersion(existing.GetResourceVersion())
		actual, err := r.spokeDynamicClient.Resource(gvr).Namespace(required.GetNamespace()).Update(
			context.TODO(), required, metav1.UpdateOptions{})
		return actual, true, err
	}

	return existing, false, nil
}

// SetupWithManager wires up the controller.
func (r *ApplyWorkReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).For(&workv1alpha1.Work{}).Complete(r)
}

// Return true when label/annotation is changed or generation is changed
func isManifestModified(observedGeneration int64, gvr schema.GroupVersionResource, existing, required *unstructured.Unstructured) bool {
	if !isSameUnstructuredMeta(required, existing) {
		return true
	}

	if existing.GetGeneration() != observedGeneration {
		return true
	}

	return false
}

// isSameUnstructuredMeta compares the metadata of two unstructured object.
func isSameUnstructuredMeta(obj1, obj2 *unstructured.Unstructured) bool {
	// Comapre gvk, name, namespace at first
	if obj1.GroupVersionKind() != obj2.GroupVersionKind() {
		return false
	}
	if obj1.GetName() != obj2.GetName() {
		return false
	}
	if obj1.GetNamespace() != obj2.GetNamespace() {
		return false
	}

	// Compare label and annotations
	if !equality.Semantic.DeepEqual(obj1.GetLabels(), obj2.GetLabels()) {
		return false
	}
	if !equality.Semantic.DeepEqual(obj1.GetAnnotations(), obj2.GetAnnotations()) {
		return false
	}

	return true
}

// findManifestConditionByIdentifier return a ManifestCondition by identifier
// 1. find the manifest condition with the whole identifier;
// 2. if identifier only has ordinal and a matched cannot found, return nil
// 3. try to find with properties other than ordinal in identifier
func findManifestConditionByIdentifier(identifier workv1alpha1.ResourceIdentifier, manifestConditions []workv1alpha1.ManifestCondition) *workv1alpha1.ManifestCondition {
	for _, manifestCondition := range manifestConditions {
		if identifier == manifestCondition.Identifier {
			return &manifestCondition
		}
	}

	if identifier == (workv1alpha1.ResourceIdentifier{Ordinal: identifier.Ordinal}) {
		return nil
	}

	identifierCopy := identifier.DeepCopy()
	for _, manifestCondition := range manifestConditions {
		identifierCopy.Ordinal = manifestCondition.Identifier.Ordinal
		if *identifierCopy == manifestCondition.Identifier {
			return &manifestCondition
		}
	}
	return nil
}

// Find observeredGeneration for applied condition type for a manifest.
func findObservedGenerationOfManifest(
	identifier workv1alpha1.ResourceIdentifier,
	manifestConditions []workv1alpha1.ManifestCondition) int64 {
	manifestCondition := findManifestConditionByIdentifier(identifier, manifestConditions)
	if manifestCondition == nil {
		return 0
	}

	condition := meta.FindStatusCondition(manifestCondition.Conditions, "Applied")
	if condition == nil {
		return 0
	}

	return condition.ObservedGeneration
}

// setSpecHashAnnotation computes the hash of the provided spec and sets an annotation of the
// hash on the provided unstructured objectt. This method is used internally by Apply<type> methods.
func setSpecHashAnnotation(obj *unstructured.Unstructured) error {
	data := obj.DeepCopy().Object
	// do not hash metadata and status section
	delete(data, "metadata")
	delete(data, "status")

	jsonBytes, err := json.Marshal(data)
	if err != nil {
		return err
	}

	specHash := fmt.Sprintf("%x", sha256.Sum256(jsonBytes))
	annotation := obj.GetAnnotations()
	if annotation == nil {
		annotation = map[string]string{}
	}
	annotation[specHashAnnotation] = specHash
	obj.SetAnnotations(annotation)
	return nil
}

func buildResourceIdentifier(index int, object *unstructured.Unstructured, gvr schema.GroupVersionResource) workv1alpha1.ResourceIdentifier {
	identifier := workv1alpha1.ResourceIdentifier{
		Ordinal: index,
	}

	identifier.Group = object.GroupVersionKind().Group
	identifier.Version = object.GroupVersionKind().Version
	identifier.Kind = object.GroupVersionKind().Kind
	identifier.Namespace = object.GetNamespace()
	identifier.Name = object.GetName()
	identifier.Resource = gvr.Resource

	return identifier
}

func buildAppliedStatusCondition(err error, observedGeneration int64) metav1.Condition {
	if err != nil {
		return metav1.Condition{
			Type:               "Applied",
			Status:             metav1.ConditionFalse,
			LastTransitionTime: metav1.Now(),
			Reason:             "AppliedManifestFailed",
			Message:            fmt.Sprintf("Failed to apply manifest: %v", err),
		}
	}

	return metav1.Condition{
		Type:               "Applied",
		Status:             metav1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
		ObservedGeneration: observedGeneration,
		Reason:             "AppliedManifestComplete",
		Message:            "Apply manifest complete",
	}
}

// generateWorkAppliedStatusCondition generate appied status condition for work.
// If one of the manifests is applied failed on the spoke, the applied status condition of the work is false.
func generateWorkAppliedStatusCondition(manifestConditions []workv1alpha1.ManifestCondition, observedGeneration int64) metav1.Condition {
	for _, manifestCond := range manifestConditions {
		if meta.IsStatusConditionFalse(manifestCond.Conditions, "Applied") {
			return metav1.Condition{
				Type:               "Applied",
				Status:             metav1.ConditionFalse,
				Reason:             "AppliedWorkFailed",
				Message:            "Failed to apply work",
				ObservedGeneration: observedGeneration,
			}
		}
	}

	return metav1.Condition{
		Type:               "Applied",
		Status:             metav1.ConditionTrue,
		Reason:             "AppliedWorkComplet",
		Message:            "Apply work complete",
		ObservedGeneration: observedGeneration,
	}
}
