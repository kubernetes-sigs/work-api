# Work API

This repo will hold design documents and implementation of the [Work API](https://docs.google.com/document/d/1cWcdB40pGg3KS1eSyb9Q6SIRvWVI8dEjFp9RI0Gk0vg/edit#).

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
- Install the `work` agent components to the `spoke` cluster.
- Deploy a `work` example on the `hub` cluster.
- Verify all the contents inside the `work` has been delivered in the `spoke` cluster.

### Prerequisites
- [Go](https://golang.org) version v1.20+
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
kubectl delete secret hub-kubeconfig-secret -n work --ignore-not-found
kubectl create secret generic hub-kubeconfig-secret --from-file=kubeconfig=hub-kubeconfig -n work 
rm hub-kubeconfig
kubectl apply -k deploy
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

### Code of conduct

Participation in the Kubernetes community is governed by the [Kubernetes Code of Conduct](code-of-conduct.md).
