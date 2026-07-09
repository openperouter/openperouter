---
weight: 41
title: "SRv6 L3VPN Configuration"
description: "How to configure OpenPERouter for SRv6 L3VPN"
icon: "article"
date: "2025-06-15T15:03:22+02:00"
lastmod: "2025-06-15T15:03:22+02:00"
toc: true
---

## Underlay Configuration

In addition to the configuration described in the
[underlay configuration section]({{< ref "configuration/#underlay-configuration" >}}),
the SRv6 underlay requires IS-IS, an SRv6 locator, and at least one IPv6
tunnel endpoint CIDR.

```yaml
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: Underlay
metadata:
  name: underlay
  namespace: openperouter-system
spec:
  asn: 64514
  neighbors:
  - address: 2001:db8:1234::1
    asn: 64520
    ebgpMultiHop: true
  - address: 2001:db8:1234::2
    asn: 64520
    ebgpMultiHop: true
  interfaces:
    - type: NetworkDevice
      networkDevice:
        interfaceName: toswitch1
  routeridcidr: 10.0.0.0/24
  tunnelEndpoint:
    cidrs:
    - 2001:db8:1234:5678::/64
  isis:
    baseNet: "49.0001.0002.0003.0004.00"
    level: 1
    features:
    - "advertisePassiveOnly"
  srv6:
    locator:
      basePrefix: "fd00:0:32::/48"
      format: "usid-f3216"
```

### Tunnel Endpoint

The `tunnelEndpoint.cidrs` field must include at least one IPv6 CIDR when
using SRv6. OpenPERouter automatically assigns a unique tunnel endpoint IP
to each node from this range.

Both IPv4 and IPv6 CIDRs may be specified for dual-stack operation (e.g.,
when combining SRv6 with VXLAN for L2VNIs):

```yaml
  tunnelEndpoint:
    cidrs:
    - 100.65.0.0/24
    - 2001:db8:1234:5678::/64
```

### Router ID

The `routeridcidr` field defines the IPv4 CIDR used for per-node router ID
assignment. Each node receives a unique router ID from this range. Defaults
to `10.0.0.0/24` if omitted.

### IS-IS Configuration

IS-IS is required when SRv6 is enabled. It provides the IGP underlay for
SRv6 reachability.

```yaml
  isis:
    baseNet: "49.0001.0002.0003.0004.00"
    level: 1
    features:
    - "advertisePassiveOnly"
```

IS-IS with IPv6 is automatically enabled for all interfaces listed in the
`interfaces` field of the underlay configuration.

For the full list of IS-IS configuration fields, see the
[ISISConfig API Reference]({{< ref "api-reference#isisconfig" >}}).

### SRv6 Configuration

The `srv6` section configures the SRv6 locator. SRv6 can only be enabled
when IS-IS is also configured.

```yaml
  srv6:
    locator:
      basePrefix: "fd00:0:32::/48"
      format: "usid-f3216"
```

The `usid-f3216` format uses a block length of 32 bits and a node length
of 16 bits with uSID behavior.

For the full list of SRv6 configuration fields, see the
[SRV6Config API Reference]({{< ref "api-reference#srv6config" >}}).

### Neighbor Configuration

In this example, we configure eBGP multihop neighbors:

```yaml
  neighbors:
  - address: 2001:db8:1234::1
    asn: 64520
    ebgpMultiHop: true
```

The `ebgpMultiHop` field indicates that the BGP peer is multiple hops
away. OpenPERouter uses a heuristic to enable the correct network layer
protocols for each neighbor unless `addressFamilies` is explicitly set,
which overrides the automatic selection.

For the full list of underlay configuration fields, see the
[UnderlaySpec API Reference]({{< ref "api-reference#underlayspec" >}}).
For neighbor configuration, see the
[Neighbor API Reference]({{< ref "api-reference#neighbor" >}}).

## L3VPN Configuration

L3VPN configurations define SRv6 IP VPN overlays. Each L3VPN creates a
separate routing domain with explicit route distinguishers and route
targets.

