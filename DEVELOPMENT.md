# Development Guide

This document describes the development targets available in the Makefile and how to use them for OpenPERouter development.

## Initial Setup

```bash
git clone https://github.com/openperouter/openperouter
cd openperouter
```

## Code Generation and Building

Generate all code and manifests.

```bash
make generate
```

Format the Go code and run static checks.

```bash
make format check
```

Build the binaries.

```bash
make build
```

Or run all of the commands above all at once.

```bash
make
```

## Unit Tests

Run unit and integration tests. Some of the integration tests require privileged host access and sudo.

```bash
make test
```

## End-to-end Tests and Local Cluster Deployment

Bring up the local kind cluster with ContainerLab for testing.

```bash
make cluster-up
```

After cluster setup, export the kubeconfig to interact with the cluster.

```bash
export KUBECONFIG=bin/kubeconfig
kubectl get nodes
```
Rebuild sources, build container images, upload to cluster and restart workload.

```bash
make cluster-sync
kubectl -n openperouter-system get pods
```

> **Note:** Some operating systems have their `inotify.max_user_intances`
> set too low to support larger kind clusters. This leads to nodemarker pods
> failing with CrashLoopBackOff, logging `too many open files`.
>
> If that happens in your setup, you may want to increase the limit with:
>
> ```bash
> sysctl -w fs.inotify.max_user_instances=1024
> ```

Run end-to-end tests.

```bash
make e2etests

# Focus on L2VNI tests
make e2etests GINKGO_ARGS="-focus=L2VNI"

# Run with verbose output
make e2etests GINKGO_ARGS="-v"
```
