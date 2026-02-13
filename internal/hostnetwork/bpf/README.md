# Shared NIC Mode - eBPF PoC

## Overview

This PoC implements a **shared NIC mode** for OpenPERouter that keeps the
underlay NIC in the host network namespace instead of moving it into the router
pod. eBPF TC programs attached to the NIC steer specific traffic (BGP, BFD,
ARP, VXLAN) between the physical interface and the router pod through a veth
pair (`ul-host` / `ul-pe`).

This is designed for **cloud environments** where the NIC's IP and MAC address
must remain visible to the cloud infrastructure (e.g. for source/destination
checks, security groups, or routing policies). Moving the NIC into a pod
namespace would break these assumptions.

## Architecture

```
 +-----------+     +-----------+
 | leafkind  |     | remote    |
 | (switch)  |     | node      |
 +-----+-----+     +-----+-----+
       |                  |
  leafkind-switch bridge
       |
+------+----------------------------------------------+
| Kind node (host namespace)                          |
|                                                     |
|  toswitch ──[eBPF nic_ingress]                      |
|      ↕                                              |
|  ul-host ──[eBPF ul_host_ingress]                   |
|      |  (veth pair)                                 |
+------+----------------------------------------------+
       |
+------+----------------------------------------------+
| Router pod namespace                                |
|                                                     |
|  ul-pe (same MAC/IP as toswitch)                    |
|      |                                              |
|  FRR (BGP/EVPN) ── vni110 (VXLAN) ── br-pe-110     |
|                                        |            |
+----------------------------------------+------------+
                                         |
                                      pe-110 (veth)
                                         |
                                      host-110
                                         |
                                      br-hs-110
                                         |
                                      pod (macvlan)
```

## eBPF Programs

### `nic_ingress` (attached to physical NIC, TC ingress)

Steers inbound traffic from the wire to the router pod:

- **ARP**: All ARP packets are cloned to `ul-host` so the router pod can
  resolve MACs for both BGP neighbors and remote VXLAN VTEPs. The host also
  sees the ARP (clone, not redirect).
- **TCP BGP (port 179)**: Packets from neighbor IPs are redirected to `ul-host`.
- **UDP BFD (ports 3784/4784)**: Packets from neighbor IPs are redirected.
- **UDP VXLAN (port 4789)**: The VNI is parsed from the VXLAN header and looked
  up in `vni_map`. Matching packets are redirected to `ul-host`.
- **Everything else**: Passed to the host kernel normally.

### `ul_host_ingress` (attached to ul-host veth, TC ingress)

Forwards all outbound traffic from the router pod to the physical NIC. Every
packet arriving at `ul-host` from `ul-pe` is unconditionally redirected to the
NIC's egress path.

## BPF Maps

| Map             | Type  | Key           | Value   | Purpose                                   |
|-----------------|-------|---------------|---------|-------------------------------------------|
| `config_map`    | Array | `0`           | ifindex | Redirect target (ul-host or NIC ifindex)  |
| `neighbor_map`  | Hash  | IPv4 (4B BE)  | flag    | BGP/BFD neighbor IPs to steer             |
| `vni_map`       | Hash  | VNI (u32 HE)  | flag    | VXLAN VNIs to steer to router pod         |

## Underlay CR Configuration

```yaml
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: Underlay
metadata:
  name: underlay
  namespace: openperouter-system
spec:
  asn: 64514
  nics:
    - toswitch
  nicMode: shared          # keeps NIC in host namespace
  neighbors:
    - asn: 64512
      address: "192.168.11.2"
  evpn:
    vtepInterface: ul-pe   # VXLAN sources from NIC's IP via ul-pe
```

Key fields:

- **`nicMode: shared`**: Enables the eBPF shared NIC mode instead of moving the
  NIC into the router pod namespace.
- **`vtepInterface: ul-pe`**: VXLAN uses `ul-pe`'s IP (copied from the NIC) as
  the VTEP source address. This ensures VXLAN traffic egresses with the same
  IP/MAC as the original NIC.

## Kernel Compatibility

The eBPF programs use TC classifier (`sched_cls`) program type with
`bpf_redirect` and `bpf_clone_redirect` helpers, which are available on all
supported kernels. The attachment method is chosen automatically based on kernel
support:

| Kernel | Attachment | Platforms |
|--------|-----------|-----------|
| >= 6.6 | **TCX** (preferred) | Fedora 39+, upstream kernels, RHEL 10 |
| 5.14+ | **TC cls_bpf** (fallback) | RHEL 9, all OpenShift 4.x releases |

### OpenShift / RHEL Kernel Versions

All current OpenShift releases use RHEL 9 with kernel 5.14, so the legacy TC
cls_bpf fallback is the path used in production:

