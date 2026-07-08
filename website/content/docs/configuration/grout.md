---
weight: 70
title: "Grout (DPDK Dataplane)"
description: "Using the optional DPDK-accelerated grout dataplane with OpenPERouter"
icon: "article"
date: "2026-05-07T09:00:00+02:00"
lastmod: "2026-05-07T09:00:00+02:00"
toc: true
---

## Overview

[Grout](https://github.com/DPDK/grout) is an optional, DPDK-accelerated data plane that can replace the Linux kernel's networking stack for packet forwarding in OpenPERouter. When enabled, grout handles VXLAN encapsulation/decapsulation and routing in user-space using poll-mode drivers, while [FRR](https://frrouting.org/) continues to manage the control plane (BGP, EVPN, route exchange).

The integration is opt-in: grout is disabled by default and enabling it does not affect existing kernel-based deployments.

## Architecture

When grout is enabled, it runs as a sidecar container in the router DaemonSet pod. It exposes a UNIX socket that serves two consumers:

- **FRR (zebra)**: uses the [`dplane_grout`](https://docs.frrouting.org/en/latest/basic.html#loadable-module-support) module to push forwarding entries into grout instead of the kernel's routing tables.
- **The controller**: uses the `grcli` CLI to configure grout ports, addresses, VRFs, and routes.

Compared to the default kernel-based deployment, enabling grout adds:

- A **grout sidecar container** in the router pod
- The `-M dplane_grout` module flag to FRR's zebra process
- The `GROUT_SOCK_PATH` environment variable for FRR to locate the grout socket
- `--datapath=grout` flag to the controller
- A shared `grout-socket` volume between the grout sidecar and the FRR container

## Current Scope and Limitations

Grout support is being delivered incrementally. The current implementation covers:

- **Underlay** interface setup via grout ports
- **L3Passthrough** forwarding via grout

The following are **not yet supported** with grout:

- L3VNI (EVPN Layer 3 overlays)
- L2VNI (EVPN Layer 2 overlays)
- Hardware acceleration with SR-IOV NICs

Additionally, grout currently:

- Uses **TAP devices** rather than DPDK poll-mode drivers bound to physical NICs
- Runs in **`--test-mode`**, meaning no hugepages are required

These limitations will be addressed in subsequent milestones.

## Prerequisites

For the current scope, no special hardware is required — grout uses TAP devices and test-mode. In future milestones, DPDK-capable NICs and hugepage configuration will be needed for hardware-accelerated forwarding.

## Helm Configuration

Grout is configured under `openperouter.grout` in the Helm values:

```yaml
openperouter:
  grout:
    enabled: true
    image:
      repository: quay.io/openperouter/router
      tag: "main-grout"
      pullPolicy: ""
    resources:
      requests:
        memory: "512Mi"
        cpu: "250m"
      limits:
        memory: "1Gi"
        cpu: "500m"
```

### Configuration Fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `dataoath` | string | `kernel` | Datapath to use for L3 forwarding. "kernel" uses the standard Linux kernel datapath; "grout" adds a DPDK-accelerated sidecar that runs alongside FRR |
| `grout.image.repository` | string | `quay.io/openperouter/router` | Grout container image repository |
| `grout.image.tag` | string | `main-grout` | Grout container image tag |
| `grout.image.pullPolicy` | string | `""` | Image pull policy (defaults to Kubernetes default) |
| `grout.resources` | object | see above | Resource requests and limits for the grout container |

## Enabling Grout for L3Passthrough

The Underlay and L3Passthrough Custom Resources are the same as the kernel-based deployment. The only difference is enabling grout in the Helm values.

### Step 1: Install with Grout Enabled

```bash
helm install openperouter openperouter/openperouter \
  --set openperouter.grout.enabled=true
```

Or using a values file:

```yaml
# values.yaml
openperouter:
  grout:
    enabled: true
```

```bash
helm install openperouter openperouter/openperouter -f values.yaml
```

### Step 2: Configure Underlay and L3Passthrough

Apply the same CRs as the kernel-based passthrough setup. See the [Passthrough Configuration]({{< ref "passthrough.md" >}}) documentation for full details.

```yaml
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: Underlay
metadata:
  name: underlay
  namespace: openperouter-system
spec:
  asn: 64514
  interfaces:
    - type: NetworkDevice
      networkDevice:
        interfaceName: toswitch
  neighbors:
    - asn: 64512
      address: 192.168.11.2
---
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: L3Passthrough
metadata:
  name: passthrough
  namespace: openperouter-system
spec:
  hostsession:
    asn: 64514
    hostasn: 64515
    localcidr:
      ipv4: 192.169.10.0/24
```

When grout is enabled, the controller configures FRR as usual but delegates the host network setup to the grout data path instead of kernel interfaces.

## Verification

### Check Grout Sidecar Status

Verify that the grout container is running in the router pod:

```bash
kubectl get pods -n openperouter-system -l app=router
```

Check grout container logs:

```bash
kubectl logs -n openperouter-system -l app=router -c grout
```

### Check BGP Sessions

Verify that BGP sessions are established. The control plane behavior is identical to the kernel-based deployment — FRR handles all BGP operations, with grout handling the forwarding plane.
