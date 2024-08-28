#!/usr/bin/env bash

# Copyright 2020 The Kubernetes Authors.
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

set -o errexit
set -o nounset
set -o pipefail

SCRIPT_ROOT=$(dirname "${BASH_SOURCE}")/..

go install k8s.io/code-generator/cmd/{client-gen,lister-gen,informer-gen,deepcopy-gen,register-gen}

# Go installs the above commands to get installed in $GOBIN if defined, and $GOPATH/bin otherwise:
GOBIN="$(go env GOBIN)"
gobin="${GOBIN:-$(go env GOPATH)/bin}"

OUTPUT_PKG=sigs.k8s.io/work-api/pkg/client
FQ_APIS=sigs.k8s.io/work-api/pkg/apis/v1alpha1
CLIENTSET_PKG_NAME=clientset

# code generator tools rely on the environment variable GOPATH and generates files to $GOPATH/src/.
export GOPATH=$(go env GOPATH | awk -F ':' '{print $1}')

echo "Generating deepcopy funcs"
"${gobin}/deepcopy-gen" \
  --go-header-file ${SCRIPT_ROOT}/hack/boilerplate.go.txt \
  --output-file=zz_generated.deepcopy.go \
  sigs.k8s.io/work-api/pkg/apis/v1alpha1

echo "Generating register at ${FQ_APIS}"
"${gobin}/register-gen" \
  --go-header-file ${SCRIPT_ROOT}/hack/boilerplate.go.txt \
  --output-file=zz_generated.register.go \
  sigs.k8s.io/work-api/pkg/apis/v1alpha1

echo "Generating clientset at ${OUTPUT_PKG}/${CLIENTSET_PKG_NAME}"
"${gobin}/client-gen" \
  --go-header-file ${SCRIPT_ROOT}/hack/boilerplate.go.txt \
  --input-base="" \
  --input=sigs.k8s.io/work-api/pkg/apis/v1alpha1 \
  --output-pkg=sigs.k8s.io/work-api/pkg/client/clientset \
  --output-dir=pkg/client/clientset \
  --clientset-name=versioned

echo "Generating listers at ${OUTPUT_PKG}/listers"
"${gobin}/lister-gen" \
  --go-header-file ${SCRIPT_ROOT}/hack/boilerplate.go.txt \
  --output-pkg=sigs.k8s.io/work-api/pkg/client/listers \
  --output-dir=pkg/client/listers \
  sigs.k8s.io/work-api/pkg/apis/v1alpha1

echo "Generating informers at ${OUTPUT_PKG}/informers"
"${gobin}/informer-gen" \
  --go-header-file ${SCRIPT_ROOT}/hack/boilerplate.go.txt \
  --versioned-clientset-package=sigs.k8s.io/work-api/pkg/client/clientset/versioned \
  --listers-package=sigs.k8s.io/work-api/pkg/client/listers \
  --output-pkg=sigs.k8s.io/work-api/pkg/client/informers \
  --output-dir=pkg/client/informers \
  sigs.k8s.io/work-api/pkg/apis/v1alpha1
