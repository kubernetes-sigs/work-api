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
	"flag"
	"os"

	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/work-api/pkg/apis/v1alpha1"
	"sigs.k8s.io/work-api/pkg/controllers"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	clientgoscheme.AddToScheme(scheme)
	v1alpha1.AddToScheme(scheme)
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var hubkubeconfig string
	flag.StringVar(&metricsAddr, "metrics-addr", ":8080", "The address the metric endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "enable-leader-election", false,
		"Enable leader election for controller manager. Enabling this will ensure there is only one active controller manager.")
	flag.StringVar(&hubkubeconfig, "hub-kubeconfig", "", "Paths to a kubeconfig connect to hub.")
	flag.Parse()
	opts := ctrl.Options{
		Scheme:             scheme,
		MetricsBindAddress: metricsAddr,
		LeaderElection:     enableLeaderElection,
		Port:               9443,
	}
	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))

	hubConfig, err := clientcmd.BuildConfigFromFlags("", hubkubeconfig)
	if err != nil {
		setupLog.Error(err, "error reading kubeconfig to connect to hub")
		os.Exit(1)
	}

	if err := controllers.Start(ctrl.SetupSignalHandler(), hubConfig, ctrl.GetConfigOrDie(), setupLog, opts); err != nil {
		setupLog.Error(err, "problem running controllers")
		os.Exit(1)
	}
}
