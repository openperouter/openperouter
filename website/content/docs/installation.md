---
weight: 30
title: "Installation"
description: "Installation guide for OpenPERouter"
icon: "article"
date: "2025-06-15T15:03:22+02:00"
lastmod: "2025-06-15T15:03:22+02:00"
toc: true
---

This guide covers the installation of OpenPERouter using different deployment methods.

## Prerequisites

Before installing OpenPERouter, ensure you have:

- A Kubernetes cluster (v1.20 or later)
- `kubectl` configured to communicate with your cluster
- Cluster administrator privileges (for creating namespaces and CRDs)
- Network interfaces configured for BGP peering with external routers
- For OpenShift: `oc` CLI tool and cluster admin access

## Installation Methods

OpenPERouter offers several deployment methods to suit different environments and preferences:

### Method 1: All-in-One Manifests (Quick Start)

The simplest way to install OpenPERouter is using the all-in-one manifests. This method is ideal for testing and development environments.

#### Standard Installation (containerd)

```bash
kubectl apply -f https://raw.githubusercontent.com/openperouter/openperouter/v0.0.3/config/all-in-one/openpe.yaml
```

#### CRI-O Variant

If your cluster uses CRI-O as the container runtime:

```bash
kubectl apply -f https://raw.githubusercontent.com/openperouter/openperouter/v0.0.3/config/all-in-one/crio.yaml
```

### Method 2: Kustomize Installation

Kustomize provides more flexibility for customizing the deployment. This method is recommended for production environments.

#### Default Configuration

Create a `kustomization.yaml` file:

```yaml
namespace: openperouter-system

resources:
  - github.com/openperouter/openperouter/config/default?ref=v0.0.3
```

Then apply it:

```bash
kubectl apply -k .
```

#### CRI-O Variant with Kustomize

```yaml
namespace: openperouter-system
resources:
  - github.com/openperouter/openperouter/config/crio?ref=v0.0.3
```

### Method 3: Helm Installation

Helm provides the most flexibility for configuration and is recommended for production deployments.

#### Add the Helm Repository

```bash
helm repo add openperouter https://openperouter.github.io/openperouter
helm repo update
```

#### Install OpenPERouter

```bash
helm install openperouter openperouter/openperouter
```

#### Customize Installation

You can customize the installation by creating a values file:

```yaml
# values.yaml
openperouter:
  logLevel: "info"
  cri: "containerd"
  frr:
    image:
      repository: "quay.io/frrouting/frr"
      tag: "10.2.1"
```

Then install with custom values:

```bash
helm install openperouter openperouter/openperouter -f values.yaml
```

## Platform-Specific Instructions

### OpenShift

When running on OpenShift, additional Security Context Constraints (SCCs) must be configured:

```bash
oc adm policy add-scc-to-user privileged -n openperouter-system -z controller
oc adm policy add-scc-to-user privileged -n openperouter-system -z perouter
```

## Verification

After installation, verify that all components are running correctly:

```bash
kubectl get pods -n openperouter-system
```

You should see pods for:

- `openperouter-controller-*` (controller daemonset)
- `openperouter-router-*` (router daemonset)
- `openperouter-nodemarker-*` (node labeler deployment)

## Next Steps

After successful installation:

1. Configure the [underlay connection]({{< ref "configuration/#underlay-configuration" >}}) to your external router
2. Set up [VNI configurations]({{< ref "configuration/#l3-vni-configuration" >}}) for your EVPN overlays
3. Test the integration with [BGP-speaking components]({{< ref "examples" >}})

For troubleshooting, check the [contributing guide]({{< ref "contributing" >}}) for development environment setup and debugging information.
