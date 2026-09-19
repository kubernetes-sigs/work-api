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
	"strings"
	"testing"

	"github.com/go-logr/logr/testr"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

func TestStartReturnsSpokeClientError(t *testing.T) {
	hubCfg := &rest.Config{Host: "http://127.0.0.1:1"}
	spokeCfg := &rest.Config{Host: "://bad host\x7f"}
	opts := ctrl.Options{Metrics: metricsserver.Options{BindAddress: "0"}}

	err := Start(context.Background(), hubCfg, spokeCfg, testr.New(t), opts)

	if err == nil {
		t.Fatal("expected Start to return an error for an invalid spoke config, got nil")
	}
	if !strings.Contains(err.Error(), "invalid control character") {
		t.Fatalf("expected the spoke dynamic client error to surface, got: %v", err)
	}
}
