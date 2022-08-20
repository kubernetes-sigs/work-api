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

include makefiles/dependency.mk

DOCKER ?= docker

# work-api version
VERSION ?= master
# Repo info
GIT_COMMIT          ?= git-$(shell git rev-parse --short HEAD)
GIT_COMMIT_LONG     ?= $(shell git rev-parse HEAD)
VERSION_KEY    := sigs.k8s.io/work-api/version.VelaVersion
GITVERSION_KEY := sigs.k8s.io/work-api/version.GitRevision
LDFLAGS             ?= "-s -w -X $(VERSION_KEY)=$(VERSION) -X $(GITVERSION_KEY)=$(GIT_COMMIT)"

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

##@ General

# The help target prints out all targets with their descriptions organized
# beneath their categories. The categories are represented by '##@' and the
# target descriptions by '##'. The awk commands is responsible for reading the
# entire set of makefiles included in this invocation, looking for lines of the
# file as xyz: ## something, and then pretty-format the target and help. Then,
# if there's a line with ##@ something, that gets pretty-printed as a category.
# More info on the usage of ANSI control characters for terminal formatting:
# https://en.wikipedia.org/wiki/ANSI_escape_code#SGR_parameters
# More info on the awk command:
# http://linuxcommand.org/lc3_adv_awk.php

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

.PHONY: all
all: generate manifests controller verify ##

##@ Development

.PHONY: controller
controller: generate fmt vet ## Build controller binary
	go build -o bin/manager cmd/workcontroller/workcontroller.go

.PHONY: fmt
fmt: ## Run go fmt against code
	go fmt ./...

.PHONY: vet
vet: ## Run go vet against code
	go vet ./...

##@ Development

.PHONY: generate
generate: ## Run generators for Deepcopy funcs and CRDs
	./hack/update-codegen.sh
	$(CONTROLLER_GEN) object:headerFile=./hack/boilerplate.go.txt paths="./..."

.PHONY: manifests
manifests: ## Generate manifests e.g. CRD, RBAC etc.
	$(CONTROLLER_GEN) $(CRD_OPTIONS) rbac:roleName=work-manager webhook schemapatch:manifests="config/crd-base" paths="./pkg/apis/v1alpha1" output:crd:none output:crd:dir="config/crd"

reviewable: manifests generate fmt vet lint staticcheck ## Runs a series of source code checks.
	go mod tidy

##@ Verify

.PHONY: test
test: generate fmt vet manifests local-unit-test ## Run tests.

.PHONY: local-unit-test
local-unit-test: ensure-kubebuilder-tools
	go test ./pkg/... -coverprofile cover.out

.PHONY: verify
verify: ## Run static analysis.
	./hack/verify-all.sh

.PHONY: test-e2e
test-e2e: build-e2e e2e-hub-kubeconfig-secret deploy ## Run e2e tests.
	./e2e.test -test.v -ginkgo.v

build-e2e: ## Compiles test binary.
	go test -c ./tests/e2e

lint: golangci
	$(GOLANGCILINT) run ./...

staticcheck: staticchecktool
	$(STATICCHECK) ./...

##@ Build

.PHONY: docker-build
docker-build: generate fmt vet manifests ## Build docker containers.
	docker build . -t ${IMG}

.PHONY: docker-push
docker-push: docker-build ## Push the docker image.
	docker push ${IMG}

##@ Deployment

.PHONY: deploy
deploy: ## Deploy to cluster both CRDs and items in deploy folder.
	kubectl apply -f config/crd
	kubectl apply -k deploy

.PHONY: ensure-kubebuilder-tools
ensure-kubebuilder-tools: ## download the kubebuilder-tools to get kube-apiserver binaries from it.
ifeq "" "$(wildcard $(KUBEBUILDER_ASSETS))"
	$(info Downloading kube-apiserver into '$(KUBEBUILDER_ASSETS)')
	mkdir -p '$(KUBEBUILDER_ASSETS)'
	curl -s -f -L https://storage.googleapis.com/kubebuilder-tools/$(KB_TOOLS_ARCHIVE_NAME) -o '$(KB_TOOLS_ARCHIVE_PATH)'
	tar -C '$(KUBEBUILDER_ASSETS)' --strip-components=2 -zvxf '$(KB_TOOLS_ARCHIVE_PATH)'
else
	$(info Using existing kube-apiserver from "$(KUBEBUILDER_ASSETS)")
endif


## Helper commands
cluster-ip:
	kubectl config use-context $(HUB_KUBECONFIG_CONTEXT) --kubeconfig $(HUB_KUBECONFIG)
  	CLUSTER_IP?=$(shell kubectl --kubeconfig $(HUB_KUBECONFIG) get svc kubernetes -n default -o jsonpath="{.spec.clusterIP}")

e2e-hub-kubeconfig-secret: cluster-ip
	cp $(HUB_KUBECONFIG) e2e-hub-kubeconfig
	kubectl apply -f deploy/component_namespace.yaml --kubeconfig $(SPOKE_KUBECONFIG)
	kubectl config set clusters.$(HUB_KUBECONFIG_CONTEXT).server https://$(CLUSTER_IP) --kubeconfig e2e-hub-kubeconfig
	kubectl delete secret hub-kubeconfig-secret -n fleet-system --ignore-not-found --kubeconfig $(SPOKE_KUBECONFIG)
	kubectl create secret generic hub-kubeconfig-secret --from-file=kubeconfig=e2e-hub-kubeconfig -n fleet-system --kubeconfig $(SPOKE_KUBECONFIG)
	rm ./e2e-hub-kubeconfig