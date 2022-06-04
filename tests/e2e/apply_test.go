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

package e2e

import (
	"context"
	"fmt"
	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	workapi "sigs.k8s.io/work-api/pkg/apis/v1alpha1"
)

const (
	eventuallyTimeout  = 60 // seconds
	eventuallyInterval = 1  // seconds
)

var _ = ginkgo.Describe("Apply Work", func() {
	ginkgo.Context("Create a service and deployment", func() {
		ginkgo.It("Should create work successfully", func() {
			workNamespace = "default"

			manifestFiles := []string{
				"testmanifests/test-deployment.yaml",
				"testmanifests/test-service.yaml",
			}

			work := &workapi.Work{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-work",
					Namespace: workNamespace,
				},
				Spec: workapi.WorkSpec{
					Workload: workapi.WorkloadTemplate{
						Manifests: []workapi.Manifest{},
					},
				},
			}

			for _, file := range manifestFiles {
				fileRaw, err := testManifestFiles.ReadFile(file)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())

				obj, _, err := genericCodec.Decode(fileRaw, nil, nil)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())

				work.Spec.Workload.Manifests = append(
					work.Spec.Workload.Manifests, workapi.Manifest{
						RawExtension: runtime.RawExtension{Object: obj},
					})
			}

			_, err := hubWorkClient.MulticlusterV1alpha1().Works(workNamespace).Create(context.Background(), work, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			gomega.Eventually(func() error {
				_, err := spokeKubeClient.AppsV1().Deployments("default").Get(context.Background(), "test-nginx", metav1.GetOptions{})
				if err != nil {
					return err
				}

				_, err = spokeKubeClient.CoreV1().Services("default").Get(context.Background(), "test-nginx", metav1.GetOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			gomega.Eventually(func() error {
				work, err := hubWorkClient.MulticlusterV1alpha1().Works(workNamespace).Get(context.Background(), "test-work", metav1.GetOptions{})
				if err != nil {
					return err
				}

				if !meta.IsStatusConditionTrue(work.Status.Conditions, "Applied") {
					return fmt.Errorf("Expect the applied contidion of the work is true")
				}

				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
		})
	})
})
