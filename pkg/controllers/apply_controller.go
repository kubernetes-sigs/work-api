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
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/dynamic"
	"k8s.io/klog/v2"
	"k8s.io/utils/pointer"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	workv1alpha1 "sigs.k8s.io/work-api/pkg/apis/v1alpha1"
)

// ApplyWorkReconciler reconciles a Work object
type ApplyWorkReconciler struct {
	client             client.Client
	spokeDynamicClient dynamic.Interface
	spokeClient        client.Client
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
	klog.InfoS("work reconcile loop triggered", "item", req.NamespacedName)

	work := &workv1alpha1.Work{}
	err := r.client.Get(ctx, req.NamespacedName, work)
	switch {
	case apierrors.IsNotFound(err):
		return ctrl.Result{}, nil
	case err != nil:
		return ctrl.Result{}, err
	}

	// do nothing if the finalizer is not present
	// it ensures all maintained resources will be cleaned once work is deleted
	if !controllerutil.ContainsFinalizer(work, workFinalizer) {
		klog.InfoS("the work has no finalizer yet, the work finalizer will create it", "item", req.NamespacedName)
		return ctrl.Result{}, nil
	}

	// we created the AppliedWork before setting the finalizer so it should exist
	appliedWork := &workv1alpha1.AppliedWork{}
	if err := r.spokeClient.Get(ctx, types.NamespacedName{Name: req.Name}, appliedWork); err != nil {
		klog.ErrorS(err, "failed to get the appliedWork", "name", req.Name)
		return ctrl.Result{}, errors.Wrap(err, fmt.Sprintf("failed to get the appliedWork %s", req.Name))
	}

	owner := metav1.OwnerReference{
		APIVersion: workv1alpha1.GroupVersion.String(),
		Kind:       appliedWork.Kind,
		Name:       appliedWork.GetName(),
		UID:        appliedWork.GetUID(),
	}

	results := r.applyManifests(work.Spec.Workload.Manifests, work.Status.ManifestConditions, owner)
	errs := []error{}

	// Update manifestCondition based on the results
	var manifestConditions []workv1alpha1.ManifestCondition
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
		klog.ErrorS(err, "update work status failed", "work", req.NamespacedName)
		errs = append(errs, err)
	}

	if len(errs) != 0 {
		klog.InfoS("we didn't apply all the manifest works successfully, queue the next reconcile", "work", req.NamespacedName)
		return ctrl.Result{}, utilerrors.NewAggregate(errs)
	}

	return ctrl.Result{}, nil
}

