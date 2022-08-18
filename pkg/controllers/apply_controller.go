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
	"sigs.k8s.io/work-api/pkg/utils"
	"time"

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
	workFieldManagerName = "work-api-agent"
)

// ApplyWorkReconciler reconciles a Work object
type ApplyWorkReconciler struct {
	client             client.Client
	spokeDynamicClient dynamic.Interface
	spokeClient        client.Client
	restMapper         meta.RESTMapper
	recorder           record.EventRecorder
	concurrency        int
	joined             bool
}

func NewApplyWorkReconciler(hubClient client.Client, spokeDynamicClient dynamic.Interface, spokeClient client.Client, restMapper meta.RESTMapper, recorder record.EventRecorder, concurrency int, joined bool) *ApplyWorkReconciler {
	return &ApplyWorkReconciler{
		client:             hubClient,
		spokeDynamicClient: spokeDynamicClient,
		spokeClient:        spokeClient,
		restMapper:         restMapper,
		recorder:           recorder,
		concurrency:        concurrency,
		joined:             joined,
	}
}

// applyResult contains the result of a manifest being applied.
type applyResult struct {
	identifier workv1alpha1.ResourceIdentifier
	generation int64
	updated    bool
	err        error
}

// Reconcile implement the control loop logic for Work object.
func (r *ApplyWorkReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	if !r.joined {
		klog.V(3).InfoS("work controller is not started yet, requeue the request", "work", req.NamespacedName)
		return ctrl.Result{RequeueAfter: time.Second * 5}, nil
	}
	klog.InfoS("work apply controller reconcile loop triggered.", "work", req.NamespacedName)

	// Fetch the work resource
	work := &workv1alpha1.Work{}
	err := r.client.Get(ctx, req.NamespacedName, work)
	switch {
	case apierrors.IsNotFound(err):
		klog.V(4).InfoS("the work resource is deleted", "work", req.NamespacedName)
		return ctrl.Result{}, nil
	case err != nil:
		klog.ErrorS(err, "failed to retrieve the work", "work", req.NamespacedName)
		return ctrl.Result{}, err
	}
	kLogObjRef := klog.KObj(work)

	// Handle deleting work, garbage collect the resources
	if !work.DeletionTimestamp.IsZero() {
		klog.V(2).InfoS("resource is in the process of being deleted", work.Kind, kLogObjRef)
		return r.garbageCollectAppliedWork(ctx, work)
	}

	// Get the appliedWork
	appliedWork, err := r.ensureAppliedWork(ctx, work)
	if err != nil {
		return ctrl.Result{}, err
	}
	owner := metav1.OwnerReference{
		APIVersion:         workv1alpha1.GroupVersion.String(),
		Kind:               workv1alpha1.AppliedWorkKind,
		Name:               appliedWork.GetName(),
		UID:                appliedWork.GetUID(),
		BlockOwnerDeletion: pointer.Bool(false),
	}

	// Apply the manifests to the member cluster
	results := r.applyManifests(ctx, work.Spec.Workload.Manifests, owner)

	// generate the work condition based on the manifest apply result
	errs := r.generateWorkCondition(results, work)

	// update the work status
	if err = r.client.Status().Update(ctx, work, &client.UpdateOptions{}); err != nil {
		errs = append(errs, err)
		klog.ErrorS(err, "failed to update work status", "work", kLogObjRef)
	}

	if len(errs) != 0 {
		klog.InfoS("manifest apply incomplete; the message is queued again for reconciliation",
			"work", kLogObjRef, "err", utilerrors.NewAggregate(errs))
		return ctrl.Result{}, utilerrors.NewAggregate(errs)
	}
	klog.InfoS("apply the work successfully ", "work", kLogObjRef)
	r.recorder.Event(work, v1.EventTypeNormal, "ReconciliationComplete", "apply the work successfully")
	// we periodically reconcile the work to make sure the member cluster state is in sync with the work
	return ctrl.Result{RequeueAfter: time.Minute * 5}, nil
}

// garbageCollectAppliedWork deletes the appliedWork and all the manifests associated with it from the cluster.
func (r *ApplyWorkReconciler) garbageCollectAppliedWork(ctx context.Context, work *workv1alpha1.Work) (ctrl.Result, error) {
	deletePolicy := metav1.DeletePropagationBackground
	if !controllerutil.ContainsFinalizer(work, workFinalizer) {
		return ctrl.Result{}, nil
	}
	// delete the appliedWork which will remove all the manifests associated with it
	// TODO: allow orphaned manifest
	appliedWork := workv1alpha1.AppliedWork{
		ObjectMeta: metav1.ObjectMeta{Name: work.Name},
	}
	err := r.spokeClient.Delete(ctx, &appliedWork, &client.DeleteOptions{PropagationPolicy: &deletePolicy})
	switch {
	case apierrors.IsNotFound(err):
		klog.V(4).InfoS("the appliedWork is already deleted", "appliedWork", work.Name)
	case err != nil:
		klog.ErrorS(err, "failed to delete the appliedWork", "appliedWork", work.Name)
		return ctrl.Result{}, err
	default:
		klog.InfoS("successfully deleted the appliedWork", "appliedWork", work.Name)
	}
	controllerutil.RemoveFinalizer(work, workFinalizer)
	return ctrl.Result{}, r.client.Update(ctx, work, &client.UpdateOptions{})
}