| OCP Version | RHEL Base | Kernel | Attachment |
|-------------|-----------|--------|------------|
| 4.14–4.15 | RHEL 9.2 EUS | 5.14.0-284.x | TC cls_bpf |
| 4.16–4.18 | RHEL 9.4 EUS | 5.14.0-427.x | TC cls_bpf |
| 4.19–4.20 | RHEL 9.6 EUS | 5.14.0-xxx | TC cls_bpf |
| 4.21+ (expected) | RHEL 10 | 6.12.0 | TCX |

The manager tries TCX first and falls back to TC cls_bpf automatically. A log
message is emitted when the fallback is used:

```
TCX attach failed, falling back to legacy TC cls_bpf
```

## Implementation Details

### Idempotent BPF Management

The BPF programs are managed by a singleton `bpf.Manager`. The setup is
idempotent:

- On first reconcile: programs are loaded, attached (via TCX or TC cls_bpf),
  and maps populated.
- On subsequent reconciles: if the NIC and ul-host ifindexes haven't changed,
  the existing attachment is reused. Only the neighbor and VNI maps are updated.
- If ifindexes change (e.g. after a router pod restart recreates the veth pair):
  the old manager is closed and a new one is created with the correct ifindexes.

### Traffic Flow

**BGP establishment:**
1. FRR sends TCP SYN to neighbor via `ul-pe`
2. `ul-pe` → `ul-host` → `ul_host_ingress` BPF → redirect to NIC → wire
3. Response arrives on NIC → `nic_ingress` BPF detects BGP port + neighbor IP → redirect to `ul-host` → `ul-pe` → FRR

**VXLAN data path:**
1. Pod sends packet → macvlan → `br-hs-110` → `host-110` → `pe-110` → `br-pe-110` → `vni110` VXLAN encap
2. Encapsulated packet routed via `ul-pe` → `ul-host` → BPF → NIC → wire
3. Remote VXLAN arrives on NIC → BPF parses VNI, matches `vni_map` → redirect to `ul-host` → `ul-pe` → VXLAN decap → bridge → pod

## E2E Test

The test is in `e2etests/tests/evpn_l2_shared_nic.go` and can be run with:

```bash
make e2etests GINKGO_ARGS="--label-filter='shared-nic'"
```

It validates L2 connectivity over VXLAN between pods on different nodes, as well
as north-south traffic to external hosts through the EVPN fabric.

## Debugging with Retis

[Retis](https://github.com/retis-org/retis) is an eBPF-based packet tracing
tool that can trace packets through the Linux networking stack. It is very
useful for debugging the shared NIC eBPF data path since it can show exactly
where packets hit TC programs, where they get redirected, and where they are
dropped.

### Quick Start

In one terminal, start collecting traces:

```bash
make retis-collect
```

In another terminal, run the shared NIC test (or create resources manually):

```bash
make e2etests GINKGO_ARGS="--label-filter='shared-nic'"
```

Stop collection with `Ctrl-C`, then inspect:

```bash
# View sorted events
make retis-inspect

# Convert to pcap for Wireshark
make retis-pcap
```

### What Retis Shows

The default collection probes are:

- **`tc:tc_classify`** — Shows every packet hitting a TC/TCX program. You can
  see packets arriving at `toswitch` (NIC ingress), being processed by
  `nic_ingress`, and the redirect verdict. Similarly for `ul-host` and
  `ul_host_ingress`.
- **`skb-drop:kfree_skb`** — Shows packets being dropped anywhere in the
  kernel, with the drop reason. Useful for diagnosing issues like checksum
  errors, routing failures, or MTU problems after BPF redirect.

### Example: Tracing a BGP Session

```bash
# Collect with a timeout
make retis-collect RETIS_ARGS="--timeout 30"

# While collecting, create the underlay
kubectl apply -f config/samples/underlay-shared-nic.yaml

# After collection, inspect
make retis-inspect
```

You should see events like:

```
 [tp] tc:tc_classify dev toswitch dir ingress prog nic_ingress verdict redirect ...
 [tp] tc:tc_classify dev ul-host  dir ingress prog ul_host_ingress verdict redirect ...
```

If packets are dropped instead of redirected, the `skb-drop` events will show
why (e.g. `reason: SKB_DROP_REASON_NOT_SPECIFIED`).

### Custom Filters

For more targeted tracing, run retis directly:

```bash
# Trace only VXLAN traffic
CONTAINER_ENGINE=docker hack/retis-collect.sh --filter 'udp.dst == 4789'

# Trace only BGP traffic
CONTAINER_ENGINE=docker hack/retis-collect.sh --filter 'tcp.dst == 179 or tcp.src == 179'
```
