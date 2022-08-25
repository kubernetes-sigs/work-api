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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilrand "k8s.io/apimachinery/pkg/util/rand"

	workv1alpha1 "sigs.k8s.io/work-api/pkg/apis/v1alpha1"
)

var _ = Describe("Work Status Reconciler", func() {
	var resourceName string
	var resourceNamespace string
	var workName string
	var workNamespace string

	const timeout = time.Second * 30
	const interval = time.Second * 1
	var wns corev1.Namespace
	var rns corev1.Namespace

	BeforeEach(func() {
		workName = "work-" + utilrand.String(5)
		workNamespace = "cluster-" + utilrand.String(5)
		resourceName = "configmap-" + utilrand.String(5)
		resourceNamespace = utilrand.String(5)

		wns = corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: workNamespace,
			},
		}
		err := k8sClient.Create(context.Background(), &wns)
		Expect(err).ToNot(HaveOccurred())

		rns = corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: resourceNamespace,
			},
		}
		err = k8sClient.Create(context.Background(), &rns)
		Expect(err).ToNot(HaveOccurred())

		// Create the Work object with some type of Manifest resource.
		cm := &corev1.ConfigMap{
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
							RawExtension: runtime.RawExtension{Object: cm},
						},
					},
				},
			},
		}

		createWorkErr := k8sClient.Create(context.Background(), work)
		Expect(createWorkErr).ToNot(HaveOccurred())

		Eventually(func() bool {
			namespacedName := types.NamespacedName{Name: workName, Namespace: workNamespace}

			getAppliedWork := workv1alpha1.AppliedWork{}
			err := k8sClient.Get(context.Background(), namespacedName, &getAppliedWork)
			if err == nil {
				return getAppliedWork.Spec.WorkName == workName
			}
			return false
		}, timeout, interval).Should(BeTrue())
	})

	AfterEach(func() {
		// TODO: Ensure that all resources are being deleted.
		err := k8sClient.Delete(context.Background(), &wns)
		Expect(err).ToNot(HaveOccurred())

		err = k8sClient.Delete(context.Background(), &rns)
		Expect(err).ToNot(HaveOccurred())
	})

	Context("Receives a request where a Work's manifest condition does not contain the metadata of an existing AppliedResourceMeta", func() {
		It("Should delete the resource from the spoke cluster", func() {
			currentWork := waitForWorkToApply(workName, workNamespace)

			By("wait for the resource to propagate to the appliedWork")
			appliedWork := workv1alpha1.AppliedWork{}
			Eventually(func() bool {
				err := k8sClient.Get(context.Background(), types.NamespacedName{Name: workName}, &appliedWork)
				Expect(err).ToNot(HaveOccurred())
				return len(appliedWork.Status.AppliedResources) == 1
			}, timeout, interval).Should(BeTrue())

			By("Remove the resource from the works")
			currentWork.Spec.Workload.Manifests = nil
			Expect(k8sClient.Update(context.Background(), currentWork)).Should(Succeed())

			Eventually(func() bool {
				var configMap corev1.ConfigMap
				return apierrors.IsNotFound(k8sClient.Get(context.Background(), types.NamespacedName{Name: resourceName, Namespace: resourceNamespace}, &configMap))
			}, timeout, interval).Should(BeTrue())
		})
	})

	// TODO: rewrite this test.
	Context("Receives a request where a Work's manifest condition exists, but there isn't a respective AppliedResourceMeta.", func() {
		It("should delete the AppliedResourceMeta from the respective AppliedWork status", func() {

			appliedWork := workv1alpha1.AppliedWork{}
			Eventually(func() error {
				err := k8sClient.Get(context.Background(), types.NamespacedName{Name: workName, Namespace: workNamespace}, &appliedWork)
				Expect(err).ToNot(HaveOccurred())

				appliedWork.Status.AppliedResources = []workv1alpha1.AppliedResourceMeta{}
				err = k8sClient.Update(context.Background(), &appliedWork)

				return err
			}, timeout, interval).ShouldNot(HaveOccurred())

			Eventually(func() bool {
				namespacedName := types.NamespacedName{Name: workName, Namespace: workNamespace}
				err := k8sClient.Get(context.Background(), namespacedName, &appliedWork)
				if err != nil {
					return false
				}

				return len(appliedWork.Status.AppliedResources) > 0
			}, timeout, interval).Should(BeTrue())
		})
	})
})
