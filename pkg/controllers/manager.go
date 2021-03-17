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
	"os"

	"github.com/go-logr/logr"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
)

const (
	workFinalizer      = "multicluster.x-k8s.io/work-cleanup"
	specHashAnnotation = "multicluster.x-k8s.io/spec-hash"
)

// Start the controllers with the supplied config
func Start(ctx context.Context, hubCfg, spokeCfg *rest.Config, setupLog logr.Logger, opts ctrl.Options) error {
	mgr, err := ctrl.NewManager(hubCfg, opts)
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	spokeDynamicClient, err := dynamic.NewForConfig(spokeCfg)
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	restMapper, err := apiutil.NewDynamicRESTMapper(spokeCfg, apiutil.WithLazyDiscovery)
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	if err = (&ApplyWorkReconciler{
		client:             mgr.GetClient(),
		spokeDynamicClient: spokeDynamicClient,
		restMapper:         restMapper,
		log:                ctrl.Log.WithName("controllers").WithName("WorkApply"),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "WorkApply")
		return err
	}

	if err = (&FinalizeWorkReconciler{
		client:             mgr.GetClient(),
		spokeDynamicClient: spokeDynamicClient,
		restMapper:         restMapper,
		log:                ctrl.Log.WithName("controllers").WithName("WorkFinalize"),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "WorkFinalize")
		return err
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctx); err != nil {
		setupLog.Error(err, "problem running manager")
		return err
	}
	return nil
}
