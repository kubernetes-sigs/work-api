# Fleet Work-API

Fleet Work-API is a Work-management system that enables Work management across multiple Kubernetes clusters, with key features such as creation and garbage collection of work resources in a multi-cluster environment.

## TL;DR

Switch to the `root` directory of the repo.
```console
kubectl create namespace fleet-system
helm install deploy-work-api -n fleet-system 
```

## Installing the Chart
To install the chart with the release name `deploy-work-api` in namespace `fleet-system`:

Switch to the `root` directory of the repo.
```console
kubectl create namespace fleet-system
helm install deploy-work-api -n fleet-system 
```

Ensure that current context is set to a member cluster
```console
kubectl config use-context member-cluster
```

## Uninstalling the Chart
To uninstall/delete the `deploy-work-api` helm release in namespace `fleet-system`:

```console
helm uninstall deploy-work-api -n fleet-system
```