// ensureAppliedWork makes sure that an associated appliedWork and a finalizer on the work resource exsits on the cluster.
func (r *ApplyWorkReconciler) ensureAppliedWork(ctx context.Context, work *workv1alpha1.Work) (*workv1alpha1.AppliedWork, error) {
	workRef := klog.KObj(work)
	appliedWork := &workv1alpha1.AppliedWork{}
	hasFinalizer := false
	if controllerutil.ContainsFinalizer(work, workFinalizer) {
		hasFinalizer = true
		err := r.spokeClient.Get(ctx, types.NamespacedName{Name: work.Name}, appliedWork)
		switch {
		case apierrors.IsNotFound(err):
			klog.ErrorS(err, "appliedWork finalizer resource does not exist even with the finalizer, it will be recreated", "appliedWork", workRef.Name)
		case err != nil:
			klog.ErrorS(err, "failed to retrieve the appliedWork ", "appliedWork", workRef.Name)
			return nil, err
		default:
			return appliedWork, nil
		}
	}

	// we create the appliedWork before setting the finalizer, so it should always exist unless it's deleted behind our back
	appliedWork = &workv1alpha1.AppliedWork{
		ObjectMeta: metav1.ObjectMeta{
			Name: work.Name,
		},
		Spec: workv1alpha1.AppliedWorkSpec{
			WorkName:      work.Name,
			WorkNamespace: work.Namespace,
		},
	}
	if err := r.spokeClient.Create(ctx, appliedWork); err != nil && !apierrors.IsAlreadyExists(err) {
		klog.ErrorS(err, "appliedWork create failed", "appliedWork", workRef.Name)
		return nil, err
	}
	if !hasFinalizer {
		klog.InfoS("add the finalizer to the work", "work", workRef)
		work.Finalizers = append(work.Finalizers, workFinalizer)
		return appliedWork, r.client.Update(ctx, work, &client.UpdateOptions{})
	}
	klog.InfoS("recreated the appliedWork resource", "appliedWork", workRef.Name)
	return appliedWork, nil
}

// applyManifests processes a given set of Manifests by: setting ownership, validating the manifest, and passing it on for application to the cluster.
func (r *ApplyWorkReconciler) applyManifests(ctx context.Context, manifests []workv1alpha1.Manifest, owner metav1.OwnerReference) []applyResult {
	var results []applyResult
	var appliedObj *unstructured.Unstructured

	for index, manifest := range manifests {
		result := applyResult{
			identifier: workv1alpha1.ResourceIdentifier{Ordinal: index},
		}
		gvr, rawObj, err := r.decodeManifest(manifest)
		if err != nil {
			result.err = err
		} else {
			utils.UpsertOwnerRef(owner, rawObj)
			appliedObj, result.updated, result.err = r.applyUnstructured(ctx, gvr, rawObj)
			result.identifier = buildResourceIdentifier(index, rawObj, gvr)
			kLogObjRef := klog.ObjectRef{
				Name:      result.identifier.Name,
				Namespace: result.identifier.Namespace,
			}
			if result.err == nil {
				result.generation = appliedObj.GetGeneration()
				if result.updated {
					klog.V(4).InfoS("manifest upsert succeeded", "gvr", gvr, "manifest", kLogObjRef, "new ObservedGeneration", result.generation)
				} else {
					klog.V(5).InfoS("manifest upsert unwarranted", "gvr", gvr, "manifest", kLogObjRef)
				}
			} else {
				klog.ErrorS(result.err, "manifest upsert failed", "gvr", gvr, "manifest", kLogObjRef)
			}
		}
		results = append(results, result)
	}
	return results
}

