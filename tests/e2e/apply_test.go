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
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/json"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	workapi "sigs.k8s.io/work-api/pkg/apis/v1alpha1"
)

const (
	eventuallyTimeout  = 60 // seconds
	eventuallyInterval = 1  // seconds
)

var (
	appliedWork            *workapi.AppliedWork
	createdWork            *workapi.Work
	createError            error
	deleteError            error
	getError               error
	updateError            error
	manifests              []string
	manifestMetaName       []string
	manifestMetaNamespaces []string
	workName               string
	workNamespace          string

	_ = Describe("Work", func() {
		manifestMetaName = []string{"test-nginx", "test-nginx", "test-configmap"} //Todo - Unmarshal raw file bytes into JSON, extract key / values programmatically.
		manifestMetaNamespaces = []string{"default", "default", "default"}        //Todo - Unmarshal raw file bytes into JSON, extract key / values programmatically.

		// The Manifests' ordinal must be respected; some tests reference by ordinal.
		manifests = []string{
			"testmanifests/test-deployment.yaml",
			"testmanifests/test-service.yaml",
			"testmanifests/test-configmap.yaml",
		}

		BeforeEach(func() {
			workName = "work-" + utilrand.String(5)
			workNamespace = "default"

			createdWork, createError = createWork(workName, workNamespace, manifests)
			Expect(createError).ToNot(HaveOccurred())
		})

		AfterEach(func() {
			deleteError = safeDeleteWork(createdWork)
		})

		Describe("created on the Hub", func() {
			// Todo - It would be better to have context of N manifest, and let the type be programmatically determined.
			Context("with a Service & Deployment & Configmap manifest", func() {
				It("should have a Work resource in the hub", func() {
					Eventually(func() error {
						_, err := retrieveWork(createdWork.Namespace, createdWork.Name)

						return err
					}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())
				})
				It("should have an AppliedWork resource in the spoke ", func() {
					Eventually(func() error {
						_, err := spokeWorkClient.MulticlusterV1alpha1().AppliedWorks().Get(context.Background(), createdWork.Name, metav1.GetOptions{})

						return err
					}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())
				})
				It("should have created a Kubernetes deployment", func() {
					Eventually(func() error {
						_, err := spokeKubeClient.AppsV1().Deployments(manifestMetaNamespaces[0]).Get(context.Background(), manifestMetaName[0], metav1.GetOptions{})

						return err
					}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())
				})
				It("should have created a Kubernetes service", func() {
					Eventually(func() error {
						_, err := spokeKubeClient.CoreV1().Services(manifestMetaNamespaces[1]).Get(context.Background(), manifestMetaName[1], metav1.GetOptions{})

						return err
					}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())
				})
				It("should have created a ConfigMap", func() {
					Eventually(func() error {
						_, err := spokeKubeClient.CoreV1().ConfigMaps(manifestMetaNamespaces[2]).Get(context.Background(), manifestMetaName[2], metav1.GetOptions{})

						return err
					}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())
				})
			})
		})

		Describe("updated on the Hub", func() {
			Context("with a new manifest", func() {
				// Create then namespace for which the new manifest will be created within.
				// Todo - utilize Work to ensure is applied.
				namespace := &v1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-namespace",
					},
				}
				manifests = append(manifests, "testmanifests/test-configmap2.yaml")
				manifestMetaName = append(manifestMetaName, "test-configmap")
				manifestMetaNamespaces = append(manifestMetaNamespaces, namespace.Name)

				BeforeEach(func() {
					var work *workapi.Work
					var err error

					By("ensuring the namespace specified within the manifest already exists on the spoke")
					Eventually(func() error {
						_, err = spokeKubeClient.CoreV1().Namespaces().Create(context.Background(), namespace, metav1.CreateOptions{})
						return err
					}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())

					By("getting the existing Work resource on the hub")
					Eventually(func() error {
						work, err = retrieveWork(createdWork.Namespace, createdWork.Name)
						return err
					}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())

					By("adding the new manifest to the Work resource and updating it on the hub")
					Eventually(func() error {
						addManifestsToWorkSpec([]string{manifests[2]}, &work.Spec)
						_, err = updateWork(work)
						return err
					}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())
				})
				AfterEach(func() {
					Eventually(func() error {
						return spokeKubeClient.CoreV1().Namespaces().Delete(context.Background(), namespace.Name, metav1.DeleteOptions{})
					}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())
				})

				It("should have applied the added manifest", func() {
					Eventually(func() error {
						_, err := spokeKubeClient.CoreV1().ConfigMaps(manifestMetaNamespaces[3]).Get(context.Background(), manifestMetaName[3], metav1.GetOptions{})

						return err
					}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())
				})
			})
			Context("with a modified manifest", func() {
				// Todo, refactor this context to not use "over complicated structure".
				var cm v1.ConfigMap
				var newDataKey string
				var newDataValue string
				var configMapName string
				var configMapNamespace string

				BeforeEach(func() {
					configManifest := createdWork.Spec.Workload.Manifests[2]
					// Unmarshal the data into a struct, modify and then update it.
					err := json.Unmarshal(configManifest.Raw, &cm)
					Expect(err).ToNot(HaveOccurred())
					configMapName = cm.Name
					configMapNamespace = cm.Namespace

					// Add random new key value pair into map.
					newDataKey = utilrand.String(5)
					newDataValue = utilrand.String(5)
					cm.Data[newDataKey] = newDataValue
					rawManifest, err := json.Marshal(cm)
					Expect(err).ToNot(HaveOccurred())
					updatedManifest := workapi.Manifest{}
					updatedManifest.Raw = rawManifest
					createdWork.Spec.Workload.Manifests[2] = updatedManifest

					By("updating a manifest specification within the existing Work on the hub")
					createdWork, updateError = updateWork(createdWork)
					Expect(updateError).ToNot(HaveOccurred())
				})

				It("should reapply the manifest.", func() {
					Eventually(func() bool {
						configMap, err := spokeKubeClient.CoreV1().ConfigMaps(configMapNamespace).Get(context.Background(), configMapName, metav1.GetOptions{})
						Expect(err).ToNot(HaveOccurred())

						return configMap.Data[newDataKey] == newDataValue
					}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
				})
			})
		})

		Describe("deleted from the Hub", func() {
			BeforeEach(func() {
				// Todo - Replace with Eventually.
				time.Sleep(2 * time.Second) // Give time for AppliedWork to be created.
				// Grab the AppliedWork, so resource garbage collection can be verified.
				appliedWork, getError = retrieveAppliedWork(createdWork.Name)
				Expect(getError).ToNot(HaveOccurred())

				deleteError = safeDeleteWork(createdWork)
				Expect(deleteError).ToNot(HaveOccurred())
			})

			It("should have deleted the Work resource on the hub", func() {
				Eventually(func() error {
					_, err := retrieveWork(workNamespace, workName)

					return err
				}, eventuallyTimeout, eventuallyInterval).Should(HaveOccurred())
			})
			It("should have deleted the resources from the spoke", func() {
				Eventually(func() bool {
					garbageCollectionComplete := true
					for _, resourceMeta := range appliedWork.Status.AppliedResources {
						gvr := schema.GroupVersionResource{
							Group:    resourceMeta.Group,
							Version:  resourceMeta.Version,
							Resource: resourceMeta.Resource,
						}
						_, err := spokeDynamicClient.Resource(gvr).Get(context.Background(), resourceMeta.Name, metav1.GetOptions{})

						if err == nil {
							garbageCollectionComplete = false
							break
						}
					}

					return garbageCollectionComplete
				}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
			})
			It("should have deleted the AppliedWork resource from the spoke", func() {
				Eventually(func() error {
					_, err := retrieveAppliedWork(createdWork.Name)

					return err
				}, eventuallyTimeout, eventuallyInterval).Should(HaveOccurred())
			})
		})

	})
)

