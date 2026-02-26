# Berth: A Laravel PaaS Backend for Kubernetes

> **WARNING: This project is in early development and is experimental.  
> The architecture, codebase, and API are highly volatile and may introduce breaking changes at any time.  
> Do not use this in production environments.**

Berth is a lightweight Kubernetes-native backend for building and deploying Laravel applications.  
It exposes a REST API that manages Kubernetes Deployments and build Jobs within your cluster.

---

## How it works

```text
/apps/{name}/build
        │
        ├─► Kubernetes Job (BuildKit, rootless)
        │       1. git clone <repo>
        │       2. inject Dockerfile (if none exists)
        │       3. buildctl build + push → registry.kube-system:5000/<app>:latest
        │
        └─► Roll Deployment
                image = registry.kube-system:5000/<app>:latest
                annotation restartedAt = <now>  ← forces pod replacement
```

---

## Prerequisites

- A running Kubernetes cluster (tested on K3s)
- A Docker registry accessible at `registry.kube-system.svc.cluster.local:5000`  
  (change `RegistryBase` in `internal/build/job.go` if your registry differs)
- A valid kubeconfig for your cluster
- A MongoDB instance

---

## Running locally

```sh
go run ./cmd/server -kubeconfig ~/.kube/config -addr :8080
```