### Basic L3VPN Configuration

```yaml
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: L3VPN
metadata:
  name: red
  namespace: openperouter-system
spec:
  hostsession:
    asn: 64514
    hostasn: 64515
    localcidr:
      ipv4: 192.169.10.0/24
  vrf: red
  rdAssignedNumber: 100
  exportRTs:
  - "64514:100"
  importRTs:
  - "64520:100"
```

For the full list of L3VPN configuration fields, see the
[L3VPNSpec API Reference]({{< ref "api-reference#l3vpnspec" >}}).

### Multiple L3VPNs Example

You can create multiple L3VPNs for different network segments:

```yaml
# Signal VPN
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: L3VPN
metadata:
  name: signal
  namespace: openperouter-system
spec:
  vrf: signal
  rdAssignedNumber: 100
  importRTs:
  - "64520:100"
  exportRTs:
  - "64514:100"
  hostsession:
    asn: 64514
    hostasn: 64515
    localcidr:
      ipv4: 192.168.10.0/24
---
# OAM VPN
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: L3VPN
metadata:
  name: oam
  namespace: openperouter-system
spec:
  vrf: oam
  rdAssignedNumber: 200
  importRTs:
  - "64520:200"
  exportRTs:
  - "64514:200"
  hostsession:
    asn: 64514
    hostasn: 64515
    localcidr:
      ipv4: 192.168.20.0/24
```

## What Happens During Reconciliation

When you create or update L3VPN configurations, OpenPERouter automatically:

1. **Creates Network Interfaces**: Sets up the Linux VRF named after the
   L3VPN
2. **Establishes Connectivity**: Creates veth pair and moves one end to
   the router's namespace
3. **Assigns IP Addresses**: Allocates IPs from the `localcidr` range:
   - Router side: First IP in the CIDR (e.g., `192.169.10.1`)
   - Host side: Each node gets a free IP in the CIDR, starting from the
     second (e.g., `192.169.10.15`)
4. **Creates BGP Session**: Opens BGP session between router and host
   using the specified ASNs
5. **Configures SRv6**: Sets up the SRv6 locator and SID allocation for
   the VRF

## L2VNI Configuration

L2VNIs provide Layer 2 connectivity across nodes using EVPN as the control
plane and VXLAN tunnels as the data plane, contrary to L3VPNs which use
SRv6.

For the full list of L2VNI configuration fields, see the
[L2VNISpec API Reference]({{< ref "api-reference#l2vnispec" >}}).

### L2VNI Example

```yaml
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: L2VNI
metadata:
  name: l2red
  namespace: openperouter-system
spec:
  vni: 210
  vrf: red
  hostmaster:
    type: linux-bridge
    linuxBridge:
      autoCreate: true
```

## Validation Rules

- L3VPNs and L3VNIs **cannot coexist**. Use L3VPNs for
  SRv6 and L3VNIs for EVPN.
- L3VPNs **require** an Underlay with SRv6 configuration on every node
  where they are applied.
- SRv6 **requires** IS-IS to be configured on the Underlay.
- SRv6 **requires** at least one IPv6 CIDR in `tunnelEndpoint.cidrs`.
- L3VPN VRF names must be unique across all L3VPNs on a node.
- L3VPN `rdAssignedNumber` values must be unique across all L3VPNs.

## Per-Node Configuration

All SRv6 resources (Underlay with SRv6, L3VPN, and L2VNI) support the
optional `nodeSelector` field, which allows you to target specific
configurations to specific nodes. This is useful for:

- Multi-rack deployments with different L3VPNs per rack
- Multi-datacenter clusters with zone-specific configurations
- Selective deployment to worker nodes only
- Hardware-specific configurations

For detailed information and examples, see the
[Node Selector Configuration]({{< ref "node-selector.md" >}})
documentation.

## API Reference

For detailed information about all available configuration fields,
validation rules, and API specifications, see the
[API Reference]({{< ref "api-reference.md" >}}) documentation.