// Decodes the manifest into usable structs.
func (r *ApplyWorkReconciler) decodeManifest(manifest workv1alpha1.Manifest) (schema.GroupVersionResource, *unstructured.Unstructured, error) {
	unstructuredObj := &unstructured.Unstructured{}
	err := unstructuredObj.UnmarshalJSON(manifest.Raw)
	if err != nil {
		return schema.GroupVersionResource{}, nil, fmt.Errorf("failed to decode object: %w", err)
	}

	mapping, err := r.restMapper.RESTMapping(unstructuredObj.GroupVersionKind().GroupKind(), unstructuredObj.GroupVersionKind().Version)
	if err != nil {
		return schema.GroupVersionResource{}, nil, fmt.Errorf("failed to find group/version/resource from restmapping: %w", err)
	}

	return mapping.Resource, unstructuredObj, nil
}

// Determines if an unstructured manifest object can & should be applied. If so, it applies (creates) the resource on the cluster.
func (r *ApplyWorkReconciler) applyUnstructured(ctx context.Context, gvr schema.GroupVersionResource, manifestObj *unstructured.Unstructured) (*unstructured.Unstructured, bool, error) {
	manifestRef := klog.ObjectRef{
		Name:      manifestObj.GetName(),
		Namespace: manifestObj.GetName(),
	}
	curObj, err := r.spokeDynamicClient.Resource(gvr).Namespace(manifestObj.GetNamespace()).Get(ctx, manifestObj.GetName(), metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		actual, createErr := r.spokeDynamicClient.Resource(gvr).Namespace(manifestObj.GetNamespace()).Create(
			ctx, manifestObj, metav1.CreateOptions{FieldManager: workFieldManagerName})
		if createErr == nil || apierrors.IsAlreadyExists(createErr) {
			klog.V(4).InfoS("successfully created the manfiest", "gvr", gvr, "manfiest", manifestRef)
			return actual, true, nil
		}
		return nil, false, createErr
	}
	if err != nil {
		return nil, false, err
	}

	// check if the existing manifest is managed by the work
	if !isManifestManagedByWork(curObj.GetOwnerReferences()) {
		err = fmt.Errorf("resource is not managed by the work controller")
		klog.ErrorS(err, "skip applying a not managed manifest", "gvr", gvr, "obj", manifestRef)
		return nil, false, err
	}

	err = setManifestHashAnnotation(manifestObj)
	if err != nil {
		return nil, false, err
	}
	// We only try to update the object if its spec hash value has changed.
	if manifestObj.GetAnnotations()[manifestHashAnnotation] != curObj.GetAnnotations()[manifestHashAnnotation] {
		return r.patchCurrentResource(ctx, gvr, manifestObj, curObj)
	}

	return curObj, false, nil
}

// patchCurrentResource use server side apply to patch the current resource with the new manifest we get from the work.
func (r *ApplyWorkReconciler) patchCurrentResource(ctx context.Context, gvr schema.GroupVersionResource, manifestObj, curObj *unstructured.Unstructured) (*unstructured.Unstructured, bool, error) {
	manifestRef := klog.ObjectRef{
		Name:      manifestObj.GetName(),
		Namespace: manifestObj.GetName(),
	}
	klog.V(5).InfoS("manifest is modified", "gvr", gvr, "manifest", manifestRef,
		"new hash", manifestObj.GetAnnotations()[manifestHashAnnotation],
		"existing hash", curObj.GetAnnotations()[manifestHashAnnotation])
	newData, err := manifestObj.MarshalJSON()
	if err != nil {
		klog.ErrorS(err, "failed to JSON marshall manifest", "gvr", gvr, "manifest", manifestRef)
		return nil, false, err
	}
	// Use sever-side apply to be safe.
	actual, patchErr := r.spokeDynamicClient.Resource(gvr).Namespace(manifestObj.GetNamespace()).
		Patch(ctx, manifestObj.GetName(), types.ApplyPatchType, newData,
			metav1.PatchOptions{Force: pointer.Bool(true), FieldManager: workFieldManagerName})
	if patchErr != nil {
		klog.ErrorS(patchErr, "failed to patch the manifest", "gvr", gvr, "manifest", manifestRef)
		return nil, false, patchErr
	}
	klog.V(3).InfoS("manifest patch succeeded", "gvr", gvr, "manifest", manifestRef)
	return actual, true, nil
}

