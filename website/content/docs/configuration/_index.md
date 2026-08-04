---
weight: 40
title: "Configuration"
description: "How to configure OpenPERouter"
icon: "article"
date: "2025-06-15T15:03:22+02:00"
lastmod: "2025-06-15T15:03:22+02:00"
toc: true
---

OpenPERouter requires two main configuration components: the **Underlay**
configuration for external router connectivity and a VPN specific
configuration for overlays.

OpenPERouter supports two overlay technologies:

- **EVPN/VXLAN**: See the
  [EVPN Configuration]({{< ref "evpn" >}}) page for details.
- **SRv6 L3VPN**: See the
  [SRv6 L3VPN Configuration]({{< ref "srv6" >}}) page for details.

All Custom Resources (CRs) must be created in the same namespace where
OpenPERouter is deployed (typically `openperouter-system`).

## Underlay Configuration

The underlay configuration establishes BGP sessions with external routers
(typically Top-of-Rack switches).

### Basic Underlay Configuration

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
```

For the full list of configuration fields, see the
[API Reference]({{< ref "api-reference.md#underlay" >}}) documentation.

### Multiple Interfaces and Neighbors

OpenPERouter supports configuring multiple physical network interfaces and
multiple BGP neighbors for production deployments with redundancy and
multi-path networking.

**Example with multiple neighbors and interfaces**:

```yaml
apiVersion: network.openperouter.io/v1alpha1
kind: Underlay
metadata:
  name: underlay
  namespace: openperouter-system
spec:
  asn: 64514
  
  # Multiple interfaces for redundancy
  interfaces:
    - type: NetworkDevice
      networkDevice:
        interfaceName: toswitch
    - type: NetworkDevice
      networkDevice:
        interfaceName: toswitch2
  
  # Multiple neighbors for dual-ToR setup
  neighbors:
    - asn: 64512
      address: 192.168.11.2
    - asn: 64512
      address: 192.168.11.3
    - asn: 64513
      address: 192.168.12.2
    - asn: 64513
      address: 192.168.12.3
```

**Validation requirements**:
- At least one neighbor must be configured
- At least one NIC must be configured
- Neighbor addresses must be unique
- NIC names must be unique
- Local ASN must differ from all neighbor ASNs

### BFD

Bidirectional Forwarding Detection can be enabled per neighbor through the
`bfd` field. An empty `bfd: {}` enables it with FRR's defaults; the
individual knobs are rendered as a BFD peer profile for that neighbor.

```yaml
  neighbors:
    - asn: 64512
      address: 192.168.11.2
      bfd:
        receiveInterval: 300     # ms, 10-60000
        transmitInterval: 300    # ms, 10-60000
        detectMultiplier: 3      # 2-255
        sessionMode: Passive     # Active (default) | Passive
        minimumTTL: 254          # 1-254, multi hop sessions only
```

`sessionMode` selects whether the local system initiates the session.
`Active`, the default when the field is omitted, starts it; `Passive`
waits for the peer to initiate before replying, per
[RFC 5880 section 6.1](https://datatracker.ietf.org/doc/html/rfc5880#section-6.1).

### Per-Node Configuration

The Underlay resource supports an optional `nodeSelector` field that
allows you to target specific configurations to specific nodes. This is
useful for multi-rack deployments, multi-datacenter clusters, or
heterogeneous hardware environments.

For detailed information and examples, see the
[Node Selector Configuration]({{< ref "node-selector.md" >}})
documentation.

### CNI-Provisioned Interfaces

Instead of moving an existing host network device into the router network
namespace, an underlay interface can be provisioned by a CNI plugin. This
allows sharing a physical NIC between the host and the router (e.g. via
`macvlan` or `ipvlan`) and delegating IP address management to the plugin.

To use a CNI-provisioned interface, set the interface `type` to `CNIDevice` and
embed the CNI configuration (a conflist JSON, CNI spec >= 1.0.0) in the
`rawConfig` field:

```yaml
apiVersion: network.openperouter.io/v1alpha1
kind: Underlay
metadata:
  name: underlay
  namespace: openperouter-system
