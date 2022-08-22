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
	"encoding/json"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilrand "k8s.io/apimachinery/pkg/util/rand"

	workv1alpha1 "sigs.k8s.io/work-api/pkg/apis/v1alpha1"
)

const timeout = time.Second * 30
const interval = time.Second * 1

var _ = Describe("Work Controller", func() {
	var workNamespace string
	var ns corev1.Namespace

	BeforeEach(func() {
		workNamespace = "work-" + utilrand.String(5)
		// Create namespace
		ns = corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: workNamespace,
			},
		}
		err := k8sClient.Create(context.Background(), &ns)
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		// Add any teardown steps that needs to be executed after each test
		err := k8sClient.Delete(context.Background(), &ns)
		Expect(err).ToNot(HaveOccurred())
	})

	Context("Deploy manifests by work", func() {
		It("Should have a configmap deployed correctly", func() {
			cmName := "testcm"
			cmNamespace := "default"
			cm := &corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "ConfigMap",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      cmName,
					Namespace: cmNamespace,
				},
				Data: map[string]string{
					"test": "test",
				},
			}

			work := &workv1alpha1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-work",
					Namespace: workNamespace,
				},
				Spec: workv1alpha1.WorkSpec{
					Workload: workv1alpha1.WorkloadTemplate{
						Manifests: []workv1alpha1.Manifest{
							{
								RawExtension: runtime.RawExtension{Object: cm},
							},
						},
					},
				},
			}
			By("create the work")
			err := k8sClient.Create(context.Background(), work)
			Expect(err).ToNot(HaveOccurred())

			resultWork := waitForWorkToApply(work.GetName(), work.GetNamespace())
			Expect(len(resultWork.Status.ManifestConditions)).Should(Equal(1))
			Expect(meta.IsStatusConditionTrue(resultWork.Status.Conditions, ConditionTypeApplied)).Should(BeTrue())
			Expect(meta.IsStatusConditionTrue(resultWork.Status.ManifestConditions[0].Conditions, ConditionTypeApplied)).Should(BeTrue())

			By("Check applied config map")
			var configMap corev1.ConfigMap
			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Name: cmName, Namespace: cmNamespace}, &configMap)).Should(Succeed())
			Expect(len(configMap.Data)).Should(Equal(1))
			Expect(configMap.Data["test"]).Should(Equal("test"))
		})

		FIt("Should pick up the manifest change correctly", func() {
			cmName := "testserverapply"
			cmNamespace := "default"
			cm := &corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "ConfigMap",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      cmName,
					Namespace: cmNamespace,
					Labels: map[string]string{
						"labelKey1": "value1",
						"labelKey2": "value2",
					},
					Annotations: map[string]string{
						"annotationKey1": "annotation1",
						"annotationKey2": "annotation2",
					},
				},
				Data: map[string]string{
					"data1": "test1",
				},
			}

			work := &workv1alpha1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-label-work",
					Namespace: workNamespace,
				},
				Spec: workv1alpha1.WorkSpec{
					Workload: workv1alpha1.WorkloadTemplate{
						Manifests: []workv1alpha1.Manifest{
							{
								RawExtension: runtime.RawExtension{Object: cm},
							},
						},
					},
				},
			}
			By("create the work")
			err := k8sClient.Create(context.Background(), work)
			Expect(err).ToNot(HaveOccurred())

			By("wait for the work to be applied")
			waitForWorkToApply(work.GetName(), work.GetNamespace())

			By("Check applied config map")
			var configMap corev1.ConfigMap
			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Name: cmName, Namespace: cmNamespace}, &configMap)).Should(Succeed())

			By("Check the config map label")
			Expect(len(configMap.Labels)).Should(Equal(2))
			Expect(configMap.Labels["labelKey1"]).Should(Equal(cm.Labels["labelKey1"]))
			Expect(configMap.Labels["labelKey2"]).Should(Equal(cm.Labels["labelKey2"]))

			By("Check the config map annotation value")
			Expect(len(configMap.Annotations)).Should(Equal(3)) // we added one more annotation (manifest hash)
			Expect(configMap.Annotations["annotationKey1"]).Should(Equal(cm.Annotations["annotationKey1"]))
			Expect(configMap.Annotations["annotationKey2"]).Should(Equal(cm.Annotations["annotationKey2"]))

			By("Check the config map data")
			Expect(len(configMap.Data)).Should(Equal(1))
			Expect(configMap.Data["data1"]).Should(Equal(cm.Data["data1"]))

			By("Modify the configMap")
			// add new data
			cm.Data["data2"] = "test2"
			// modify one data
			cm.Data["data1"] = "newValue"
			// modify label key1
			cm.Labels["labelKey1"] = "newValue"
			// remove label key2
			delete(cm.Labels, "labelKey2")
			// add annotations key3
			cm.Annotations["annotationKey3"] = "annotation3"
			// remove annotations key1
			delete(cm.Annotations, "annotationKey1")

			By("update the work")
			resultWork := waitForWorkToApply(work.GetName(), work.GetNamespace())
			rawCM, err := json.Marshal(cm)
			Expect(err).Should(Succeed())
			resultWork.Spec.Workload.Manifests[0].Raw = rawCM
			Expect(k8sClient.Update(context.Background(), resultWork)).Should(Succeed())

			By("wait for the change of the work to be applied")
			resultWork = waitForWorkToApply(work.GetName(), work.GetNamespace())

			By("Get the last applied config map")
			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Name: cmName, Namespace: cmNamespace}, &configMap)).Should(Succeed())
			/*
				By("Check the config map data")
				Expect(len(configMap.Data)).Should(Equal(2))
				Expect(configMap.Data["data1"]).Should(Equal(cm.Data["data1"]))
				Expect(configMap.Data["data2"]).Should(Equal(cm.Data["data2"]))

				By("Check the config map label")
				Expect(len(configMap.Labels)).Should(Equal(1))
				Expect(configMap.Labels["labelKey1"]).Should(Equal(cm.Labels["labelKey1"]))

				By("Check the config map annotation value")
				Expect(len(configMap.Annotations)).Should(Equal(3)) // we added one more annotation (manifest hash)
				_, found := configMap.Annotations["annotationKey1"]
				Expect(found).Should(BeFalse())
				Expect(configMap.Annotations["annotationKey2"]).Should(Equal(cm.Annotations["annotationKey2"]))
				Expect(configMap.Annotations["annotationKey3"]).Should(Equal(cm.Annotations["annotationKey3"]))
			*/
		})
	})
})

func waitForWorkToApply(workName, workNS string) *workv1alpha1.Work {
	By("Wait for the work to be applied")
	var resultWork workv1alpha1.Work
	Eventually(func() bool {
		err := k8sClient.Get(context.Background(), types.NamespacedName{Name: workName, Namespace: workNS}, &resultWork)
		if err != nil {
			return false
		}
		if len(resultWork.Status.ManifestConditions) != 1 {
			return false
		}
		if !meta.IsStatusConditionTrue(resultWork.Status.ManifestConditions[0].Conditions, ConditionTypeApplied) {
			return false
		}
		applyCond := meta.FindStatusCondition(resultWork.Status.Conditions, ConditionTypeApplied)
		if applyCond.Status != metav1.ConditionTrue || applyCond.ObservedGeneration != resultWork.Generation {
			return false
		}
		return true
	}, timeout, interval).Should(BeTrue())
	return &resultWork
}
