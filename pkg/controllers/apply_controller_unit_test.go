package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/crossplane/crossplane-runtime/pkg/test"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/fake"
	testingclient "k8s.io/client-go/testing"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

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
	clientFailDynamicClient.PrependReactor("get", "*", func(action testingclient.Action) (handled bool, ret runtime.Object, err error) {
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

func TestReconcile(t *testing.T) {
	workNamespace := getRandomString()
	workName := getRandomString()
	appliedWorkName := getRandomString()
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Namespace: workNamespace,
			Name:      workName,
		},
	}
	wrongReq := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Namespace: getRandomString(),
			Name:      getRandomString(),
		},
	}
	invalidReq := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Namespace: "",
			Name:      "",
		},
	}

	getMock := func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
		if key.Namespace != workNamespace {
			return &apierrors.StatusError{
				ErrStatus: metav1.Status{
					Status: metav1.StatusFailure,
					Reason: metav1.StatusReasonNotFound,
				}}
		}
		o, _ := obj.(*workv1alpha1.Work)
		*o = workv1alpha1.Work{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:  workNamespace,
				Name:       workName,
				Finalizers: []string{"multicluster.x-k8s.io/work-cleanup"},
			},
			Spec: workv1alpha1.WorkSpec{Workload: workv1alpha1.WorkloadTemplate{Manifests: []workv1alpha1.Manifest{testManifest}}},
		}
		return nil
	}

	happyDeployment := appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Deployment",
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "Deployment",
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: workv1alpha1.GroupVersion.String(),
					Kind:       "AppliedWork",
					Name:       appliedWorkName,
				},
			},
		},
		Spec: appsv1.DeploymentSpec{
			MinReadySeconds: 5,
		},
	}
	rawHappyDeployment, _ := json.Marshal(happyDeployment)
	happyManifest := workv1alpha1.Manifest{RawExtension: runtime.RawExtension{
		Raw: rawHappyDeployment,
	}}
	_, happyDynamicClient, _ := createObjAndDynamicClient(happyManifest.Raw)

	getMockAppliedWork := func(ctx context.Context, key client.ObjectKey, obj client.Object) error {

		if key.Name != workName {
			return &apierrors.StatusError{
				ErrStatus: metav1.Status{
					Status: metav1.StatusFailure,
					Reason: metav1.StatusReasonNotFound,
				}}
		}
		o, _ := obj.(*workv1alpha1.AppliedWork)
		*o = workv1alpha1.AppliedWork{
			ObjectMeta: metav1.ObjectMeta{
				Name: appliedWorkName,
			},
			Spec: workv1alpha1.AppliedWorkSpec{
				WorkName:      workNamespace,
				WorkNamespace: workName,
			},
		}
		return nil
	}

	clientFailDynamicClient := fake.NewSimpleDynamicClient(runtime.NewScheme())
	clientFailDynamicClient.PrependReactor("get", "*", func(action testingclient.Action) (handled bool, ret runtime.Object, err error) {
		return true, nil, errors.New("Failed to apply an unstructrued object")
	})

	testCases := map[string]struct {
		reconciler ApplyWorkReconciler
		req        ctrl.Request
		wantErr    error
	}{
		"work cannot be retrieved, client failed due to client error": {
			reconciler: ApplyWorkReconciler{
				client: &test.MockClient{
					MockGet: func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
						return fmt.Errorf("client failing")
					},
				},
				spokeDynamicClient: fakeDynamicClient,
				spokeClient:        &test.MockClient{},
				restMapper:         testMapper{},
			},
			req:     invalidReq,
			wantErr: errors.New("client failing"),
		},
		"work cannot be retrieved, client failed due to not found error": {
			reconciler: ApplyWorkReconciler{
				client: &test.MockClient{
					MockGet: getMock,
				},
				spokeDynamicClient: fakeDynamicClient,
				spokeClient:        &test.MockClient{},
				restMapper:         testMapper{},
			},
			req:     wrongReq,
			wantErr: nil,
		},
		"work without finalizer / no error": {
			reconciler: ApplyWorkReconciler{
				client: &test.MockClient{
					MockGet: func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
						o, _ := obj.(*workv1alpha1.Work)
						*o = workv1alpha1.Work{
							ObjectMeta: metav1.ObjectMeta{
								Namespace: workNamespace,
								Name:      workName,
							},
						}
						return nil
					},
				},
				spokeDynamicClient: fakeDynamicClient,
				spokeClient:        &test.MockClient{},
				restMapper:         testMapper{},
			},
			req:     req,
			wantErr: nil,
		},
		"work with non-zero deletion-timestamp / succeed": {
			reconciler: ApplyWorkReconciler{
				client: &test.MockClient{
					MockGet: func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
						o, _ := obj.(*workv1alpha1.Work)
						*o = workv1alpha1.Work{
							ObjectMeta: metav1.ObjectMeta{
								Namespace:         workNamespace,
								Name:              workName,
								Finalizers:        []string{"multicluster.x-k8s.io/work-cleanup"},
								DeletionTimestamp: &metav1.Time{Time: time.Now()},
							},
						}
						return nil
					},
				},
				spokeDynamicClient: fakeDynamicClient,
				spokeClient:        &test.MockClient{},
				restMapper:         testMapper{},
			},
			req:     req,
			wantErr: nil,
		},
		"Retrieving appliedwork fails": {
			reconciler: ApplyWorkReconciler{
				client: &test.MockClient{
					MockGet: getMock,
				},
				spokeDynamicClient: fakeDynamicClient,
				spokeClient: &test.MockClient{
					MockGet: func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
						return &apierrors.StatusError{
							ErrStatus: metav1.Status{
								Status: metav1.StatusFailure,
								Reason: metav1.StatusReasonNotFound,
							}}
					},
				},
				restMapper: testMapper{},
			},
			req:     req,
			wantErr: errors.New("failed to get the appliedWork"),
		},
		"ApplyManifest fails": {
			reconciler: ApplyWorkReconciler{
				client: &test.MockClient{
					MockGet: getMock,
					MockStatusUpdate: func(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
						return nil
					},
				},
				spokeDynamicClient: clientFailDynamicClient,
				spokeClient: &test.MockClient{
					MockGet: getMockAppliedWork,
					MockStatusUpdate: func(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
						return nil
					},
				},
				restMapper: testMapper{},
			},
			req:     req,
			wantErr: errors.New("Failed to apply an unstructrued object"),
		},
		"client update fails": {
			reconciler: ApplyWorkReconciler{
				client: &test.MockClient{
					MockGet: getMock,
					MockStatusUpdate: func(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
						return errors.New("update work status failed")
					},
				},
				spokeDynamicClient: clientFailDynamicClient,
				spokeClient: &test.MockClient{
					MockGet: getMockAppliedWork,
				},
				restMapper: testMapper{},
			},
			req:     req,
			wantErr: errors.New("update work status failed"),
		},
		"Happy Path": {
			reconciler: ApplyWorkReconciler{
				client: &test.MockClient{
					MockGet: getMock,
					MockStatusUpdate: func(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
						return nil
					},
				},
				spokeDynamicClient: happyDynamicClient,
				spokeClient: &test.MockClient{
					MockGet: getMockAppliedWork,
				},
				restMapper: testMapper{},
			},
			req:     req,
			wantErr: nil,
		},
	}
	for testName, testCase := range testCases {
		t.Run(testName, func(t *testing.T) {
			ctrlResult, err := testCase.reconciler.Reconcile(context.Background(), testCase.req)
			if testCase.wantErr != nil {
				assert.Containsf(t, err.Error(), testCase.wantErr.Error(), "incorrect error for Testcase %s", testName)
			} else {
				assert.Equalf(t, false, ctrlResult.Requeue, "incorrect ctrlResult for Testcase %s", testName)
			}
		})
	}
}

func createObjAndDynamicClient(rawManifest []byte) (unstructured.Unstructured, dynamic.Interface, string) {
	unstructuredObj := &unstructured.Unstructured{}
	_ = unstructuredObj.UnmarshalJSON(rawManifest)
	validSpecHash, _ := generateSpecHash(unstructuredObj)
	unstructuredObj.SetAnnotations(map[string]string{"multicluster.x-k8s.io/spec-hash": validSpecHash})
	dynamicClient := fake.NewSimpleDynamicClient(runtime.NewScheme())
	dynamicClient.PrependReactor("get", "*", func(action testingclient.Action) (handled bool, ret runtime.Object, err error) {
		return true, unstructuredObj, nil
	})
	dynamicClient.PrependReactor("patch", "*", func(action testingclient.Action) (handled bool, ret runtime.Object, err error) {
		return true, unstructuredObj, nil
	})
	return *unstructuredObj, dynamicClient, validSpecHash
}

func getRandomString() string {
	return rand.String(10)
}
