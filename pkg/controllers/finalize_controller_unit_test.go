package controllers

import (
	"context"
	"testing"
	"time"

	"github.com/crossplane/crossplane-runtime/pkg/test"
	"github.com/stretchr/testify/assert"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/work-api/pkg/utils"

	workv1alpha1 "sigs.k8s.io/work-api/pkg/apis/v1alpha1"
)

// TestWrapper is a struct used to encapsulate the mocked dependencies needed to simulate a specific flow in logic.
type TestWrapper struct {
	mockAppliedWork *workv1alpha1.AppliedWork
	mockReconciler  *FinalizeWorkReconciler
	mockWork        *workv1alpha1.Work
}

func TestGarbageCollectAppliedWork(t *testing.T) {

	// The happy path constitutes a Work object with a finalizer & an associated AppliedWork object
	happyPathTestWrapper := generateTestWrapper()
	controllerutil.AddFinalizer(happyPathTestWrapper.mockWork, workFinalizer)

	tests := map[string]struct {
		r              FinalizeWorkReconciler
		tw             TestWrapper
		expectedResult ctrl.Result
		expectedError  error
	}{
		"Happy Path: AppliedWork deleted, work finalizer removed": {
			r:  *happyPathTestWrapper.mockReconciler,
			tw: *happyPathTestWrapper,
		},
	}
	for testName, tt := range tests {
		t.Run(testName, func(t *testing.T) {
			_, err := tt.r.garbageCollectAppliedWork(context.Background(), tt.tw.mockWork)

			assert.False(t, controllerutil.ContainsFinalizer(tt.tw.mockWork, workFinalizer), "The Work object still contains a finalizer, it should not.")
			assert.NoError(t, err, "An error occurred but none was expected.")
		})
	}
}

func TestFinalizerReconcile(t *testing.T) {

	tests := map[string]struct {
		r              FinalizeWorkReconciler
		req            ctrl.Request
		expectedResult ctrl.Result
		expectedError  error
	}{
		"Controller not joined": {
			r: FinalizeWorkReconciler{Joined: false},
			req: ctrl.Request{
				NamespacedName: types.NamespacedName{
					Namespace: "work" + rand.String(5),
					Name:      "work" + rand.String(5),
				},
			},
			expectedResult: ctrl.Result{RequeueAfter: time.Second * 5},
			expectedError:  nil,
		},
	}
	for testName, tt := range tests {
		t.Run(testName, func(t *testing.T) {
			ctrlResult, err := tt.r.Reconcile(ctx, tt.req)
			assert.Equalf(t, tt.expectedResult, ctrlResult, "wrong ctrlResult for testcase %s", testName)
			assert.Equal(t, tt.expectedError, err)
		})
	}
}

func generateWork(workName string, workNamespace string) *workv1alpha1.Work {
	return &workv1alpha1.Work{
		ObjectMeta: metaV1.ObjectMeta{
			Name:      workName,
			Namespace: workNamespace,
		},
		Spec: workv1alpha1.WorkSpec{
			Workload: workv1alpha1.WorkloadTemplate{
				Manifests: []workv1alpha1.Manifest{},
			},
		},
	}
}

func generateAppliedWork(workName string) *workv1alpha1.AppliedWork {
	return &workv1alpha1.AppliedWork{
		ObjectMeta: metaV1.ObjectMeta{
			Name: workName,
		},
		Spec: workv1alpha1.AppliedWorkSpec{
			WorkName: workName,
		},
	}
}

func generateTestWrapper() *TestWrapper {
	workName := rand.String(5)
	workNameSpace := rand.String(5)

	mockAppliedWork := generateAppliedWork(workName)
	mockWork := generateWork(workName, workNameSpace)

	return &TestWrapper{
		mockReconciler: &FinalizeWorkReconciler{
			client:      test.NewMockClient(),
			recorder:    utils.NewFakeRecorder(2),
			spokeClient: test.NewMockClient(),
			Joined:      true,
		},
		mockAppliedWork: mockAppliedWork,
		mockWork:        mockWork,
	}
}
