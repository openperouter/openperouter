---
weight: 70
title: "Grout (DPDK Dataplane)"
description: "Using the optional DPDK-accelerated grout dataplane with OpenPERouter"
icon: "article"
date: "2026-05-07T09:00:00+02:00"
lastmod: "2026-10-14T09:00:00+02:00"
toc: true
---

## Overview

[Grout](https://github.com/DPDK/grout) is an optional, DPDK-accelerated data plane that can replace the Linux kernel's networking stack for packet forwarding in OpenPERouter. When enabled, grout handles VXLAN and SRv6 encapsulation/decapsulation and routing in user-space using poll-mode drivers, while [FRR](https://frrouting.org/) continues to manage the control plane (BGP, EVPN, IS-IS, route exchange).

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
- **L2VNI** (EVPN Layer 2 overlays) via grout bridge domains and TAP devices
- **IS-IS** underlay routing, with the tunnel endpoint carried on a dedicated
  `vtep-lo` grout port advertised as a passive interface
- **L3VPN** (SRv6 Layer 3 overlays) via grout VRFs and TAP devices
- **L2Trunk** of type `SRIOV`: L2VNIs carried, VLAN-tagged, by an SR-IOV VF
  bound to grout, with local VF-to-VF traffic switched by the NIC

The following are **not yet supported** with grout:

- `L2Trunk` of type `Host` (TAP trunk attached to a host bridge)

By default grout binds underlay NICs as **PCI ports**. Set
`--grout-tap-underlay` on the controller to use **TAP devices** (`net_tap` with
`remote=`). The `acceleratedConfig` fields configure either port type.

> **Warning**: Delete all `Underlay` resources before switching between TAP
> devices and accelerated (PCI) devices, in either direction. Recreate them
> once the controller is running with the new `--grout-tap-underlay` value.

## Prerequisites

TAP-based grout underlays need no DPDK-capable NIC or `vfio-pci`; they are
independent of test mode. With test mode disabled (the default), grout still
needs hugepages as described below. DPDK-accelerated underlay ports need a
PCI NIC supported by DPDK, including virtio-net PCI interfaces in virtual
machines.

## Helm Configuration

Select grout with `openperouter.datapath`; its sidecar settings are under
`openperouter.grout`:

```yaml
openperouter:
  datapath: grout
  grout:
    tapUnderlay: false
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
| `grout.tapUnderlay` | bool | `false` | Use TAP ports for grout underlay interfaces instead of PCI ports |
| `grout.testMode` | bool | `false` | Run grout in test mode. See [Test mode](#test-mode) |
| `grout.maxMTU` | int | `""` (grout default, 1800) | Maximum MTU grout accepts on its ports. See [MTU](#mtu) |
| `grout.image.repository` | string | `quay.io/openperouter/router` | Grout container image repository |
| `grout.image.tag` | string | `main-grout` | Grout container image tag |
| `grout.image.pullPolicy` | string | `""` | Image pull policy (defaults to Kubernetes default) |
| `grout.resources` | object | see above | Resource requests and limits for the grout container |

With the operator, set `GROUT_TAP_UNDERLAY=true` on the operator deployment to
select TAP underlay ports for its Grout installation.

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

### MTU

The grout underlay ports inherit the MTU of the underlay interfaces they are
attached to, so that the underlay MTU is preserved. Grout refuses ports whose
MTU exceeds its maximum MTU, which defaults to 1800: when the underlay
interfaces use a larger MTU (for example jumbo frames), raise it through
`openperouter.grout.maxMTU`, otherwise the underlay setup fails:

```yaml
openperouter:
  datapath: grout
  grout:
    maxMTU: 9500
```

Every packet buffer is sized after the maximum MTU, so larger values increase
grout memory usage.

With the operator, the maximum MTU is set through the `GROUT_MAX_MTU`
environment variable on the operator deployment.

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

## Enabling Grout for EVPN

`L3VNI` and `L2VNI` resources use the same configuration with the grout data
path as with the kernel data path. See the [EVPN
Configuration]({{< ref "evpn.md" >}}) documentation for the available fields and
examples.

For each `L2VNI`, grout creates a VXLAN-backed bridge domain and connects it to
the host through a TAP device. Disconnected L2VNIs provide east-west Layer 2
connectivity. When an L2VNI references an `L3VNI` routing domain, grout places
the bridge in that VRF and configures any addresses in `gatewayIPs` as the
distributed anycast gateway. The `hostMaster` settings continue to control the
Linux or OVS bridge attachment on the host.

Grout keeps the EVPN Type-2 routes of silent workloads alive by itself: when a
learned neighbor has not answered for 30 seconds, grout probes it, and a
neighbor that doesn't reply is removed. OpenPERouter sets this lifetime on
grout to match the kernel's default neighbor reachable time, instead of grout's
20 minute default.

## Enabling Grout for SRv6 L3VPN

`L3VPN` resources, and the `srv6` and `isis` settings of the `Underlay`, use
the same configuration with the grout data path as with the kernel data path.
See the [SRv6 L3VPN Configuration]({{< ref "srv6.md" >}}) documentation for
the available fields and examples.

For each `L3VPN`, grout creates a VRF and, when `hostSession` is set, connects
it to the host through a TAP device. FRR programs the SRv6 local SIDs and the
encapsulated routes into grout. `L2VNI` resources can use an `L3VPN` as their
routing domain, as with the kernel data path.

When grout is enabled, the controller adapts the generated FRR configuration:

- The underlay network devices are attached to grout ports named
  `u_<interfaceName>`, which are the interfaces FRR runs on. Keep using the
  network device names in the `Underlay` resource, for example in
  `isis.interfaces` and in the `interface` of the neighbors: the controller
  translates them to the grout port names.
- The tunnel endpoint IPv6 address is set as the SRv6 encapsulation source
  address (`segment-routing srv6 encapsulation source-address`).

## DPDK-Accelerated Underlay Ports

The controller binds `NetworkDevice` entries as PCI ports by default, even if
`acceleratedConfig` is absent. With `--grout-tap-underlay`, it uses TAP+`remote=`
ports for every underlay interface, including those with `acceleratedConfig`.
For PCI ports, the controller:

1. Resolves the PCI address from `/sys/class/net/<interfaceName>/device`
2. Saves the original driver and non-link-local addresses
3. Binds non-bifurcated NICs (for example Intel and virtio-net PCI) to
   `vfio-pci`, or moves `mlx5_core` devices (Mellanox) into the router namespace
4. Creates the grout port with `grcli interface add port u_<interfaceName> devargs <pci>`

The interface referenced by interfaceName must exist on the host. Any address
assigned to the interface will be used for the setup in the accelerated data path.
When the Underlay CR is disposed, the interface will be moved back to the kernel
together with its addresses and the original driver.

`acceleratedConfig` is rejected when `--datapath=kernel`.

> **Note**: The DPDK `mlx5` driver opens a `mlx5_core` device through its RDMA
> device as well as its netdev. The controller moves only the netdev into the
> router namespace, so the RDMA subsystem must run in shared network namespace
> mode, the kernel default: `rdma system show` must report `netns shared`. In
> exclusive mode, set for example by the sriov-network-operator for RDMA
> exclusive pools, the RDMA device stays in the host namespace and grout fails
> to open the port. This applies to underlay ports and to `SRIOV` trunk VFs.

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

All `acceleratedConfig` fields are optional. The field can be omitted to use
grout defaults for either port type.
All underlay ports are named `u_<interfaceName>`, so interface names must be at most 13 characters.
Underlay interfaces referenced in a `RawFRRConfig` must use the `u_` prefix.
Other interface names, such as `vtep-lo`, remain unchanged.
[Raw FRR Configuration]({{< ref "raw-frr-config.md" >}}).

> **Note**: virtio-net PCI NICs in virtual machines are supported as PCI ports.
> DPDK drives them through `vfio-pci`, so the VM needs a vIOMMU and the NIC must
> translate DMA through it; with QEMU, use `intel-iommu` with `device-iotlb=on`
> and `virtio-net-pci` with `iommu_platform=on,ats=on`. Without that, grout hangs
> while setting up the port. A virtio NIC has a single queue pair unless
> multiqueue is enabled (`queues=<N>` on the tap netdev and `mq=on` on the `virtio-net-pci` device), and
> `rxQueues` and `qSize` cannot exceed the queue pairs and the `rx_queue_size`
> of the device.

## SR-IOV Trunks

An `L2Trunk` of type `SRIOV` binds one SR-IOV Virtual Function to grout as a
trunk port. Every `L2VNI` referencing the trunk gets a grout VLAN interface on
that port, carrying its `trunk.vlan.tag`, attached to the L2VNI's bridge
domain. Workloads consume the other VFs of the same PF, configured with the
L2VNI's VLAN: the NIC switches the traffic between local VFs, and sends the
traffic to remote endpoints and to the L2 gateway to grout through the trunk
VF. No TAP device and no host bridge are involved.

```yaml
apiVersion: network.openperouter.io/v1alpha1
kind: L2Trunk
metadata:
  name: workloads
  namespace: openperouter-system
spec:
  type: SRIOV
  sriov:
    pfName: enp1s0
    vfIndex: 1
    acceleratedConfig:
      rxQueues: 4
      qSize: 2048
---
apiVersion: network.openperouter.io/v1alpha1
kind: L2VNI
metadata:
  name: tenant-a
  namespace: openperouter-system
spec:
  vni: 10100
  trunk:
    name: workloads
    vlan:
      tag: 100
```

When the trunk is created, the controller:

1. Resolves `pfName` through netlink, so it can be the primary name or an
   alternative name of the PF, then reads the VF PCI address from
   `/sys/class/net/<primaryName>/device/virtfn<vfIndex>`
2. Saves the VF's original driver
3. Binds the VF to `vfio-pci`, or moves `mlx5_core` VFs into the router
   namespace, and creates the grout port named after the last 10 characters of
   the PF name followed by the VF index in two hexadecimal digits (e.g.
   `enp1s001` for VF 1 of `enp1s0`)

`mlx5_core` VFs need the RDMA subsystem in shared network namespace mode, see
[DPDK-Accelerated Underlay Ports](#dpdk-accelerated-underlay-ports).

Each L2VNI carried by the trunk gets a VLAN interface on the trunk port, named
after the trunk port name followed by the VLAN tag in three hexadecimal digits
(e.g. `enp1s001021` for VLAN 33 on VF 1 of `enp1s0`). The `vfIndex` is
therefore limited to 255, the highest value whose hexadecimal form fits two
characters.

Deleting the trunk removes its VLAN interfaces and its port, and hands the VF
back to its original driver. The referencing L2VNIs stay in the API, but lose their data plane configuration until the trunk exists again.

Prerequisites:

- SR-IOV is enabled on the PF, with more than `vfIndex` VFs
- The trunk VF is bound to its kernel VF driver when the trunk is created:
  the controller captures the VF's state from its kernel netdev before
  handing the VF to grout, and a VF already bound to `vfio-pci`, for example
  by a `SriovNetworkNodePolicy` with `deviceType: vfio-pci` covering its
  index, has no kernel netdev and fails the trunk setup
- The trunk VF is configured on the PF by the user, as the controller does not
  change its PF-side settings:
  - no VLAN, so the workload VFs' traffic reaches grout tagged
  - `spoofchk off`, as the VF forwards frames of remote endpoints with their
    own source MAC
  - `trust on`, so the VF receives frames for MACs it does not own

  For example: `ip link set <pfName> vf <vfIndex> vlan 0 spoofchk off trust on`
- The trunk VF is not allocated to workloads, for
  example by excluding it from the sriov-device-plugin resource pools
- The trunk VF is not an underlay `NetworkDevice`: DPDK opens a PCI device
  only once. A trunk selecting an underlay's VF fails, and an underlay
  selecting a trunk's VF fails the underlay setup
- The PF is not an underlay `NetworkDevice`: using the PF as the underlay
  breaks the VF-to-VF traffic of the trunk. Use another VF of the PF, or
  another NIC, as the underlay
- The workload VFs are configured with the VLAN tags of their L2VNIs, by the
  user or by the sriov-network-operator

Two trunks applying to the same node cannot select the same PF, as each trunk VF
uplinks its whole PF, and two L2VNIs cannot use the same tag on the same trunk.
`SRIOV` trunks are rejected when `--datapath=kernel`.

For a complete workload setup, including `SriovNetworkNodePolicy`,
`SriovNetwork`, and DPDK pods, see the [Layer 2 SR-IOV with Grout
example]({{< relref "../examples/evpnexamples/layer2-sriov.md" >}}).

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
