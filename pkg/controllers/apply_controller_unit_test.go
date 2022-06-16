package controllers

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/crossplane/crossplane-runtime/pkg/test"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/dynamic/fake"
	clienttesting "k8s.io/client-go/testing"

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
	testDeployment = appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Deployment",
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "Deployment",
			OwnerReferences: []metav1.OwnerReference{
				ownerRef,
			},
		},
		Spec: appsv1.DeploymentSpec{
			MinReadySeconds: 5,
		},
	}
	rawTestDeployment, _ = json.Marshal(testDeployment)
	testManifest         = workv1alpha1.Manifest{RawExtension: runtime.RawExtension{
		Raw: rawTestDeployment,
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
	// Manifests
	rawInvalidResource, _ := json.Marshal([]byte(getRandomString()))
	rawMissingResource, _ := json.Marshal(
		v1.Pod{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Pod",
				APIVersion: "core/v1",
			},
		})
	InvalidManifest := workv1alpha1.Manifest{RawExtension: runtime.RawExtension{
		Raw: rawInvalidResource,
	}}
	MissingManifest := workv1alpha1.Manifest{RawExtension: runtime.RawExtension{
		Raw: rawMissingResource,
	}}

	// GVRs
	expectedGvr := schema.GroupVersionResource{
		Group:    "apps",
		Version:  "v1",
		Resource: "Deployment",
	}
	emptyGvr := schema.GroupVersionResource{}

	// DynamicClients
	clientFailDynamicClient := fake.NewSimpleDynamicClient(runtime.NewScheme())
	clientFailDynamicClient.PrependReactor("get", "*", func(action clienttesting.Action) (handled bool, ret runtime.Object, err error) {
		return true, nil, errors.New("Failed to apply an unstructrued object")
	})

	testCases := map[string]struct {
		reconciler   ApplyWorkReconciler
		manifestList []workv1alpha1.Manifest
		generation   int64
		updated      bool
		wantGvr      schema.GroupVersionResource
		wantErr      error
	}{
		"manifest is in proper format/ happy path": {
			reconciler: ApplyWorkReconciler{
				client:             &test.MockClient{},
				spokeDynamicClient: fakeDynamicClient,
				spokeClient:        &test.MockClient{},
				restMapper:         testMapper{},
			},
			manifestList: append([]workv1alpha1.Manifest{}, testManifest),
			generation:   0,
			updated:      true,
			wantGvr:      expectedGvr,
			wantErr:      nil,
		},
		"manifest has incorrect syntax/ decode fail": {
			reconciler: ApplyWorkReconciler{
				client:             &test.MockClient{},
				spokeDynamicClient: fakeDynamicClient,
				spokeClient:        &test.MockClient{},
				restMapper:         testMapper{},
			},
			manifestList: append([]workv1alpha1.Manifest{}, InvalidManifest),
			generation:   0,
			updated:      false,
			wantGvr:      emptyGvr,
			wantErr: &json.UnmarshalTypeError{
				Value: "string",
				Type:  reflect.TypeOf(map[string]interface{}{}),
			},
		},
		"manifest is correct / object not mapped in restmapper / decode fail": {
			reconciler: ApplyWorkReconciler{
				client:             &test.MockClient{},
				spokeDynamicClient: fakeDynamicClient,
				spokeClient:        &test.MockClient{},
				restMapper:         testMapper{},
			},
			manifestList: append([]workv1alpha1.Manifest{}, MissingManifest),
			generation:   0,
			updated:      false,
			wantGvr:      emptyGvr,
			wantErr:      errors.New("failed to find gvr from restmapping: test error: mapping does not exist."),
		},
		"manifest is in proper format/ should fail applyUnstructured": {
			reconciler: ApplyWorkReconciler{
				client:             &test.MockClient{},
				spokeDynamicClient: clientFailDynamicClient,
				spokeClient:        &test.MockClient{},
				restMapper:         testMapper{},
			},
			manifestList: append([]workv1alpha1.Manifest{}, testManifest),
			generation:   0,
			updated:      false,
			wantGvr:      expectedGvr,
			wantErr:      errors.New("Failed to apply an unstructrued object"),
		},
	}

	for testName, testCase := range testCases {
		t.Run(testName, func(t *testing.T) {
			resultList := testCase.reconciler.applyManifests(context.Background(), testCase.manifestList, ownerRef)
			for _, result := range resultList {
				if testCase.wantErr != nil {
					assert.Containsf(t, result.err.Error(), testCase.wantErr.Error(), "Incorrect error for Testcase %s", testName)
				}
				assert.Equalf(t, testCase.generation, result.generation, "Testcase %s: generation incorrect", testName)
				assert.Equalf(t, testCase.updated, result.updated, "Testcase %s: Updated boolean incorrect", testName)
			}
		})
	}
}

func getRandomString() string {
	return utilrand.String(10)
}
