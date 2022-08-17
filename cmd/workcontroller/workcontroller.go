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

package main

import (
	"context"
	"flag"
	"fmt"

	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/work-api/pkg/apis/v1alpha1"
	"sigs.k8s.io/work-api/pkg/controllers"
	"sigs.k8s.io/work-api/version"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(v1alpha1.AddToScheme(scheme))
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var hubkubeconfig string
	var hubsecret string
	var workNamespace string
	var certDir string
	var webhookPort int
	var healthAddr string
	var concurrentReconciles int

	klog.InitFlags(nil)
	flag.StringVar(&metricsAddr, "metrics-addr", ":8080", "The address the metric endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "enable-leader-election", false,
		"Enable leader election for controller manager. Enabling this will ensure there is only one active controller manager.")
	flag.IntVar(&webhookPort, "webhook-port", 9443, "admission webhook listen address")
	flag.StringVar(&certDir, "webhook-cert-dir", "/k8s-webhook-server/serving-certs", "Admission webhook cert/key dir.")
	flag.StringVar(&healthAddr, "health-addr", ":9440", "The address the health endpoint binds to.")
	flag.StringVar(&hubkubeconfig, "hub-kubeconfig", "", "Paths to a kubeconfig connect to hub.")
	flag.StringVar(&hubsecret, "hub-kubeconfig-secret", "", "the name of the secret that contains the hub kubeconfig")
	flag.StringVar(&workNamespace, "work-namespace", "", "Namespace to watch for work.")
	flag.IntVar(&concurrentReconciles, "concurrency", 5, "max work reconciler concurrency")

	flag.Parse()
	defer klog.Flush()
	flag.VisitAll(func(f *flag.Flag) {
		klog.InfoS("flag:", "name", f.Name, "value", f.Value)
	})

	opts := ctrl.Options{
		Scheme:                 scheme,
		MetricsBindAddress:     metricsAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "azure-fleet-work-api-" + version.GitRevision,
		Port:                   webhookPort,
		CertDir:                certDir,
		HealthProbeBindAddress: healthAddr,
		Namespace:              workNamespace,
	}

	var hubConfig *restclient.Config
	var err error

	if len(hubkubeconfig) != 0 {
		setupLog.Info("read kubeconfig from file")
		hubConfig, err = clientcmd.BuildConfigFromFlags("", hubkubeconfig)
	} else {
		setupLog.Info("read kubeconfig from secret")
		hubConfig, err = getKubeConfig(hubsecret)
	}
	if err != nil {
		setupLog.Error(err, "error reading kubeconfig to connect to hub")
		klog.FlushAndExit(klog.ExitFlushTimeout, 1)
	}

	if err := controllers.Start(ctrl.SetupSignalHandler(), hubConfig, ctrl.GetConfigOrDie(), setupLog, opts); err != nil {
		setupLog.Error(err, "problem running controllers")
		klog.FlushAndExit(klog.ExitFlushTimeout, 1)
	}
}

func getKubeConfig(hubkubeconfig string) (*restclient.Config, error) {
	spokeClientSet, err := kubernetes.NewForConfig(ctrl.GetConfigOrDie())
	if err != nil {
		return nil, errors.Wrap(err, "cannot create the spoke client")
	}

	secret, err := spokeClientSet.CoreV1().Secrets("fleet-system").Get(context.Background(), hubkubeconfig, metav1.GetOptions{})
	if err != nil {
		return nil, errors.Wrap(err, "cannot find kubeconfig secrete")
	}

	kubeConfigData, ok := secret.Data["kubeconfig"]
	if !ok || len(kubeConfigData) == 0 {
		return nil, fmt.Errorf("wrong formatted kube config")
	}

	kubeConfig, err := clientcmd.RESTConfigFromKubeConfig(kubeConfigData)
	if err != nil {
		return nil, errors.Wrap(err, "cannot create the rest client")
	}

	return kubeConfig, nil
}
