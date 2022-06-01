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
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"

	clientset "sigs.k8s.io/work-api/pkg/client/clientset/versioned"
)

const (
	workFinalizer        = "multicluster.x-k8s.io/work-cleanup"
	appliedWorkFinalizer = "multicluster.x-k8s.io/appliedResource-cleanup"
	specHashAnnotation   = "multicluster.x-k8s.io/spec-hash"

	ConditionTypeApplied = "Applied"
)

// Start the controllers with the supplied config
func Start(ctx context.Context, hubCfg, spokeCfg *rest.Config, setupLog logr.Logger, opts ctrl.Options) error {
	hubMgr, err := ctrl.NewManager(hubCfg, opts)
	if err != nil {
		setupLog.Error(err, "unable to start hub manager")
		os.Exit(1)
	}

	spokeOpts := ctrl.Options{
		Scheme:             opts.Scheme,
		LeaderElection:     opts.LeaderElection,
		MetricsBindAddress: ":4848",
		Port:               8443,
	}
	spokeMgr, err := ctrl.NewManager(spokeCfg, spokeOpts)
	if err != nil {
		setupLog.Error(err, "unable to start member manager")
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

	spokeClientset, err := clientset.NewForConfig(spokeCfg)
	if err != nil {
		klog.Fatalf("Error building example clientset: %s", err.Error())
	}
	//
	//hubClientset, err := clientset.NewForConfig(hubCfg)
	//	if err != nil {
	//		klog.Fatalf("Error building kubernetes clientset: %s", err.Error())
	//	}
	// hubInformerFactory := workinformers.NewSharedInformerFactory(hubClientset, time.Second*3)
	// spokeInformerFactory := workinformers.NewSharedInformerFactory(spokeClientset, time.Second*3)

	// TODO: Add event recorder
	if err = newAppliedWorkReconciler(opts.Namespace, hubMgr.GetClient(), spokeMgr.GetClient(), spokeDynamicClient, restMapper).SetupWithManager(spokeMgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "AppliedWork")
		return err
	}

	if err = newWorkStatusReconciler(hubMgr.GetClient(), spokeMgr.GetClient(), spokeDynamicClient, restMapper).SetupWithManager(hubMgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "WorkStatus")
		return err
	}

	if err = (&ApplyWorkReconciler{
		client:             hubMgr.GetClient(),
		spokeDynamicClient: spokeDynamicClient,
		spokeClient:        spokeMgr.GetClient(),
		restMapper:         restMapper,
		log:                ctrl.Log.WithName("Work reconciler"),
	}).SetupWithManager(hubMgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Work")
		return err
	}

	if err = (&FinalizeWorkReconciler{
		client:      hubMgr.GetClient(),
		spokeClient: spokeClientset,
		restMapper:  restMapper,
		log:         ctrl.Log.WithName("WorkFinalize reconcier"),
	}).SetupWithManager(hubMgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "WorkFinalize")
		return err
	}

	go func() error {
		klog.Info("starting hub manager")
		defer klog.Info("shutting down hub manager")
		if err := hubMgr.Start(ctx); err != nil {
			setupLog.Error(err, "problem running hub  manager")
			return err
		}
		return nil
	}()

	if err := spokeMgr.Start(ctx); err != nil {
		setupLog.Error(err, "problem running spoke manager")
		return err
	}
	return nil
}
