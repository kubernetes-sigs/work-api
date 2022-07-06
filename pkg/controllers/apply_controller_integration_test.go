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
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	workv1alpha1 "sigs.k8s.io/work-api/pkg/apis/v1alpha1"
)

var _ = Describe("work reconciler", func() {
	var resourceName string
	var resourceNamespace string
	var workName string
	var workNamespace string
	var configMapA corev1.ConfigMap

	const timeout = time.Second * 30
	const interval = time.Second * 1

	BeforeEach(func() {
		workName = utilrand.String(5)
		workNamespace = utilrand.String(5)
		resourceName = utilrand.String(5)
		resourceNamespace = utilrand.String(5)

		wns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: workNamespace,
			},
		}
		_, err := k8sClient.CoreV1().Namespaces().Create(context.Background(), wns, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		rns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: resourceNamespace,
			},
		}
		_, err = k8sClient.CoreV1().Namespaces().Create(context.Background(), rns, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		configMapA = corev1.ConfigMap{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "v1",
				Kind:       "ConfigMap",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      resourceName,
				Namespace: resourceNamespace,
			},
			Data: map[string]string{
				"test": "test",
			},
		}

		work := &workv1alpha1.Work{
			ObjectMeta: metav1.ObjectMeta{
				Name:      workName,
				Namespace: workNamespace,
			},
			Spec: workv1alpha1.WorkSpec{
				Workload: workv1alpha1.WorkloadTemplate{
					Manifests: []workv1alpha1.Manifest{
						{
							RawExtension: runtime.RawExtension{Object: &configMapA},
						},
					},
				},
			},
		}

		_, createWorkErr := workClient.MulticlusterV1alpha1().Works(workNamespace).Create(context.Background(), work, metav1.CreateOptions{})
		Expect(createWorkErr).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		err := k8sClient.CoreV1().Namespaces().Delete(context.Background(), workNamespace, metav1.DeleteOptions{})
		Expect(err).ToNot(HaveOccurred())
	})

	Context("receives a Work to reconcile", func() {
		It("should verify that the Work contains multicluster.x-k8s.io/work-cleanup finalizer", func() {
			Eventually(func() bool {
				currentWork, err := workClient.MulticlusterV1alpha1().Works(workNamespace).Get(context.Background(), workName, metav1.GetOptions{})
				Expect(err).ToNot(HaveOccurred())
				return controllerutil.ContainsFinalizer(currentWork, workFinalizer)
			}, timeout, interval).Should(BeTrue())
		})

		It("should have created an AppliedWork Resource", func() {
			Eventually(func() bool {
				appliedWorkObject, err := workClient.MulticlusterV1alpha1().AppliedWorks().Get(context.Background(), workName, metav1.GetOptions{})
				if err == nil {
					return appliedWorkObject.Spec.WorkName == workName
				}
				return false
			}, timeout, interval).Should(BeTrue())
		})

		It("should have applied the resource manifest", func() {
			Eventually(func() bool {
				resource, err := k8sClient.CoreV1().ConfigMaps(resourceNamespace).Get(context.Background(), resourceName, metav1.GetOptions{})
				if err == nil {
					return resource.GetName() == resourceName
				}
				return false
			}, timeout, interval).Should(BeTrue())
		})

		It("should save the conditions for the manifest", func() {
			Eventually(func() bool {
				currentWork, err := workClient.MulticlusterV1alpha1().Works(workNamespace).Get(context.Background(), workName, metav1.GetOptions{})
				if err == nil {
					if len(currentWork.Status.ManifestConditions) > 0 {
						return currentWork.Status.ManifestConditions[0].Identifier.Name == resourceName &&
							currentWork.Status.ManifestConditions[0].Identifier.Namespace == resourceNamespace &&
							currentWork.Status.Conditions[0].Status == metav1.ConditionTrue
					}
				}
				return false
			}, timeout, interval).Should(BeTrue())
		})

		It("should have created AppliedResourceMeta details on the AppliedWork's status", func() {
			Eventually(func() bool {
				appliedWorkObject, err := workClient.MulticlusterV1alpha1().AppliedWorks().Get(context.Background(), workName, metav1.GetOptions{})
				if err == nil {
					if len(appliedWorkObject.Status.AppliedResources) > 0 {
						return appliedWorkObject.Status.AppliedResources[0].Name == resourceName &&
							appliedWorkObject.Status.AppliedResources[0].Namespace == resourceNamespace
					}
				}
				return false
			}, timeout, interval).Should(BeTrue())
		})
	})

	Context("updating existing Work", func() {
		It("should update the resources", func() {
			cm := corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "ConfigMap",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: resourceNamespace,
				},
				Data: map[string]string{
					"updated_test": "updated_test",
				},
			}

			var originalWork *workv1alpha1.Work
			var err error
			originalWork, err = workClient.MulticlusterV1alpha1().Works(workNamespace).Get(context.Background(), workName, metav1.GetOptions{})
			Expect(err).ToNot(HaveOccurred())

			newWork := originalWork.DeepCopy()
			newWork.Spec.Workload.Manifests = []workv1alpha1.Manifest{
				{
					RawExtension: runtime.RawExtension{Object: &cm},
				},
			}
			_, err = workClient.MulticlusterV1alpha1().Works(workNamespace).Update(context.Background(), newWork, metav1.UpdateOptions{})
			Expect(err).ToNot(HaveOccurred())

			By("Work status", func() {
				Eventually(func() bool {
					currentWork, err := workClient.MulticlusterV1alpha1().Works(workNamespace).Get(context.Background(), workName, metav1.GetOptions{})
					if err == nil {
						if len(currentWork.Status.Conditions) > 0 && len(currentWork.Status.ManifestConditions) > 0 {
							return currentWork.Status.ManifestConditions[0].Identifier.Name == cm.Name &&
								currentWork.Status.ManifestConditions[0].Identifier.Namespace == cm.Namespace &&
								currentWork.Status.Conditions[0].Type == ConditionTypeApplied &&
								currentWork.Status.Conditions[0].Status == metav1.ConditionTrue
						}
					}

					return false
				}, timeout, interval).Should(BeTrue())
			})

			By("created resources", func() {
				Eventually(func() bool {
					configMap, err := k8sClient.CoreV1().ConfigMaps(resourceNamespace).Get(context.Background(), resourceName, metav1.GetOptions{})
					if err == nil {
						return configMap.Data["updated_test"] == "updated_test"
					}
					return false
				}, timeout, interval).Should(BeTrue())
			})
		})
	})
})
