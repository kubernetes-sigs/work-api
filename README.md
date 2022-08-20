# Work API

This repo will hold design documents and implementation of the [Work API](https://docs.google.com/document/d/1cWcdB40pGg3KS1eSyb9Q6SIRvWVI8dEjFp9RI0Gk0vg/edit#).

![GitHub go.mod Go version][1] ![Build Status][2] [![codecov][3]][4]
![issues][5] ![Activity][6]

![GitHub release (latest by date)][7]

## Community, discussion, contribution, and support

Learn how to engage with the Kubernetes community on the [community page](http://kubernetes.io/community/).

You can reach the maintainers of this project at:

- [Slack](https://kubernetes.slack.com/messages/sig-multicluster)
- [Mailing List](https://groups.google.com/forum/#!forum/kubernetes-sig-multicluster)
- [SIG Multicluster](https://github.com/kubernetes/community/blob/master/sig-multicluster/README.md)

## Quick Start

This guide will cover:
- Create a `kind` cluster that acts as the `hub` work delivery control plane.
- Create a `kind` cluster that acts as the `spoke` cluster for the work to be delivery to.
- Install the `work` CRD to the `hub` cluster.
- Install the `appliedwork` CRD to the `spoke` cluster.
- Install the `work` agent components to the `spoke` cluster.
- Deploy a `work` example on the `hub` cluster.
- Verify all the contents inside the `work` has been delivered in the `spoke` cluster.

### Prerequisites
- [Go](https://golang.org) version v1.17+
- [kubectl](https://kubernetes.io/docs/tasks/tools/install-kubectl) version v1.19+
- [kind](https://kind.sigs.k8s.io) version v0.9.0+

### Create and setup the Hub cluster
Open a new terminal window and run the following commands:
```
cd /tmp
git clone git@github.com:kubernetes-sigs/work-api.git
kind delete cluster --name hub 
kind create cluster --name hub
kind get kubeconfig --name hub  > /tmp/hub-io-kubeconfig
export KUBECONFIG=/tmp/hub-io-kubeconfig
cd /tmp/work-api
kubectl apply -f config/crd
cp /tmp/hub-io-kubeconfig hub-kubeconfig
kubectl config set clusters.kind-hub.server https://hub-control-plane:6443 --kubeconfig hub-kubeconfig
```

### Create and setup the Spoke cluster
Open another new terminal window and run the following commands:
```
kind delete cluster --name cluster1
kind create cluster --name cluster1
kind get kubeconfig --name cluster1 > /tmp/cluster1-io-kubeconfig
export KUBECONFIG=/tmp/cluster1-io-kubeconfig
cd /tmp/work-api
make docker-build
kind load docker-image --name=cluster1 work-api-controller:latest
kubectl apply -f deploy/component_namespace.yaml 
kubectl delete secret hub-kubeconfig-secret -n fleet-system --ignore-not-found
kubectl create secret generic hub-kubeconfig-secret --from-file=kubeconfig=hub-kubeconfig -n fleet-system 
rm hub-kubeconfig
kubectl apply -k deploy
```

### Run the controller against the Spoke cluster locally
Create a secret in the Spoke cluster that contains the kubconfig file pointing to the hub and run your code against.
```shell
kubectl create namespace fleet-system
kubectl delete secret hub-kubeconfig-secret -n fleet-system
kubectl create secret generic hub-kubeconfig-secret --from-file=kubeconfig=/Users/ryanzhang/.kube/hub -n fleet-system
go run cmd/workcontroller/workcontroller.go --work-namespace=default --hub-kubeconfig-secret=hub-kubeconfig-secret -v 5 -add_dir_header
```

### Run the e2e against a cluster locally
Create a secret in the cluster that contains the kubconfig file pointing to itself. Run the feature code against it.
```shell
kubectl create namespace fleet-system
kubectl delete secret hub-kubeconfig-secret -n fleet-system
kubectl create secret generic hub-kubeconfig-secret --from-file=kubeconfig=/Users/ryanzhang/.kube/member-a -n fleet-system
go run cmd/workcontroller/workcontroller.go --work-namespace=default --hub-kubeconfig-secret=hub-kubeconfig-secret -v 5 -add_dir_header

```
run the test in another window
```
export KUBECONFIG=kubeconfig=/Users/ryanzhang/.kube/member-a
go test . -test.v -ginkgo.v
```

### Deploy a Work on the Hub cluster
On the `Hub` cluster terminal, run the following command:
```
kubectl apply -f examples/example-work.yaml
```

### Verify delivery on the Spoke cluster
On the `Spoke` cluster terminal, run the following commands:
```
$ kubectl -n default get deploy test-nginx
NAME         READY   UP-TO-DATE   AVAILABLE   AGE
test-nginx   2/2     2            2           32s
$ kubectl -n default get service test-nginx
NAME         TYPE        CLUSTER-IP     EXTERNAL-IP   PORT(S)   AGE
test-nginx   ClusterIP   10.96.96.136   <none>        80/TCP    46s
```

### Modify the Work on the Hub cluster
On the `Hub` cluster terminal, run the following command:
```
kubectl apply -f examples/example-work-modify.yaml
```


### Code of conduct

Participation in the Kubernetes community is governed by the [Kubernetes Code of Conduct](code-of-conduct.md).

[1]:  https://img.shields.io/github/go-mod/go-version/Azure/k8s-work-api
[2]:  https://github.com//Azure/k8s-work-api/actions/workflows/ci.yml/badge.svg
[3]:  https://codecov.io/gh/Azure/k8s-work-api/branch/master/graph/badge.svg?token=POCZ4NVDJF
[4]:  https://codecov.io/gh/Azure/k8s-work-api
[5]:  https://img.shields.io/github/issues/azure/k8s-work-api
[6]:  https://img.shields.io/github/commit-activity/m/azure/k8s-work-api
[7]:  https://img.shields.io/github/v/release/Azure/k8s-work-api