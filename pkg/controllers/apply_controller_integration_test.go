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
	"sigs.k8s.io/work-api/pkg/utils"
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

	const (
		timeout  = time.Second * 30
		interval = time.Second * 1
	)

	var (
		resourceName      string
		resourceNamespace string
		workName          string
		workNamespace     string
		configMapA        corev1.ConfigMap
		work              workv1alpha1.Work
	)

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
		By("Create a namespace for Work objects")
		_, err := k8sClient.CoreV1().Namespaces().Create(context.Background(), wns, metav1.CreateOptions{})
		Expect(err).Should(SatisfyAny(Succeed(), &utils.AlreadyExistMatcher{}))

		rns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: resourceNamespace,
			},
		}
		By("Create a namespace for Manifest Resources")
		_, err = k8sClient.CoreV1().Namespaces().Create(context.Background(), rns, metav1.CreateOptions{})
		Expect(err).Should(SatisfyAny(Succeed(), &utils.AlreadyExistMatcher{}))

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

		work = workv1alpha1.Work{
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
	})

	AfterEach(func() {
		Expect(workClient.MulticlusterV1alpha1().Works(workNamespace).Delete(context.Background(), workName, metav1.DeleteOptions{})).Should(Succeed())
	})
	Context("Work Creation Process", func() {
		It("create a new work object in the hub cluster", func() {
			_, err := workClient.MulticlusterV1alpha1().Works(workNamespace).Create(context.Background(), &work, metav1.CreateOptions{})
			Expect(err).ShouldNot(HaveOccurred())

			By("work object should eventually contain a finalizer.")
			Eventually(func() bool {
				createdWork, err := workClient.MulticlusterV1alpha1().Works(workNamespace).Get(context.Background(), workName, metav1.GetOptions{})
				if err != nil {
					return false
				}
				return controllerutil.ContainsFinalizer(createdWork, workFinalizer)
			}, timeout, interval).Should(BeTrue())

			By("corresponding appliedWork Resource should have been created.")
			Eventually(func() bool {
				appliedWorkObject, err := workClient.MulticlusterV1alpha1().AppliedWorks().Get(context.Background(), workName, metav1.GetOptions{})
				if err != nil {
					return false
				}
				return appliedWorkObject.Spec.WorkName == workName
			}, timeout, interval).Should(BeTrue())

			By("manifests should have been applied.")
			Eventually(func() bool {
				resource, err := k8sClient.CoreV1().ConfigMaps(resourceNamespace).Get(context.Background(), resourceName, metav1.GetOptions{})
				if err != nil {
					return false
				}
				return resource.GetName() == resourceName
			}, timeout, interval).Should(BeTrue())

			By("status should have been saved for each manifests applied.")
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

			By("AppliedResourceMeta details should have been created on AppliedWork's status")
			Eventually(func() bool {
				appliedWorkObject, err := workClient.MulticlusterV1alpha1().AppliedWorks().Get(context.Background(), workName, metav1.GetOptions{})
				if err != nil {
					return false
				}
				return appliedWorkObject.Status.AppliedResources[0].Name == resourceName &&
					appliedWorkObject.Status.AppliedResources[0].Namespace == resourceNamespace
			}, timeout, interval).Should(BeTrue())
		})

		Context("Work is being updated", func() {
			It("modify a manifest, then update the work object", func() {
				By("creating a new work object")
				createdWork, err := workClient.MulticlusterV1alpha1().Works(workNamespace).Create(context.Background(), &work, metav1.CreateOptions{})
				Expect(err).ShouldNot(HaveOccurred())

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

				createdWork.Spec.Workload.Manifests = []workv1alpha1.Manifest{
					{
						RawExtension: runtime.RawExtension{Object: &cm},
					},
				}

				By("Updating the work object")
				_, err = workClient.MulticlusterV1alpha1().Works(workNamespace).Update(context.Background(), createdWork, metav1.UpdateOptions{})
				Expect(err).ShouldNot(HaveOccurred())

				By("Work status should be updated")
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

				By("the manifest resource should be created")
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
