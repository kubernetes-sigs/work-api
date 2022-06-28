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

	"github.com/pkg/errors"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	"k8s.io/utils/pointer"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	workv1alpha1 "sigs.k8s.io/work-api/pkg/apis/v1alpha1"
)

const (
	workFieldManagerName                 = "work-api-agent"
	eventReasonNewManifestApplied        = "NewManifestApplied"
	eventReasonReconciliationAggregated  = "WorkReconciliationAggregated"
	eventReasonResourceNotFound          = "ResourceNotFound"
	eventReasonResourceNotOwnedByWorkAPI = "ResourceNotOwnedByWorkAPI"
	eventReasonUpdateWorkStatusFailed    = "UpdateWorkStatusFailed"
	eventReasonUpdateWorkStatusSuccess   = "UpdateWorkStatusSuccessful"
	eventReasonWorkResourcePatched       = "WorkResourcePatched"
	eventReasonWorkResourcePatchFailed   = "WorkResourcePatchFailed"
)

// ApplyWorkReconciler reconciles a Work object
type ApplyWorkReconciler struct {
	client             client.Client
	spokeDynamicClient dynamic.Interface
	spokeClient        client.Client
	restMapper         meta.RESTMapper
	recorder           record.EventRecorder
	concurrency        int
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

	// leave the finalizer to clean up
	if !work.DeletionTimestamp.IsZero() {
		klog.InfoS("the work is being deleted, the work finalizer will garbage collect the resource", "item", req.NamespacedName)
		return ctrl.Result{}, nil
	}

	// we created the AppliedWork before setting the finalizer, it should already exist
	appliedWork := &workv1alpha1.AppliedWork{}
	if err := r.spokeClient.Get(ctx, types.NamespacedName{Name: req.Name}, appliedWork); err != nil {
		klog.ErrorS(err, "failed to get the appliedWork", "name", req.Name)
		r.recorder.Eventf(work, v1.EventTypeWarning, eventReasonResourceNotFound, "AppliedWork not found for Work %s", work.GetName())
		return ctrl.Result{}, errors.Wrap(err, fmt.Sprintf("failed to get the appliedWork %s", req.Name))
	}

	owner := metav1.OwnerReference{
		APIVersion: workv1alpha1.GroupVersion.String(),
		Kind:       workv1alpha1.AppliedWorkKind,
		Name:       appliedWork.GetName(),
		UID:        appliedWork.GetUID(),
	}

	results := r.applyManifests(ctx, work.Spec.Workload.Manifests, owner)
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
		r.recorder.Eventf(work, v1.EventTypeWarning, eventReasonUpdateWorkStatusFailed, "Updating Work %s failed", work.GetName())
		errs = append(errs, err)
	} else {
		r.recorder.Eventf(work, v1.EventTypeNormal, eventReasonUpdateWorkStatusSuccess, "Updating Work %s successful", work.GetName())
	}

	if len(errs) != 0 {
		klog.InfoS("we didn't apply all the manifest works successfully, queue the next reconcile", "work", req.NamespacedName)
		r.recorder.Eventf(work, v1.EventTypeWarning, eventReasonReconciliationAggregated, "Not all Manifest for Work %s were applied, queueing next reconciliation", work.GetName())
		return ctrl.Result{}, utilerrors.NewAggregate(errs)
	}

	return ctrl.Result{}, nil
}

