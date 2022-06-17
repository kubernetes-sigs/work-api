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
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	workapi "sigs.k8s.io/work-api/pkg/apis/v1alpha1"
)

const (
	eventuallyTimeout  = 60 // seconds
	eventuallyInterval = 1  // seconds
)

var _ = ginkgo.Describe("Apply Work", func() {

	workName := "work-" + utilrand.String(5)
	workNamespace = "default"

	ginkgo.Context("Create a service and deployment", func() {
		ginkgo.It("Should create work successfully", func() {

			manifestFiles := []string{
				"testmanifests/test-deployment.yaml",
				"testmanifests/test-service.yaml",
			}

			work := &workapi.Work{
				ObjectMeta: metav1.ObjectMeta{
					Name:      workName,
					Namespace: workNamespace,
				},
				Spec: workapi.WorkSpec{
					Workload: workapi.WorkloadTemplate{
						Manifests: []workapi.Manifest{},
					},
				},
			}

			addManifestsToWorkSpec(manifestFiles, &work.Spec)

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
				work, err := hubWorkClient.MulticlusterV1alpha1().Works(workNamespace).Get(context.Background(), workName, metav1.GetOptions{})
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
	ginkgo.Context("Add a manifest to an existing work", func() {
		ginkgo.It("Should create new resources successfully.", func() {
			manifestFiles := []string{
				"testmanifests/test-deployment2.yaml",
				"testmanifests/test-service2.yaml",
			}

			// Get existing work, then add the new manifests
			work, err := hubWorkClient.MulticlusterV1alpha1().Works(workNamespace).Get(context.Background(), workName, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(work).ToNot(gomega.BeNil())

			addManifestsToWorkSpec(manifestFiles, &work.Spec)

			_, err = hubWorkClient.MulticlusterV1alpha1().Works(workNamespace).Update(context.Background(), work, metav1.UpdateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			gomega.Eventually(func() error {
				_, err := spokeKubeClient.AppsV1().Deployments("default").Get(context.Background(), "test-nginx2", metav1.GetOptions{})
				if err != nil {
					return err
				}

				_, err = spokeKubeClient.CoreV1().Services("default").Get(context.Background(), "test-nginx2", metav1.GetOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			gomega.Eventually(func() error {
				work, err := hubWorkClient.MulticlusterV1alpha1().Works(workNamespace).Get(context.Background(), workName, metav1.GetOptions{})
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

func addManifestsToWorkSpec(manifestFileRelativePaths []string, workSpec *workapi.WorkSpec) {
	for _, file := range manifestFileRelativePaths {
		fileRaw, err := testManifestFiles.ReadFile(file)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		obj, _, err := genericCodec.Decode(fileRaw, nil, nil)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		workSpec.Workload.Manifests = append(
			workSpec.Workload.Manifests, workapi.Manifest{
				RawExtension: runtime.RawExtension{Object: obj},
			})
	}
}
