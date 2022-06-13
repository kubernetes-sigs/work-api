package controllers

import (
	json2 "encoding/json"
	"github.com/pkg/errors"
	testing2 "k8s.io/client-go/testing"
	"reflect"
	"testing"

	"github.com/crossplane/crossplane-runtime/pkg/test"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/json"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/dynamic/fake"

	workv1alpha1 "sigs.k8s.io/work-api/pkg/apis/v1alpha1"
)

var (
	fakeDynamicClient = fake.NewSimpleDynamicClient(runtime.NewScheme())
	appliedWork       = &workv1alpha1.AppliedWork{}
	ownerRef          = metav1.OwnerReference{
		APIVersion: workv1alpha1.GroupVersion.String(),
		Kind:       appliedWork.Kind,
		Name:       appliedWork.GetName(),
		UID:        appliedWork.GetUID(),
	}
	testGvr = schema.GroupVersionResource{
		Group:    "apps",
		Version:  "v1",
		Resource: "Deployment",
	}
	expectedGvr = schema.GroupVersionResource{
		Group:    "apps",
		Version:  "v1",
		Resource: "Deployment",
	}

	emptyGvr       = schema.GroupVersionResource{}
	testDeployment = appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Deployment",
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{Name: "TestDeployment"},
	}
	testPod = v1.Pod{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Pod",
			APIVersion: "core/v1",
		},
	}
	testInvalidYaml       = []byte(getRandomString())
	rawTestDeployment, _  = json.Marshal(testDeployment)
	rawInvalidResource, _ = json.Marshal(testInvalidYaml)
	rawMissingResource, _ = json.Marshal(testPod)
	testManifest          = workv1alpha1.Manifest{RawExtension: runtime.RawExtension{
		Raw: rawTestDeployment,
	}}

	InvalidManifest = workv1alpha1.Manifest{RawExtension: runtime.RawExtension{
		Raw: rawInvalidResource,
	}}

	MissingManifest = workv1alpha1.Manifest{RawExtension: runtime.RawExtension{
		Raw: rawMissingResource,
	}}
)

// This interface is needed for testMapper abstract class.
type testMapper struct {
	meta.RESTMapper
}

func (m testMapper) RESTMapping(gk schema.GroupKind, versions ...string) (*meta.RESTMapping, error) {
	if gk.Kind == "Deployment" {
		return &meta.RESTMapping{
			Resource:         testGvr,
			GroupVersionKind: testDeployment.GroupVersionKind(),
			Scope:            nil,
		}, nil
	} else {
		return nil, errors.New("test error: mapping does not exist.")
	}
}

