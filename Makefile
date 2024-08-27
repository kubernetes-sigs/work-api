# Copyright 2019 The Kubernetes Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

DOCKER ?= docker

GOHOSTOS ?=$(shell go env GOHOSTOS)
GOHOSTARCH ?=$(shell go env GOHOSTARCH)
K8S_VERSION ?=1.19.2
KB_TOOLS_ARCHIVE_NAME :=kubebuilder-tools-$(K8S_VERSION)-$(GOHOSTOS)-$(GOHOSTARCH).tar.gz
KB_TOOLS_ARCHIVE_PATH := /tmp/$(KB_TOOLS_ARCHIVE_NAME)
export KUBEBUILDER_ASSETS ?=/tmp/kubebuilder/bin

HUB_KUBECONFIG ?=$(KUBECONFIG)
HUB_KUBECONFIG_CONTEXT ?=$(shell kubectl --kubeconfig $(HUB_KUBECONFIG) config current-context)
SPOKE_KUBECONFIG ?=$(KUBECONFIG)
SPOKE_KUBECONFIG_CONTEXT ?=$(shell kubectl --kubeconfig $(SPOKE_KUBECONFIG) config current-context)

# TOP is the current directory where this Makefile lives.
TOP := $(dir $(firstword $(MAKEFILE_LIST)))
# ROOT is the root of the mkdocs tree.
ROOT := $(abspath $(TOP))
# Image URL to use all building/pushing image targets
IMG ?= work-api-controller:latest
# Need v1 to support defaults in CRDs, unfortunately limiting us to k8s 1.16+
CRD_OPTIONS ?= "crd:crdVersions=v1"

CONTROLLER_GEN=go run sigs.k8s.io/controller-tools/cmd/controller-gen

.PHONY: all
all: generate manifests controller verify

# Build controller binary
.PHONY: controller
controller: generate fmt vet
	go build -o bin/manager cmd/workcontroller/workcontroller.go

# Run go fmt against code
.PHONY: fmt
fmt:
	go fmt ./...

# Run go vet against code
.PHONY: vet
vet:
	go vet ./...

# Run generators for Deepcopy funcs and CRDs
.PHONY: generate
generate:
	./hack/update-codegen.sh

# Generate manifests e.g. CRD, RBAC etc.
.PHONY: manifests
manifests:
	$(CONTROLLER_GEN) $(CRD_OPTIONS) rbac:roleName=work-manager webhook schemapatch:manifests="config/crd-base" paths="./pkg/apis/v1alpha1" output:crd:none output:schemapatch:dir="config/crd"

# Run tests
.PHONY: test
test: generate fmt vet manifests ensure-kubebuilder-tools
	go test ./pkg/... -coverprofile cover.out

# Run static analysis.
.PHONY: verify
verify:
	./hack/verify-all.sh

# Build docker containers
.PHONY: docker-build
docker-build: generate fmt vet manifests
	docker build . -t ${IMG}

# Push the docker image
.PHONY: docker-push
docker-push: docker-build
	docker push ${IMG}

.PHONY: deploy
deploy:
	kubectl apply -f config/crd
	kubectl apply -k deploy

cluster-ip:
	kubectl config use-context $(HUB_KUBECONFIG_CONTEXT) --kubeconfig $(HUB_KUBECONFIG)
  	CLUSTER_IP?=$(shell kubectl --kubeconfig $(HUB_KUBECONFIG) get svc kubernetes -n default -o jsonpath="{.spec.clusterIP}")

e2e-hub-kubeconfig-secret: cluster-ip
	cp $(HUB_KUBECONFIG) e2e-hub-kubeconfig
	kubectl apply -f deploy/component_namespace.yaml --kubeconfig $(SPOKE_KUBECONFIG)
	kubectl config set clusters.$(HUB_KUBECONFIG_CONTEXT).server https://$(CLUSTER_IP) --kubeconfig e2e-hub-kubeconfig
	kubectl delete secret hub-kubeconfig-secret -n work --ignore-not-found --kubeconfig $(SPOKE_KUBECONFIG)
	kubectl create secret generic hub-kubeconfig-secret --from-file=kubeconfig=e2e-hub-kubeconfig -n work --kubeconfig $(SPOKE_KUBECONFIG)
	rm ./e2e-hub-kubeconfig

build-e2e:
	go test -c ./tests/e2e

.PHONY: test-e2e
test-e2e: build-e2e e2e-hub-kubeconfig-secret deploy
	./e2e.test -test.v -ginkgo.v

# download the kubebuilder-tools to get kube-apiserver binaries from it
.PHONY: ensure-kubebuilder-tools
ensure-kubebuilder-tools:
ifeq "" "$(wildcard $(KUBEBUILDER_ASSETS))"
	$(info Downloading kube-apiserver into '$(KUBEBUILDER_ASSETS)')
	mkdir -p '$(KUBEBUILDER_ASSETS)'
	curl -s -f -L https://storage.googleapis.com/kubebuilder-tools/$(KB_TOOLS_ARCHIVE_NAME) -o '$(KB_TOOLS_ARCHIVE_PATH)'
	tar -C '$(KUBEBUILDER_ASSETS)' --strip-components=2 -zvxf '$(KB_TOOLS_ARCHIVE_PATH)'
else
	$(info Using existing kube-apiserver from "$(KUBEBUILDER_ASSETS)")
endif
