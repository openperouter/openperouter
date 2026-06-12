# Grout: DPDK-Accelerated Dataplane for OpenPERouter

## Summary

OpenPERouter currently relies on the Linux kernel's networking stack for data
plane forwarding (VXLAN encap/decap, VRF routing, bridge learning). While
correct and stable, kernel-based forwarding has inherent performance limits
due to interrupt-driven packet processing and kernel/user-space transitions.

This enhancement adds **grout** as an optional, DPDK-accelerated data plane
that runs alongside FRR as a sidecar container ([GitHub](https://github.com/DPDK/grout)). 
When enabled, grout replaces the kernel networking stack for packet forwarding while FRR continues to
handle the control plane via its `dplane_grout` zebra [module](https://docs.frrouting.org/en/latest/basic.html#loadable-module-support).
The integration is opt-in and does not affect the default kernel-based data path.

## Motivation

### Goals

- **Higher forwarding throughput**: leverage DPDK poll-mode drivers to bypass
  kernel overhead for VXLAN encapsulation, decapsulation, and routing.
- **Opt-in activation**: grout is disabled by default. Operators enable it via
  a Helm value (`openperouter.grout.enabled: true`) with no impact on existing
  deployments.
- **Reuse existing control plane**: FRR remains the routing daemon. Zebra's
  `dplane_grout` module pushes forwarding entries to grout instead of the
  kernel, requiring no changes to BGP, EVPN, or route exchange logic.
- **Support underlay and passthrough flows**: the initial integration covers
  underlay interface setup and L3 passthrough via grout ports. Following milestone
  will cover all the kernel based scenarios, adding HW acceleration via SR-IOV
  Virtual Functions plugged into workload pods. 

### Non-Goals

- Replacing the kernel-based data plane. Both modes coexist; the kernel path
  remains the default.

## Proposal

### Overview

Grout runs as a privileged sidecar container in the router DaemonSet pod. It
exposes a UNIX socket (`/var/run/grout/grout.sock`) that serves two consumers:

1. **FRR (zebra)**: uses the `dplane_grout` module to program forwarding
   entries into grout instead of the kernel's netlink interface. The socket path
   is injected via the `GROUT_SOCK_PATH` environment variable.
2. **The controller**: uses the `grcli` CLI  to configure grout ports, addresses, 
   vrfs and routes on the grout instance.

When grout is enabled, the controller will take an alternative configuration path
instead of `configureInterfaces()`, delegating interface setup to a
dedicated grout package.


### User Stories

#### Story 1: High-Throughput PE Router

As a network operator running OpenPERouter on nodes with DPDK-capable NICs, I
want to enable grout so that VXLAN encap/decap and routing happen in user-space
with poll-mode drivers, achieving higher packet-per-second throughput than the
kernel data plane.


## Design Details

### Helm Configuration

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

When `grout.enabled` is `true`, the Helm templates add:

- The `-M dplane_grout` module flag to FRR's zebra process
- The `GROUT_SOCK_PATH` environment variable to the FRR container
- A grout sidecar container in the router pod
- A `grout-socket` hostPath volume at `/var/run/openperouter/grout`
- `--grout-enabled=true` and `--grout-socket` flags to the controller

### Controller Integration

The controller receives two new CLI flags:

- `--grout-enabled` (bool): switches the reconciler to the grout data path
- `--grout-socket` (string, default `/var/run/grout/grout.sock`): path to the
  grout UNIX socket

When grout is enabled, the reconciler configures FRR as before. After that, the host network
configuration is skipped in favor of a Grout configuration.

#### Configuring Grout

The following paragraphs show how the controller implements the PERouter logic by
configuring the deployed grout instance.

##### L3Passthrough

The following actions are executed by the controller when the example Underlay + L3Passthrough
configuration is in place.

```
apiVersion: ...
kind: Underlay
metadata:
  name: underlay
  namespace: openperouter-system
spec:
  asn: 64514
  nics:
    - eth3
  neighbors:
    - asn: 64512
      address: 192.168.11.2
---
apiVersion: ...
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

Underlay
1. The controller moves `eth3` (from `Underlay.spec.nics[0]`) interface to the 
   router’s namespace. This step is common to the kernel datapath.
2. controller creates a grout port for the underlay, linking it to the eth3 nic:

   `grcli interface add port underlay devargs net_tap0,remote=eth3,iface=tap_underlay`

3. move all the ip addresses from eth3 to the grout underlay port
  
   `grcli address add <eth3_ip> iface underlay`

4. set kernel route for all the moved ip addresses to use grout. `main` is the default vrf 
   control plane interface. This is needed to make sure all the connections created by FRR
   go through Grout
  
   `ip route add <eth3_net> dev main src <eth3 IP addr>`


L3Passthrough
1. The controller creates a pair grout_port/tap_interface (compared to the veth pair of kernel datapath):

   `grcli interface add port pt-ns devargs net_tap1,iface=pt-host`

   `grcli address add 192.169.10.1/31 iface pt-ns`

2. The controller move the pt-host tap interface to the host namespace and configures it

   `ip link set pt-host netns <host>`
   
   `ip addr add 192.169.10.2/31 dev pt-host`



### CI and Build

- A new image of the OpenPERouter will be shipped for this scenario. 
- A new CI job (`build-grout-images`) builds a Docker image tagged
  `quay.io/openperouter/router:main-grout` and based on `Dockerfile.grout`
  Current `router:main` image is based on Alpine Linux, which is a non-libc distro and
  DPDK/grout does not build on such system. Unless refactor the main image, which
  is not a goal of this proposal, grout integration will be provided as a side image.
- The Dockerfile installs grout, FRR, and grout-frr RPMs from grout's 
  [GitHub release page](https://github.com/DPDK/grout/releases). Using an FRR upstream
  release involves further investigation on the dataplane plugin mechanism, and will be
  addressed in a later milestone (See M4).
- E2E tests run with `--label-filter=grout-support` for the grout configuration, so we can 
  mark which scenarios are currenlty supported by the configuration.
- A set of `e2etests` lanes will be added to test against a grout kind/clab deployment.
- As the features will be implemented incrementally, the `GINKGO_ARGS="--label-filter=..."`
  variable will be used to have a stable CI signal.

### Test Plan

- **E2E tests**: All the tests under `e2etests/` are supposed to run as is, without
  any specific change, on a grout based deployment.
- **Grout diagnostics**: `e2etests/tests/dump.go` needs to be extended to gather 
  information about grout state, when the feature is enabled.

### Milestones

#### M1 Grout Deployment and L3Passthrough
- Grout sidecar deployed as opt-in via Helm values.
- Underlay and passthrough setup implemented.
- FRR zebra configured with `dplane_grout` module when grout is enabled.
- E2E passthrough tests passing with grout-enabled configuration.
- Controller issues `grcli` command to configure grout.
- Grout interfaces are based on TAP devices. No hardware acceleration at this stage.
- grout runs in `--test-mode`, which means no huge pages are used at the moment.

#### M1a Underlay HW acceleration
_depends on M1_
- Support for hardware acceleration with SR-IOV NICs, CPU isolation and huge pages for the Underlay NIC.
- To be defined the degree of testing and CI we will be able to enforce in this repository.
- When the Underlay nic is an SR-IOV capable NIC configured with VFs, then the PERouter binds a DPDK driver
  to it to communicate with the fabric. 

#### M2 L3VNI
_depends on M1_
- L3VNI scenario implemented with grout and TAP interfaces.

#### M3 L2VNI
_depends on M1_
- L2VNI scenario implemented with grout. The controller triggers a TAP interface creation on Grout and moves it
  to the host network namespace. Then the user can create a `host-device` NetworkAttachmentDefinition to move the TAP
  to a Pod.

#### M3a L2VNI with VFpair
_depends on M3_ 
- HW acceleration support for L2VNI. The API must be enhanced with the information about a set of SR-IOV Virtual Function.
  The VFs will be set on a configurable VLAN and will be used for intra-node communications. 
- The first VF are attached to Grout. The other VFs stay in the host network namespace.
- The user creates `sriov` NetworkAttachmentDefinitions to move the VF to the workload pod and leverage the L2VNI 
  connectivity with kernel or DPDK applications.

#### M4 - Use upstream FRR image
- For the `quay.io/openperouter/router:main-grout` image, use the upstream released FRR image instead of the 
  one built in the DPDK/grout project. 

#### M5 - Optional
- Implement a Golang library in github.com/DPDK/grout to issue commands to grout, instead of
  using grcli

## Drawbacks

- **Privileged container**: grout requires raw device access, increasing the
  security surface of the router pod.
- **Incomplete VNI support**: L2VNI and L3VNI are stubbed. Operators who need
  VNI forwarding acceleration must wait for follow-up work.
- **Hardware dependency**: DPDK requires specific NIC hardware and driver
  binding (vfio-pci, uio_pci_generic), limiting portability.
- **Dedicated CPU cores for zero packet loss**: achieving zero packet loss
  with grout requires dedicating CPU cores to its data plane. When idle,
  grout uses micro-sleeps to allow CPUs to enter lower C-states, and
  overall it achieves a higher packets-per-second-per-watt ratio than
  Linux. However, the kernel path does not require dedicated cores
  (though it also does not guarantee zero packet loss).

## Implementation History

- 2026-04-22: Initial proposal drafted