func TestApplyManifest(t *testing.T) {
	testDeploymentWithOwnerRef := appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Deployment",
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{Name: "TestDeployment",
			OwnerReferences: []metav1.OwnerReference{
				ownerRef,
			}},
	}
	rawDeploymentWithOwnerRef, _ := json.Marshal(testDeploymentWithOwnerRef)
	testManifestWithOwnerRef := workv1alpha1.Manifest{RawExtension: runtime.RawExtension{
		Raw: rawDeploymentWithOwnerRef,
	}}
	failDynamicClient := fake.NewSimpleDynamicClient(runtime.NewScheme())
	failDynamicClient.PrependReactor("get", "*", func(action testing2.Action) (handled bool, ret runtime.Object, err error) {
		return true, nil, errors.New("Failed to apply an unstructrued object")
	})

	testCases := map[string]struct {
		reconciler ApplyWorkReconciler
		ml         []workv1alpha1.Manifest
		mcl        []workv1alpha1.ManifestCondition
		generation int64
		wantGvr    schema.GroupVersionResource
		wantErr    error
	}{
		"manifest is in proper format/ happy path": {
			reconciler: ApplyWorkReconciler{
				client:             &test.MockClient{},
				spokeDynamicClient: fakeDynamicClient,
				spokeClient:        &test.MockClient{},
				log:                logr.Logger{},
				restMapper:         testMapper{},
			},
			ml:         append([]workv1alpha1.Manifest{}, testManifest),
			mcl:        []workv1alpha1.ManifestCondition{},
			generation: 0,
			wantGvr:    expectedGvr,
			wantErr:    nil,
		},
		"manifest is in proper format/ owner reference already exists / should succeed": {
			reconciler: ApplyWorkReconciler{
				client:             &test.MockClient{},
				spokeDynamicClient: fakeDynamicClient,
				spokeClient:        &test.MockClient{},
				log:                logr.Logger{},
				restMapper:         testMapper{},
			},
			ml:         append([]workv1alpha1.Manifest{}, testManifestWithOwnerRef),
			mcl:        []workv1alpha1.ManifestCondition{},
			generation: 0,
			wantGvr:    expectedGvr,
			wantErr:    nil,
		},
		"manifest has incorrect syntax/ decode fail": {
			reconciler: ApplyWorkReconciler{
				client:             &test.MockClient{},
				spokeDynamicClient: fakeDynamicClient,
				spokeClient:        &test.MockClient{},
				log:                logr.Logger{},
				restMapper:         testMapper{},
			},
			ml:         append([]workv1alpha1.Manifest{}, InvalidManifest),
			mcl:        []workv1alpha1.ManifestCondition{},
			generation: 0,
			wantGvr:    emptyGvr,
			wantErr: &json2.UnmarshalTypeError{
				Value: "string",
				Type:  reflect.TypeOf(map[string]interface{}{}),
			},
		},
		"manifest is correct / object not mapped in restmapper / decode fail": {
			reconciler: ApplyWorkReconciler{
				client:             &test.MockClient{},
				spokeDynamicClient: fakeDynamicClient,
				spokeClient:        &test.MockClient{},
				log:                logr.Logger{},
				restMapper:         testMapper{},
			},
			ml:         append([]workv1alpha1.Manifest{}, MissingManifest),
			mcl:        []workv1alpha1.ManifestCondition{},
			generation: 0,
			wantGvr:    emptyGvr,
			wantErr:    errors.New("failed to find gvr from restmapping: test error: mapping does not exist."),
		},
		"manifest is in proper format / existing ManifestCondition does not have condition / fail": {
			reconciler: ApplyWorkReconciler{
				client:             &test.MockClient{},
				spokeDynamicClient: fakeDynamicClient,
				spokeClient:        &test.MockClient{},
				log:                logr.Logger{},
				restMapper:         testMapper{},
			},
			ml:         append([]workv1alpha1.Manifest{}, testManifest),
			mcl:        []workv1alpha1.ManifestCondition{},
			generation: 0,
			wantGvr:    expectedGvr,
			wantErr:    nil,
		},
		"manifest is in proper format / existing ManifestCondition has incorrect condition / fail": {
			reconciler: ApplyWorkReconciler{
				client:             &test.MockClient{},
				spokeDynamicClient: fakeDynamicClient,
				spokeClient:        &test.MockClient{},
				log:                logr.Logger{},
				restMapper:         testMapper{},
			},
			ml: append([]workv1alpha1.Manifest{}, testManifest),
			mcl: append([]workv1alpha1.ManifestCondition{}, workv1alpha1.ManifestCondition{
				Identifier: workv1alpha1.ResourceIdentifier{
					Ordinal:   0,
					Group:     testDeployment.GroupVersionKind().Group,
					Version:   testDeployment.GroupVersionKind().Version,
					Kind:      testDeployment.GroupVersionKind().Kind,
					Resource:  testGvr.Resource,
					Namespace: getRandomString(),
					Name:      getRandomString(),
				},
				Conditions: nil,
			}),
			generation: 0,
			wantGvr:    expectedGvr,
			wantErr:    nil,
		},
		"manifest is in proper format/ should fail applyUnstructured": {
			reconciler: ApplyWorkReconciler{
				client:             &test.MockClient{},
				spokeDynamicClient: failDynamicClient,
				spokeClient:        &test.MockClient{},
				log:                logr.Logger{},
				restMapper:         testMapper{},
			},
			ml:         append([]workv1alpha1.Manifest{}, testManifest),
			mcl:        []workv1alpha1.ManifestCondition{},
			generation: 0,
			wantGvr:    expectedGvr,
			wantErr:    errors.New("Failed to apply an unstructrued object"),
		},
	}

	for testName, testCase := range testCases {
		t.Run(testName, func(t *testing.T) {
			resultList := testCase.reconciler.applyManifests(testCase.ml, testCase.mcl, ownerRef)
			for _, result := range resultList {
				if testCase.wantErr != nil {
					assert.Containsf(t, result.err.Error(), testCase.wantErr.Error(), "Incorrect error for Testcase %s", testName)
				}
				if result.identifier.Kind == "Deployment" {
					assert.Equalf(t, testCase.generation, result.generation, "Testcase %s: generation incorrect", testName)
				} else {
					assert.Equalf(t, int64(0), result.generation, "Testcase %s: non-involved resources generation changed", testName)
				}
			}
		})
	}
}

func getRandomString() string {
	return utilrand.String(10)
}
