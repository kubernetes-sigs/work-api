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
	"embed"
	"k8s.io/apimachinery/pkg/api/meta"
	"os"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"testing"

	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextension "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	workclientset "sigs.k8s.io/work-api/pkg/client/clientset/versioned"
)

var (
	restConfig *rest.Config
	restMapper meta.RESTMapper

	hubWorkClient workclientset.Interface

	spokeApiExtensionClient *apiextension.Clientset
	spokeDynamicClient      dynamic.Interface
	spokeKubeClient         kubernetes.Interface
	spokeWorkClient         workclientset.Interface

	//go:embed manifests
	testManifestFiles embed.FS

	genericScheme = runtime.NewScheme()
	genericCodecs = serializer.NewCodecFactory(genericScheme)
	genericCodec  = genericCodecs.UniversalDeserializer()
)

func init() {
	utilruntime.Must(scheme.AddToScheme(genericScheme))
	utilruntime.Must(apiextensionsv1.AddToScheme(genericScheme))
}

func TestE2e(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "E2e Suite")
}

// This suite is sensitive to the following environment variables:
//
// - IMAGE_NAME sets the exact image to deploy for the work agent
// - IMAGE_REGISTRY sets the image registry to use to build the IMAGE_NAME if
//   IMAGE_NAME is unset: IMAGE_REGISTRY/work:latest
// - KUBECONFIG is the location of the kubeconfig file to use
var _ = ginkgo.BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(ginkgo.GinkgoWriter), zap.UseDevMode(true)))

	var err error
	kubeconfig := os.Getenv("KUBECONFIG")
	restConfig, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	hubWorkClient, err = workclientset.NewForConfig(restConfig)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	spokeApiExtensionClient, err = apiextension.NewForConfig(restConfig)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	spokeDynamicClient, err = dynamic.NewForConfig(restConfig)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	spokeKubeClient, err = kubernetes.NewForConfig(restConfig)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	spokeWorkClient, err = workclientset.NewForConfig(restConfig)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	restMapper, err = apiutil.NewDynamicRESTMapper(restConfig, apiutil.WithLazyDiscovery)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
})
