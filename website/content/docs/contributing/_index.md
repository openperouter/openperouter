---
weight: 100
title: "Contributing"
description: "How to contribute to OpenPERouter"
icon: "article"
date: "2025-06-15T15:03:22+02:00"
lastmod: "2025-06-15T15:03:22+02:00"
toc: true
---


We would love to hear from you! Here are some places you can find us.

## Issue Tracker

Use the [GitHub issue
tracker](https://github.com/openperouter/openperouter/issues) to file bugs and
features request. 

# Contributing

Contributions are more than welcome! Here's some information to get
you started.

## Code of Conduct

This project is released with a [Contributor Code of Conduct]({{%
relref "code-of-conduct.md" %}}). By participating in this project you
agree to abide by its terms.

## Code changes

Before you make significant code changes, please consider opening a pull
request with a proposed design in the `design/` directory. That should
reduce the amount of time required for code review. If you don't have a full
design proposal ready, feel free to open an issue to discuss what you would
like to do.

All submissions require review. We use GitHub pull requests for this
purpose. Consult [GitHub
Help](https://help.github.com/articles/about-pull-requests/) for more
information on using pull requests.

## Certificate of Origin

By contributing to this project you agree to the Developer Certificate of
Origin (DCO). This document was created by the Linux Kernel community and is a
simple statement that you, as a contributor, have the legal right to make the
contribution. See the [DCO](https://github.com/openperouter/openperouter/blob/main/DCO)
file for details.

## Code organization

The OpenPERouter codebase is organized to separate core functionality, supporting libraries, deployment resources, and documentation. Here is an overview of the most relevant directories:

- `cmd/` – Contains the main entry points for the OpenPERouter binaries. Please check the [architecture documentation]({{< relref "../architecture.md" >}}) for an overview of the binaries and their responsibilities.
- `api/` – Defines the Kubernetes Custom Resource Definitions (CRDs) and API types used by OpenPERouter.
- `internal/` – Contains internal packages that implement the core logic of OpenPERouter. This directory is not intended for use outside the project and is structured as follows:
  - `controller/` – Implements the controllers logic, including reconciliation loops and resource management for OpenPERouter components.
  - `conversion/` – Handles conversion logic between different API versions or resource representations.
  - `frr/` – Contains code related to FRRouting (FRR) integration, such as configuration generation and management.
  - `frrconfig/` – Manages FRR configuration files and templates used by OpenPERouter.
  - `hostnetwork/` – Provides utilities and logic for managing host networking aspects required by FRR to make EVPN work.
  - `ipam/` – Implements IP Address Management (IPAM) logic, including allocation and tracking of IP addresses.
- `operator/` – Contains the Kubernetes operator for OpenPERouter, including its main code, API, and configuration.
- `charts/` – Helm charts for deploying OpenPERouter and its components.
- `e2etests/` – End-to-end test suite for validating OpenPERouter functionality.

This structure helps keep the project modular, maintainable, and easy to navigate for contributors and users alike.

In addition to code, there's deployment configuration and
documentation:

- **Helm charts**: The `charts/` directory contains Helm charts for deploying OpenPERouter and its components. These charts provide a convenient way to install, upgrade, and manage OpenPERouter in Kubernetes environments. Refer to the README in the `charts/` directory for usage instructions and configuration options.

- **Configuration**: Deployment and runtime configuration files are located in the `operator/config/` directory. This includes manifests for Kubernetes resources, sample CRs, and other configuration templates. Review these files to understand how to customize OpenPERouter for your environment.

- **Website and Documentation**: The `website/` directory contains the source for the OpenPERouter documentation site. Contributions to documentation, guides, and tutorials are welcome. To propose changes, edit the relevant Markdown files and submit a pull request.

## Building and running the code

Start by fetching the OpenPERouter repository.

```bash
git clone https://github.com/openperouter/openperouter
cd openperouter
```

### Code Generation and Building

Generate all code and manifests.

```bash
make generate
```

Format the Go code and run static checks.

```bash
make format static-check
```

Build the binaries.

```bash
make build
```

Or run all of the commands above all at once.

```bash
make
```

### Unit Tests

Run unit and integration tests. Some of the integration tests require privileged host access and sudo.

```bash
make test
```

### Local Development Environment

#### Two-Step Deployment Process

The OpenPERouter development environment follows a two-step process:

1. **Cluster Setup**: Create the local kind cluster with ContainerLab networking
2. **Workload Deployment**: Build and deploy the OpenPERouter controller

#### Step 1: Cluster Setup

Bring up the local kind cluster with ContainerLab for testing.

```bash
make cluster-up
```

This command:
- Creates a kind cluster
- Sets up ContainerLab networking environment
- Loads base container images (FRR, rbac-proxy, etc.)

After cluster setup, export the kubeconfig to interact with the cluster.

```bash
export KUBECONFIG=bin/kubeconfig
kubectl get nodes
```

#### Step 2: Workload Deployment

Rebuild sources, build container images, upload to cluster and restart workload.

```bash
make cluster-sync
kubectl -n openperouter-system get pods
```

This command:
- Generates code and manifests
- Builds the OpenPERouter Docker image
- Loads the application container image to the cluster
- Deploys the controller to the cluster

**Alternative Deployment Methods:**

Deploy using Helm charts:
```bash
make cluster-sync-helm
```

Deploy using OLM (Operator Lifecycle Manager):
```bash
make cluster-sync-olm
```

Deploy with Prometheus monitoring (explicit three-step process):
```bash
make cluster-up
make cluster-sync
make enable-prometheus
```

**Adding Monitoring to Existing Deployment:**

You can also add Prometheus monitoring to an existing deployment:
```bash
# First deploy normally
make cluster-sync

# Later add Prometheus monitoring
make enable-prometheus
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

Check [the dev environment documentation]({{< relref "devenv.md" >}}) for more details.

### Running the tests locally

Run end-to-end tests on your local environment deployed above.

```bash
make e2etests

# Focus on L2VNI tests
make e2etests GINKGO_ARGS="-focus=L2VNI"

# Run with verbose output
make e2etests GINKGO_ARGS="-v"
```

## Commit Messages

The following are our commit message guidelines:

- Line wrap the body at 72 characters
- For a more complete discussion of good git commit message practices, see
  <https://chris.beams.io/posts/git-commit/>.

## Extending the end to end test suite

When adding a new feature, or modifying a current one, consider adding a new test
to the test suite located in `/e2etest`.
Each feature should come with enough unit test / end to end coverage to make
us confident of the change.



