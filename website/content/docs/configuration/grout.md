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

- **Underlay** interface setup via grout ports, including optional DPDK
  acceleration (`acceleratedConfig` on `NetworkDevice`)
- **L3Passthrough** forwarding via grout
- **L3VNI** (EVPN Layer 3 overlays) via grout TAP devices

The following are **not yet supported** with grout:

- L2VNI (EVPN Layer 2 overlays)
- Hardware acceleration of L2VNI via VF pairs

By default grout uses **TAP devices** (`net_tap` with `remote=`) rather than
binding physical NICs to a DPDK poll-mode driver. Add `acceleratedConfig` to a
`NetworkDevice` to bind that device as a DPDK port instead.

## Prerequisites

TAP-based grout underlays need no DPDK-capable NIC or `vfio-pci`; they are
independent of test mode. With test mode disabled (the default), grout still
needs hugepages as described below. DPDK-accelerated underlay ports need a
DPDK-capable NIC.

## Helm Configuration

Select grout with `openperouter.datapath`; its sidecar settings are under
`openperouter.grout`:

```yaml
openperouter:
  datapath: grout
  grout:
    testMode: false
    image:
      repository: quay.io/openperouter/router
      tag: "main-grout"
      pullPolicy: ""
    resources:
      requests:
        memory: "512Mi"
        cpu: "250m"
      limits:
        memory: "2Gi"
        cpu: "500m"
```

### Configuration Fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `datapath` | string | `kernel` | Datapath to use for L3 forwarding. "kernel" uses the standard Linux kernel datapath; "grout" adds a DPDK-accelerated sidecar that runs alongside FRR |
| `grout.testMode` | bool | `false` | Run grout in test mode. See [Test mode](#test-mode) |
| `grout.image.repository` | string | `quay.io/openperouter/router` | Grout container image repository |
| `grout.image.tag` | string | `main-grout` | Grout container image tag |
| `grout.image.pullPolicy` | string | `""` | Image pull policy (defaults to Kubernetes default) |
| `grout.resources` | object | see above | Resource requests and limits for the grout container |

### Test mode

By default grout runs against hugepages, with real longest prefix match (LPM)
FIB tables. Hugepages must be allocated on the node, and requested through
`grout.resources`, which is passed to the container verbatim:

```yaml
openperouter:
  datapath: grout
  grout:
    resources:
      requests:
        memory: "512Mi"
        cpu: "250m"
        hugepages-2Mi: "1Gi"
      limits:
        memory: "2Gi"
        cpu: "500m"
        hugepages-2Mi: "1Gi"
```

Setting `openperouter.grout.testMode` to `true` instead starts grout with
`--test-mode` and with the `DUMMY` FIB algorithms. Test mode needs no
hugepages, and `DUMMY` skips the LPM tables whose per-VRF allocation would
otherwise exhaust typical pod memory — at the cost of real route lookups. This
is the configuration the project's end-to-end tests run against:

```yaml
openperouter:
  datapath: grout
  grout:
    testMode: true
```

With the operator, test mode is set through the `GROUT_TEST_MODE` environment
variable on the operator deployment rather than through the Helm values.

## Enabling Grout for L3Passthrough

The Underlay and L3Passthrough Custom Resources are the same as the kernel-based deployment. The only difference is enabling grout in the Helm values.

### Step 1: Install with Grout Enabled

```bash
helm install openperouter openperouter/openperouter \
  --set openperouter.datapath=grout
```

Or using a values file:

```yaml
# values.yaml
openperouter:
  datapath: grout
```

```bash
helm install openperouter openperouter/openperouter -f values.yaml
```

### Step 2: Configure Underlay and L3Passthrough

Apply the same CRs as the kernel-based passthrough setup. See the [Passthrough Configuration]({{< ref "passthrough.md" >}}) documentation for full details.

```yaml
apiVersion: network.openperouter.io/v1alpha1
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
apiVersion: network.openperouter.io/v1alpha1
kind: L3Passthrough
metadata:
  name: passthrough
  namespace: openperouter-system
spec:
  hostSession:
    asn: 64514
    hostASN: 64515
    localCIDRs:
      - 192.169.10.0/24
```

When grout is enabled, the controller configures FRR as usual but delegates the host network setup to the grout data path instead of kernel interfaces.

## DPDK-Accelerated Underlay Ports

`NetworkDevice` entries without `acceleratedConfig` use the TAP+`remote=` path.
When `acceleratedConfig` is set, the controller binds the device as a DPDK port:

1. Resolves the PCI address from `/sys/class/net/<interfaceName>/device`
2. Saves the original driver, MTU, and non-link-local addresses
3. Binds non-bifurcated NICs (for example Intel) to `vfio-pci`, or moves
   `mlx5_core` devices (Mellanox) into the router namespace
4. Creates the grout port with `grcli interface add port <portName> devargs <pci>`


The interface referenced by interfaceName must exist on the host. Any address 
assigned to the interface will be used for the setup in the accelerated data path.
When the Underlay CR is disposed, the interface will be moved back to the kernel 
together with its addresses and the original driver.

`acceleratedConfig` is rejected when `--datapath=kernel`.

```yaml
apiVersion: network.openperouter.io/v1alpha1
kind: Underlay
metadata:
  name: underlay-dpdk
  namespace: openperouter-system
spec:
  asn: 64514
  interfaces:
    - type: NetworkDevice
      networkDevice:
        interfaceName: enp3s0f0v0
        acceleratedConfig:
          rxQueues: 4
          qSize: 1024
  neighbors:
    - asn: 64512
      address: 192.168.1.1
```

All `acceleratedConfig` fields are optional. `acceleratedConfig: {}` enables
DPDK attachment with grout defaults. `promiscuous` defaults to false.
`portName` overrides the grout port name (`u_<interfaceName>` when unset).

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