spec:
  asn: 64514
  interfaces:
    - type: CNIDevice
      cniDevice:
        type: RawConfig
        interfaceName: net1
        rawConfig:
          cniVersion: "1.0.0"
          name: macvlan-underlay
          plugins:
            - type: macvlan
              master: toswitch
              mode: bridge
              ipam:
                type: static
                addresses:
                  - address: 192.168.11.10/24
  neighbors:
    - asn: 64512
      address: 192.168.11.2
```

The controller invokes the plugin with `CNI_IFNAME` set to
`interfaceName` (defaults to `net1`) and the router network namespace as
the target. The plugin binaries are looked up in the directories passed
via the controller's `--cni-plugin-dirs` flag; a set of reference plugins is
bundled in the controller image.

#### DHCP IPAM

To use DHCP instead of static addressing, set `ipam.type` to `dhcp`.
The controller automatically starts and supervises the DHCP daemon when
it detects a CNI config with DHCP IPAM — no additional configuration is
needed.

The controller supervises the DHCP daemon as a child process — starting
it on demand, restarting it automatically on exit, and triggering lease
re-acquisition for all DHCP-backed underlay interfaces after each daemon
restart.

```yaml
apiVersion: network.openperouter.io/v1alpha1
kind: Underlay
metadata:
  name: underlay
  namespace: openperouter-system
spec:
  asn: 64514
  interfaces:
    - type: CNIDevice
      cniDevice:
        type: RawConfig
        interfaceName: net1
        rawConfig:
          cniVersion: "1.0.0"
          name: macvlan-underlay
          plugins:
            - type: macvlan
              master: toswitch
              mode: bridge
              ipam:
                type: dhcp
        runtimeConfig:
          mac: "aa:bb:cc:00:00:01"
  neighbors:
    - asn: 64512
      address: 192.168.11.2
```

MAC pinning via `runtimeConfig` (requires the macvlan plugin to declare
`capabilities: {"mac": true}`) ensures the interface keeps the same MAC
across netns rebuilds, which helps DHCP servers with MAC-based
reservations assign a stable IP address.

Key behaviors to be aware of:

- **Interface types cannot be mixed**: all the entries of `interfaces`
  must be of the same type, either `NetworkDevice` or `CNIDevice`.
- **IPAM is delegated to the plugin**: use the plugin's `ipam` block
  (e.g. `static` or `dhcp`) to assign the interface address.
- **`rawConfig` is immutable**: to change the CNI configuration, delete
  and recreate the Underlay. This is enforced by the validation webhook,
  as reconciling a config change in place would require a teardown/re-add
  cycle with partial-failure states. Configuration paths that bypass the
  webhook (e.g. static file configuration in host mode) are enforced at
  reconcile time instead: the controller compares the desired
  configuration against the one recorded when the interface was
  provisioned, keeps the existing interface untouched and fails the
  reconcile if they differ.
- **`runtimeConfig`** can pass CNI capability arguments (e.g. `ips`,
  `mac`, `bandwidth`) to plugins that declare the corresponding
  `capabilities` in their config; undeclared keys are ignored. Like
  `rawConfig`, it is immutable once the Underlay is created.
- **Drift is detected and repaired on the next reconcile**: the controller
  runs a CNI CHECK against the cached attachment before trusting it. If the
  interface was removed or misconfigured outside of OpenPERouter, the next
  reconcile (triggered by a resource change, or a controller/router
  restart) tears it down and re-provisions it.
- Since the interface address is typically node-specific, CNI underlays
  are usually node-scoped via `nodeSelector`, one Underlay per node. See
  the [example on GitHub](https://github.com/openperouter/openperouter/tree/main/examples/evpn/cni-underlay).

### Route Reflector

A node can act as a BGP route reflector (RFC 4456) to reflect underlay and EVPN routes between its configured route reflector clients — the neighbors accepted via `listenRange` that carry the per-address-family `routeReflectorClient` property — removing the need for a full iBGP mesh between them.

For detailed information and examples, see the [Route Reflector]({{< ref "route-reflector.md" >}}) documentation.

## Sysctl Configuration

OpenPERouter automatically tunes several kernel sysctl settings inside the
router's network namespace (IP forwarding, ARP accept, IPv6 Neighbor
Advertisement accept). Some of these settings require a minimum kernel
version.

For the full list of sysctls and kernel requirements, see the
[Sysctl Configuration]({{< ref "sysctl.md" >}}) documentation.
