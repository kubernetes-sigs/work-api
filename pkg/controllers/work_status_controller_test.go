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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"
	ctrl "sigs.k8s.io/controller-runtime"

	"sigs.k8s.io/work-api/pkg/apis/v1alpha1"
)

// TestCalculateNewAppliedWork validates the calculation logic between the Work & AppliedWork resources.
// The result of the tests pass back a collection of resources that should either
// be applied to the member cluster or removed.
func TestCalculateNewAppliedWork(t *testing.T) {
	identifier := generateResourceIdentifier()
	inputWork := generateWorkObj(nil)
	inputWorkWithResourceIdentifier := generateWorkObj(&identifier)
	inputAppliedWork := generateAppliedWorkObj(nil)
	inputAppliedWorkWithResourceIdentifier := generateAppliedWorkObj(&identifier)

	tests := map[string]struct {
		r                WorkStatusReconciler
		inputWork        v1alpha1.Work
		inputAppliedWork v1alpha1.AppliedWork
		expectedNewRes   []v1alpha1.AppliedResourceMeta
		expectedStaleRes []v1alpha1.AppliedResourceMeta
		hasErr           bool
	}{
		"AppliedWork and Work has been garbage collected; AppliedWork and Work of a resource both does not exist": {
			r:                WorkStatusReconciler{joined: true},
			inputWork:        inputWork,
			inputAppliedWork: inputAppliedWork,
			expectedNewRes:   []v1alpha1.AppliedResourceMeta(nil),
			expectedStaleRes: []v1alpha1.AppliedResourceMeta(nil),
		},
		"AppliedWork and Work of a resource exists; there are nothing being deleted": {
			r:                WorkStatusReconciler{joined: true},
			inputWork:        inputWorkWithResourceIdentifier,
			inputAppliedWork: inputAppliedWorkWithResourceIdentifier,
			expectedNewRes: []v1alpha1.AppliedResourceMeta{
				{
					ResourceIdentifier: inputAppliedWorkWithResourceIdentifier.Status.AppliedResources[0].ResourceIdentifier,
					UID:                inputAppliedWorkWithResourceIdentifier.Status.AppliedResources[0].UID,
				},
			},
			expectedStaleRes: []v1alpha1.AppliedResourceMeta(nil),
		},
		"Work resource has been deleted, but the corresponding AppliedWork remains": {
			r:                WorkStatusReconciler{joined: true},
			inputWork:        inputWork,
			inputAppliedWork: inputAppliedWorkWithResourceIdentifier,
			expectedNewRes:   []v1alpha1.AppliedResourceMeta(nil),
			expectedStaleRes: []v1alpha1.AppliedResourceMeta{
				{
					ResourceIdentifier: inputAppliedWorkWithResourceIdentifier.Status.AppliedResources[0].ResourceIdentifier,
					UID:                inputAppliedWorkWithResourceIdentifier.Status.AppliedResources[0].UID,
				},
			},
		},
	}
	for testName, tt := range tests {
		t.Run(testName, func(t *testing.T) {
			newRes, staleRes, err := tt.r.generateDiff(context.Background(), &tt.inputWork, &tt.inputAppliedWork)
			assert.Equalf(t, tt.expectedNewRes, newRes, "Testcase %s: NewRes is different from what it should be.", testName)
			assert.Equalf(t, tt.expectedStaleRes, staleRes, "Testcase %s: StaleRes is different from what it should be.", testName)
			if tt.hasErr {
				assert.Truef(t, err != nil, "Testcase %s: Should get an err.", testName)
			}
		})
	}
}

func TestStop(t *testing.T) {
	testCases := map[string]struct {
		reconciler WorkStatusReconciler
		ctrlResult ctrl.Result
		wantErr    error
	}{
		"controller is being stopped": {
			reconciler: WorkStatusReconciler{
				joined: false,
			},
			ctrlResult: ctrl.Result{RequeueAfter: time.Second * 5},
			wantErr:    nil,
		},
	}

	for testName, testCase := range testCases {
		t.Run(testName, func(t *testing.T) {
			ctrlResult, err := testCase.reconciler.Reconcile(context.Background(), ctrl.Request{
				NamespacedName: types.NamespacedName{
					Namespace: "work" + rand.String(5),
					Name:      "work" + rand.String(5),
				},
			})
			assert.Equalf(t, testCase.ctrlResult, ctrlResult, "wrong ctrlResult for testcase %s", testName)
			assert.Equal(t, testCase.wantErr, err)
		})
	}
}

func generateWorkObj(identifier *v1alpha1.ResourceIdentifier) v1alpha1.Work {
	if identifier != nil {
		return v1alpha1.Work{
			Status: v1alpha1.WorkStatus{
				ManifestConditions: []v1alpha1.ManifestCondition{
					{
						Identifier: *identifier,
						Conditions: []metav1.Condition{
							{
								Type:   ConditionTypeApplied,
								Status: metav1.ConditionTrue,
							},
						},
					},
				},
			},
		}
	} else {
		return v1alpha1.Work{}
	}
}

func generateAppliedWorkObj(identifier *v1alpha1.ResourceIdentifier) v1alpha1.AppliedWork {
	if identifier != nil {
		return v1alpha1.AppliedWork{
			TypeMeta:   metav1.TypeMeta{},
			ObjectMeta: metav1.ObjectMeta{},
			Spec:       v1alpha1.AppliedWorkSpec{},
			Status: v1alpha1.AppliedtWorkStatus{
				AppliedResources: []v1alpha1.AppliedResourceMeta{
					{
						ResourceIdentifier: *identifier,
						UID:                types.UID(rand.String(20)),
					},
				},
			},
		}
	} else {
		return v1alpha1.AppliedWork{}
	}
}

func generateResourceIdentifier() v1alpha1.ResourceIdentifier {
	return v1alpha1.ResourceIdentifier{
		Ordinal:   rand.Int(),
		Group:     rand.String(10),
		Version:   rand.String(10),
		Kind:      rand.String(10),
		Resource:  rand.String(10),
		Namespace: rand.String(10),
		Name:      rand.String(10),
	}
}