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
  will cover all the kernel based scenarios.

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
      repository: quay.io/grout/grout
      tag: "0.15.0"
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

### CI and Build

- A new image of the OpenPERouter will be shipped for this scenario. 
- A new CI job (`build-grout-images`) builds a Docker image tagged
  `quay.io/openperouter/router:grout` and based on `Dockerfile.grout`
  Current `router:main` image is based on Alpine Linux, which is a non-libc distro and
  DPDK/grout does not build on such system. Unless refactor the main image, which
  is not a goal of this proposal, grout integration will be provided as a side image.
- The Dockerfile installs grout, frr, and grout-frr RPMs from grout's 
  [GitHub release page](https://github.com/DPDK/grout/releases)
- E2E tests run with `--label-filter=passthrough` for the grout configuration
- A set of `e2etests` lanes will be added to test against a grout kind/clab deployment.
- As the features will be implemented incrementally, the `GINKGO_ARGS="--label-filter=..."`
  variable will be used to have a stable CI signal.

### Test Plan

- **E2E tests**: All the tests under `e2etests/` are supposed to run as is, without
  any specific change, on a grout based deployment.
- **Grout diagnostics**: `e2etests/tests/dump.go` needs to be extended to gather 
  information about grout state, when the feature is enabled.

### Milestone

#### M1

- Grout sidecar deployed as opt-in via Helm values.
- Underlay and passthrough setup implemented.
- FRR zebra configured with `dplane_grout` module when grout is enabled.
- E2E passthrough tests passing with grout-enabled configuration.
- Controller issues `grcli` command to configure grout.
- Grout interfaces are based on TAP devices. No hardware acceleration at this stage.
- grout runs in `--test-mode`, which means no huge pages are used at the moment.

#### M1a
_depends on M1_
- Support for hardware acceleration with SR-IOV NICs, CPU isolation and huge pages for L3Passthrough scenario.
- To be defined the degree of testing and CI we will be able to enforce in this repository.

#### M2
_depends on M1_
- L3VNI scenario implemented with grout and TAP interfaces.

#### M2a
_depdends on M2_
- HW acceleration support for L3VNI.

#### M3
_depends on M1_
- L2VNI scenario implemented with grout.

#### M3a
_depends on M1_
- HW acceleration support for L2VNI.

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
