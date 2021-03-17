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
	"fmt"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	workv1alpha1 "sigs.k8s.io/work-api/pkg/apis/v1alpha1"
)

var _ = Describe("Work Controller", func() {
	const workNamespace = "cluster"
	const timeout = time.Second * 30
	const interval = time.Second * 1

	BeforeEach(func() {
		// Create namespace
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: workNamespace,
			},
		}
		_, err := k8sClient.CoreV1().Namespaces().Create(context.Background(), ns, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		// Add any teardown steps that needs to be executed after each test
		err := k8sClient.CoreV1().Namespaces().Delete(context.Background(), workNamespace, metav1.DeleteOptions{})
		Expect(err).ToNot(HaveOccurred())
	})
	Context("Deploy a work", func() {
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
					Name:      "comfigmap-work",
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

			_, err := workClient.MulticlusterV1alpha1().Works(workNamespace).Create(context.Background(), work, metav1.CreateOptions{})
			Expect(err).ToNot(HaveOccurred())

			Eventually(func() error {
				_, err := k8sClient.CoreV1().ConfigMaps(cmNamespace).Get(context.Background(), cmName, metav1.GetOptions{})
				return err
			}, timeout, interval).Should(Succeed())

			Eventually(func() error {
				resultWork, err := workClient.MulticlusterV1alpha1().Works(workNamespace).Get(context.Background(), work.Name, metav1.GetOptions{})
				if err != nil {
					return err
				}
				if len(resultWork.Status.ManifestConditions) != 1 {
					return fmt.Errorf("Expect the 1 manifest condition is updated")
				}

				if !meta.IsStatusConditionTrue(resultWork.Status.ManifestConditions[0].Conditions, "Applied") {
					return fmt.Errorf("Exepect condition status of the manifest to be true")
				}

				if !meta.IsStatusConditionTrue(resultWork.Status.Conditions, "Applied") {
					return fmt.Errorf("Exepect condition status of the work to be true")
				}

				return nil
			}, timeout, interval).Should(Succeed())
		})
	})
})
