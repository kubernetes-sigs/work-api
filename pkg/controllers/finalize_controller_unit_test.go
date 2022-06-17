package controllers

import (
	"context"
	"testing"

	"github.com/crossplane/crossplane-runtime/pkg/test"
	"github.com/stretchr/testify/assert"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	workv1alpha1 "sigs.k8s.io/work-api/pkg/apis/v1alpha1"
	"sigs.k8s.io/work-api/pkg/client/clientset/versioned/fake"
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
			client: test.NewMockClient(),
			spokeClient: fake.NewSimpleClientset(
				mockWork,
				mockAppliedWork,
			)},
		mockAppliedWork: mockAppliedWork,
		mockWork:        mockWork,
	}
}