func (r *ApplyWorkReconciler) applyManifests(manifests []workv1alpha1.Manifest,
	manifestConditions []workv1alpha1.ManifestCondition, owner metav1.OwnerReference) []applyResult {
	var results []applyResult

	for index, manifest := range manifests {
		result := applyResult{
			identifier: workv1alpha1.ResourceIdentifier{Ordinal: index},
		}
		gvr, rawObj, err := r.decodeUnstructured(manifest)
		if err != nil {
			result.err = err
		} else {
			var obj *unstructured.Unstructured
			result.identifier = buildResourceIdentifier(index, rawObj, gvr)
			rawObj.SetOwnerReferences(insertOwnerReference(rawObj.GetOwnerReferences(), owner))
			observedGeneration := findObservedGenerationOfManifest(result.identifier, manifestConditions)
			obj, result.updated, result.err = r.applyUnstructured(gvr, rawObj, observedGeneration)
			if result.err == nil {
				result.generation = obj.GetGeneration()
				klog.V(5).InfoS("applied an unstructrued object", "gvr", gvr, "obj", obj.GetName(), "new observedGeneration", result.generation)
			} else {
				klog.ErrorS(err, "Failed to apply an unstructrued object", "gvr", gvr, "obj", rawObj.GetName())
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
		return schema.GroupVersionResource{}, nil, fmt.Errorf("failed to decode object: %w", err)
	}
	mapping, err := r.restMapper.RESTMapping(unstructuredObj.GroupVersionKind().GroupKind(), unstructuredObj.GroupVersionKind().Version)
	if err != nil {
		return schema.GroupVersionResource{}, nil, fmt.Errorf("failed to find gvr from restmapping: %w", err)
	}

	return mapping.Resource, unstructuredObj, nil
}

func (r *ApplyWorkReconciler) applyUnstructured(
	gvr schema.GroupVersionResource,
	workObj *unstructured.Unstructured,
	observedGeneration int64) (*unstructured.Unstructured, bool, error) {

	err := setSpecHashAnnotation(workObj)
	if err != nil {
		return nil, false, err
	}

	curObj, err := r.spokeDynamicClient.
		Resource(gvr).
		Namespace(workObj.GetNamespace()).
		Get(context.TODO(), workObj.GetName(), metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		actual, err := r.spokeDynamicClient.Resource(gvr).Namespace(workObj.GetNamespace()).Create(
			context.TODO(), workObj, metav1.CreateOptions{})
		return actual, true, err
	}
	if err != nil {
		return nil, false, err
	}

	// Compare and update the unstrcuctured.
	needUpdate := false
	if !isSameUnstructuredMeta(workObj, curObj) {
		klog.V(5).InfoS("work object meta has changed", "gvr", gvr, "obj", workObj.GetName())
		needUpdate = true
		workObj.SetAnnotations(mergeMapOverrideWithDst(curObj.GetAnnotations(), workObj.GetAnnotations()))
		workObj.SetLabels(mergeMapOverrideWithDst(curObj.GetLabels(), workObj.GetLabels()))
		workObj.SetOwnerReferences(mergeOwnerReference(curObj.GetOwnerReferences(), workObj.GetOwnerReferences()))
	}

	if isManifestModified(observedGeneration, curObj, workObj) || needUpdate {
		var actual *unstructured.Unstructured
		newData, err := workObj.MarshalJSON()
		if err != nil {
			klog.ErrorS(err, "work object json marshal failed", "gvr", gvr, "obj", workObj.GetName())
			return nil, false, err
		}
		// try to use severside apply to be safe
		actual, err = r.spokeDynamicClient.Resource(gvr).Namespace(workObj.GetNamespace()).
			Patch(context.TODO(), workObj.GetName(), types.ApplyPatchType, newData,
				metav1.PatchOptions{Force: pointer.Bool(true), FieldManager: "work-api agent"})
		if err != nil {
			klog.ErrorS(err, "work object patched failed", "gvr", gvr, "obj", workObj.GetName())
			workObj.SetResourceVersion(curObj.GetResourceVersion())
			actual, err = r.spokeDynamicClient.Resource(gvr).Namespace(workObj.GetNamespace()).Update(
				context.TODO(), workObj, metav1.UpdateOptions{})
			klog.V(5).InfoS("work object updated", "gvr", gvr, "obj", workObj.GetName(), "err", err)
		} else {
			klog.V(5).InfoS("work object patched", "gvr", gvr, "obj", workObj.GetName())
		}
		return actual, true, err
	}

	return curObj, false, nil
}

// SetupWithManager wires up the controller.
func (r *ApplyWorkReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).For(&workv1alpha1.Work{},
		builder.WithPredicates(predicate.ResourceVersionChangedPredicate{})).Complete(r)
}

// Return true when label/annotation is changed or generation is changed
func isManifestModified(observedGeneration int64, curObj, workObj *unstructured.Unstructured) bool {
	if curObj.GetGeneration() != observedGeneration {
		klog.V(5).InfoS("work object generation has changed", "obj", curObj.GetName(),
			"curobj generation", curObj.GetGeneration(), "observedGeneration", observedGeneration)
		return true
	}

	return false
}

// isSameUnstructuredMeta compares the metadata of two unstructured object.
func isSameUnstructuredMeta(obj1, obj2 *unstructured.Unstructured) bool {
	// we just care about label/annotation and owner references
	if !equality.Semantic.DeepEqual(obj1.GetLabels(), obj2.GetLabels()) {
		return false
	}
	if !equality.Semantic.DeepEqual(obj1.GetAnnotations(), obj2.GetAnnotations()) {
		return false
	}
	if !equality.Semantic.DeepEqual(obj1.GetOwnerReferences(), obj2.GetOwnerReferences()) {
		return false
	}

	return true
}

// MergeMapOverrideWithDst merges two could be nil maps. Keep the dst for any conflicts,
func mergeMapOverrideWithDst(src, dst map[string]string) map[string]string {
	if src == nil && dst == nil {
		return nil
	}
	r := make(map[string]string)
	for k, v := range src {
		r[k] = v
	}
	// override the src for the same key
	for k, v := range dst {
		r[k] = v
	}
	return r
}

// insertOwnerReference inserts a new owner
func insertOwnerReference(owners []metav1.OwnerReference, newOwner metav1.OwnerReference) []metav1.OwnerReference {
	found := false
	for _, owner := range owners {
		if owner.APIVersion == newOwner.APIVersion && owner.Kind == newOwner.Kind &&
			owner.Name == newOwner.Name && owner.UID == newOwner.UID {
			found = true
			break
		}
	}
	if found {
		return owners
	} else {
		return append(owners, newOwner)
	}
}

func mergeOwnerReference(owners, newOwners []metav1.OwnerReference) []metav1.OwnerReference {
	for _, newOwner := range newOwners {
		owners = insertOwnerReference(owners, newOwner)
	}
	return owners
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

// Find the observed generation for an applied condition type for a manifest.
func findObservedGenerationOfManifest(
	identifier workv1alpha1.ResourceIdentifier,
	manifestConditions []workv1alpha1.ManifestCondition) int64 {
	manifestCondition := findManifestConditionByIdentifier(identifier, manifestConditions)
	if manifestCondition == nil {
		return 0
	}

	condition := meta.FindStatusCondition(manifestCondition.Conditions, ConditionTypeApplied)
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
			Type:               ConditionTypeApplied,
			Status:             metav1.ConditionFalse,
			LastTransitionTime: metav1.Now(),
			Reason:             "AppliedManifestFailed",
			Message:            fmt.Sprintf("Failed to apply manifest: %v", err),
		}
	}

	return metav1.Condition{
		Type:               ConditionTypeApplied,
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
		if meta.IsStatusConditionFalse(manifestCond.Conditions, ConditionTypeApplied) {
			return metav1.Condition{
				Type:               ConditionTypeApplied,
				Status:             metav1.ConditionFalse,
				Reason:             "AppliedWorkFailed",
				Message:            "Failed to apply work",
				ObservedGeneration: observedGeneration,
			}
		}
	}

	return metav1.Condition{
		Type:               ConditionTypeApplied,
		Status:             metav1.ConditionTrue,
		Reason:             "AppliedWorkComplete",
		Message:            "Apply work complete",
		ObservedGeneration: observedGeneration,
	}
}