// generateWorkCondition constructs the work condition based on the apply result
func (r *ApplyWorkReconciler) generateWorkCondition(results []applyResult, work *workv1alpha1.Work) []error {
	var errs []error
	// Update manifestCondition based on the results.
	var manifestConditions []workv1alpha1.ManifestCondition
	for _, result := range results {
		if result.err != nil {
			errs = append(errs, result.err)
		}
		appliedCondition := buildManifestAppliedCondition(result.err, result.updated, result.generation)
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
	workCond := generateWorkAppliedCondition(manifestConditions, work.Generation)
	work.Status.Conditions = []metav1.Condition{workCond}
	return errs
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

// Generates a hash of the spec annotation from an unstructured object after we remove all the fields
// we have modified.
func computeManifestHash(obj *unstructured.Unstructured) (string, error) {
	manifest := obj.DeepCopy()
	// strip all the appliedWork owner fields just in case
	// those fields should not exist in the manifest
	curRefs := manifest.GetOwnerReferences()
	var newRefs []metav1.OwnerReference
	for _, ownerRef := range curRefs {
		if ownerRef.APIVersion == workv1alpha1.GroupVersion.String() && ownerRef.Kind == workv1alpha1.AppliedWorkKind {
			// we skip the appliedWork owner
			continue
		}
		newRefs = append(newRefs, ownerRef)
	}
	manifest.SetOwnerReferences(newRefs)
	// remove the manifestHash Annotation just in case
	annotation := manifest.GetAnnotations()
	if annotation != nil {
		delete(annotation, manifestHashAnnotation)
		manifest.SetAnnotations(annotation)
	}
	// strip the status just in case
	manifest.SetResourceVersion("")
	manifest.SetGeneration(0)
	manifest.SetUID("")
	manifest.SetSelfLink("")
	manifest.SetDeletionTimestamp(nil)
	manifest.SetManagedFields(nil)
	unstructured.RemoveNestedField(manifest.Object, "metadata", "creationTimestamp")
	unstructured.RemoveNestedField(manifest.Object, "status")
	data := manifest.Object
	// compute the sha256 hash of the remaining data
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(jsonBytes)), nil
}

// isManifestManagedByWork determines if an object is managed by the work controller.
func isManifestManagedByWork(ownerRefs []metav1.OwnerReference) bool {
	// an object is managed by the work if any of its owner reference is of type appliedWork
	for _, ownerRef := range ownerRefs {
		if ownerRef.APIVersion == workv1alpha1.GroupVersion.String() && ownerRef.Kind == workv1alpha1.AppliedWorkKind {
			return true
		}
	}
	return false
}

// findManifestConditionByIdentifier return a ManifestCondition by identifier
// 1. Find the manifest condition with the whole identifier.
// 2. If identifier only has ordinal, and a matched cannot be found, return nil.
// 3. Try to find properties, other than the ordinal, within the identifier.
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

// setManifestHashAnnotation computes the hash of the provided manifest and sets an annotation of the
// hash on the provided unstructured object.
func setManifestHashAnnotation(manifestObj *unstructured.Unstructured) error {
	manifestHash, err := computeManifestHash(manifestObj)
	if err != nil {
		return err
	}

	annotation := manifestObj.GetAnnotations()
	if annotation == nil {
		annotation = map[string]string{}
	}
	annotation[manifestHashAnnotation] = manifestHash
	manifestObj.SetAnnotations(annotation)
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

func buildManifestAppliedCondition(err error, updated bool, observedGeneration int64) metav1.Condition {
	if err != nil {
		return metav1.Condition{
			Type:               ConditionTypeApplied,
			Status:             metav1.ConditionFalse,
			ObservedGeneration: observedGeneration,
			LastTransitionTime: metav1.Now(),
			Reason:             "appliedManifestFailed",
			Message:            fmt.Sprintf("Failed to apply manifest: %v", err),
		}
	}

	if updated {
		return metav1.Condition{
			Type:               ConditionTypeApplied,
			Status:             metav1.ConditionTrue,
			LastTransitionTime: metav1.Now(),
			ObservedGeneration: observedGeneration,
			Reason:             "appliedManifestUpdated",
			Message:            "appliedManifest updated",
		}
	}
	return metav1.Condition{
		Type:               ConditionTypeApplied,
		Status:             metav1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
		ObservedGeneration: observedGeneration,
		Reason:             "appliedManifestComplete",
		Message:            "Apply manifest complete",
	}
}

// generateWorkAppliedCondition generate appied status condition for work.
// If one of the manifests is applied failed on the spoke, the applied status condition of the work is false.
func generateWorkAppliedCondition(manifestConditions []workv1alpha1.ManifestCondition, observedGeneration int64) metav1.Condition {
	for _, manifestCond := range manifestConditions {
		if meta.IsStatusConditionFalse(manifestCond.Conditions, ConditionTypeApplied) {
			return metav1.Condition{
				Type:               ConditionTypeApplied,
				Status:             metav1.ConditionFalse,
				LastTransitionTime: metav1.Now(),
				Reason:             "appliedWorkFailed",
				Message:            "Failed to apply work",
				ObservedGeneration: observedGeneration,
			}
		}
	}

	return metav1.Condition{
		Type:               ConditionTypeApplied,
		Status:             metav1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
		Reason:             "appliedWorkComplete",
		Message:            "Apply work complete",
		ObservedGeneration: observedGeneration,
	}
}
