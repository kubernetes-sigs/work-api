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
# TOP is the current directory where this Makefile lives.
TOP := $(dir $(firstword $(MAKEFILE_LIST)))
# ROOT is the root of the mkdocs tree.
ROOT := $(abspath $(TOP))
# Image URL to use all building/pushing image targets
IMG ?= work-api-controller:latest
# Need v1 to support defaults in CRDs, unfortunately limiting us to k8s 1.16+
CRD_OPTIONS ?= "crd:crdVersions=v1"

CONTROLLER_GEN=go run sigs.k8s.io/controller-tools/cmd/controller-gen
# enable Go modules
export GO111MODULE=on

.PHONY: all
all: generate manifests controller verify

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
	$(CONTROLLER_GEN) object:headerFile=./hack/boilerplate.go.txt paths="./..."

# Generate manifests e.g. CRD, RBAC etc.
.PHONY: manifests
manifests:
	$(CONTROLLER_GEN) $(CRD_OPTIONS) rbac:roleName=work-manager webhook schemapatch:manifests="config/crd-base" paths="./..." output:crd:none output:schemapatch:dir="config/crd"

# Run tests
.PHONY: test
test: generate fmt vet manifests
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