func addManifestsToWorkSpec(manifestFileRelativePaths []string, workSpec *workapi.WorkSpec) {
	for _, file := range manifestFileRelativePaths {
		fileRaw, err := testManifestFiles.ReadFile(file)
		Expect(err).ToNot(HaveOccurred())

		obj, _, err := genericCodec.Decode(fileRaw, nil, nil)
		Expect(err).ToNot(HaveOccurred())

		workSpec.Workload.Manifests = append(
			workSpec.Workload.Manifests, workapi.Manifest{
				RawExtension: runtime.RawExtension{Object: obj},
			})
	}
}
func createWork(workName string, workNamespace string, manifestFiles []string) (*workapi.Work, error) {
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
	createdWork, createError = hubWorkClient.MulticlusterV1alpha1().Works(work.Namespace).Create(context.Background(), work, metav1.CreateOptions{})

	return createdWork, createError
}
func retrieveAppliedWork(resourceName string) (*workapi.AppliedWork, error) {
	return spokeWorkClient.MulticlusterV1alpha1().AppliedWorks().Get(context.Background(), resourceName, metav1.GetOptions{})
}
func retrieveWork(workNamespace string, workName string) (*workapi.Work, error) {
	return hubWorkClient.MulticlusterV1alpha1().Works(workNamespace).Get(context.Background(), workName, metav1.GetOptions{})
}
func safeDeleteWork(work *workapi.Work) error {
	// ToDo - Replace with proper Eventually.
	time.Sleep(1 * time.Second)
	_, getError = hubWorkClient.MulticlusterV1alpha1().Works(work.Namespace).Get(context.Background(), work.Name, metav1.GetOptions{})
	if getError == nil {
		deleteError = hubWorkClient.MulticlusterV1alpha1().Works(createdWork.Namespace).Delete(context.Background(), createdWork.Name, metav1.DeleteOptions{})
	}

	return getError
}
func updateWork(work *workapi.Work) (*workapi.Work, error) {
	return hubWorkClient.MulticlusterV1alpha1().Works(work.Namespace).Update(context.Background(), work, metav1.UpdateOptions{})
}