func (r *ApplyWorkReconciler) applyManifests(ctx context.Context, manifests []workv1alpha1.Manifest, owner metav1.OwnerReference) []applyResult {
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
			// TODO: add webhooks to block any manifest that has owner reference
			rawObj.SetOwnerReferences([]metav1.OwnerReference{owner})
			obj, result.updated, result.err = r.applyUnstructured(ctx, gvr, rawObj)
			if result.err == nil {
				result.generation = obj.GetGeneration()
				if result.updated {
					klog.V(5).InfoS("applied an unstructrued object", "gvr", gvr, "obj", obj.GetName(), "new observedGeneration", result.generation)
				} else {
					klog.V(8).InfoS("object spec has not changed", "gvr", gvr, "obj", obj.GetName())
				}
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
	ctx context.Context,
	gvr schema.GroupVersionResource,
	workObj *unstructured.Unstructured) (*unstructured.Unstructured, bool, error) {

	err := setSpecHashAnnotation(workObj)
	if err != nil {
		return nil, false, err
	}

	curObj, err := r.spokeDynamicClient.
		Resource(gvr).
		Namespace(workObj.GetNamespace()).
		Get(ctx, workObj.GetName(), metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		actual, err := r.spokeDynamicClient.Resource(gvr).Namespace(workObj.GetNamespace()).Create(
			ctx, workObj, metav1.CreateOptions{FieldManager: workFieldManagerName})
		r.recorder.Eventf(workObj, v1.EventTypeNormal, eventReasonNewManifestApplied, "Manifest Applied for Work %s", workObj.GetName())

		return actual, true, err
	}
	if err != nil {
		return nil, false, err
	}

	if !hasSharedOwnerReference(curObj.GetOwnerReferences(), workObj.GetOwnerReferences()[0]) {
		err = fmt.Errorf("object %s:%s is not owned by the work-api anymore", gvr, curObj.GetName())
		klog.ErrorS(err, "Found an object that is not owned by the work-api")
		r.recorder.Eventf(workObj, v1.EventTypeWarning, eventReasonResourceNotOwnedByWorkAPI, "Resource for %s not owned by the work api", workObj.GetName())
		return nil, false, err
	}

	// We only try to update the object if its spec hash value has changed
	if workObj.GetAnnotations()[specHashAnnotation] != curObj.GetAnnotations()[specHashAnnotation] {
		klog.V(5).InfoS("work object's specification has changed", "gvr", gvr, "obj", workObj.GetName(),
			"new hash", workObj.GetAnnotations()[specHashAnnotation], "existingHash", curObj.GetAnnotations()[specHashAnnotation])
		workObj.SetAnnotations(mergeMapOverrideWithDst(curObj.GetAnnotations(), workObj.GetAnnotations()))
		workObj.SetLabels(mergeMapOverrideWithDst(curObj.GetLabels(), workObj.GetLabels()))
		workObj.SetOwnerReferences(mergeOwnerReference(curObj.GetOwnerReferences(), workObj.GetOwnerReferences()))

		newData, err := workObj.MarshalJSON()
		if err != nil {
			klog.ErrorS(err, "work object json marshal failed", "gvr", gvr, "obj", workObj.GetName())
			return nil, false, err
		}
		// Use severside apply to be safe
		actual, err := r.spokeDynamicClient.Resource(gvr).Namespace(workObj.GetNamespace()).
			Patch(ctx, workObj.GetName(), types.ApplyPatchType, newData,
				metav1.PatchOptions{Force: pointer.Bool(true), FieldManager: workFieldManagerName})
		if err != nil {
			klog.ErrorS(err, "work object patched failed", "gvr", gvr, "obj", workObj.GetName())
			r.recorder.Eventf(workObj, v1.EventTypeWarning, eventReasonWorkResourcePatchFailed, "work resource patch failed for %s", workObj.GetName())

			return nil, false, err
		}
		klog.V(5).InfoS("work object patched", "gvr", gvr, "obj", workObj.GetName())
		r.recorder.Eventf(workObj, v1.EventTypeWarning, eventReasonWorkResourcePatched, "work resource patch successful for %s", workObj.GetName())

		return actual, true, err
	}

	return curObj, false, nil
}

// SetupWithManager wires up the controller.
func (r *ApplyWorkReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: r.concurrency,
		}).
		For(&workv1alpha1.Work{}, builder.WithPredicates(predicate.ResourceVersionChangedPredicate{})).
		Complete(r)
}

// Generates a hash of the spec annotation from an unstructured object.
func generateSpecHash(obj *unstructured.Unstructured) (string, error) {
	data := obj.DeepCopy().Object
	delete(data, "metadata")
	delete(data, "status")

	jsonBytes, err := json.Marshal(data)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("%x", sha256.Sum256(jsonBytes)), nil
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

// Determines if two arrays contain the same metav1.OwnerReference.
func hasSharedOwnerReference(owners []metav1.OwnerReference, target metav1.OwnerReference) bool {
	// TODO: Move to a util directory or find an existing library.
	for _, owner := range owners {
		if owner.APIVersion == target.APIVersion && owner.Kind == target.Kind && owner.Name == target.Name && owner.UID == target.UID {
			return true
		}
	}
	return false
}

// Inserts the owner reference into the array of existing owner references.
func insertOwnerReference(owners []metav1.OwnerReference, newOwner metav1.OwnerReference) []metav1.OwnerReference {
	if hasSharedOwnerReference(owners, newOwner) {
		return owners
	} else {
		return append(owners, newOwner)
	}
}

// Merges two owner reference arrays.
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

// setSpecHashAnnotation computes the hash of the provided spec and sets an annotation of the
// hash on the provided unstructured objectt. This method is used internally by Apply<type> methods.
func setSpecHashAnnotation(obj *unstructured.Unstructured) error {
	specHash, err := generateSpecHash(obj)
	if err != nil {
		return err
	}

	annotation := obj.GetAnnotations()
	if annotation == nil {
		annotation = map[string]string{}
	}
	annotation[specHashAnnotation] = specHash
	obj.SetAnnotations(annotation)
	return nil
}

// Builds a resource identifier for a given unstructured.Unstructured object.
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
