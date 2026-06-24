---
weight: 40
title: "EVPN Configuration"
description: "How to configure OpenPERouter"
icon: "article"
date: "2025-06-15T15:03:22+02:00"
lastmod: "2025-06-15T15:03:22+02:00"
toc: true
---

## Underlay Configuration

In addition to the configuration described in the [underlay configuration section]({{< ref "configuration/#underlay-configuration" >}}), the VTEP (Virtual Tunnel End Point) source must be configured via the `evpn.vtepCIDR` field.

```yaml
apiVersion: network.openperouter.io/v1alpha1
kind: Underlay
metadata:
  name: underlay
  namespace: openperouter-system
spec:
  asn: 64514
  tunnelEndpoint:
    cidrs:
    - 100.65.0.0/24
  interfaces:
    - type: NetworkDevice
      networkDevice:
        interfaceName: toswitch
  neighbors:
    - asn: 64512
      address: 192.168.11.2
```

The `tunnelEndpoint.cidrs` field defines the IP range used for VTEP addresses. OpenPERouter automatically assigns a unique VTEP IP to each node from this range. At least one CIDR (IPv4 or IPv6) is required, and both may be specified for dual-stack operation. For example, with `100.65.0.0/24`:

- Node 1: `100.65.0.1`
- Node 2: `100.65.0.2`
- Node 3: `100.65.0.3`
- etc.

A loopback interface is created inside the router namespace with the allocated IP, and OpenPERouter advertises the VTEP IP to the fabric over the BGP underlay session.

#### IPv6 and Dual-Stack VTEP

IPv6-only or dual-stack (IPv4 + IPv6) VTEP configurations are supported:

```yaml
  tunnelEndpoint:
    cidrs:
    - 100.65.0.0/24
    - fd00:64::/120
```

When both IPv4 and IPv6 CIDRs are specified, individual VNIs can select which address family to use via the `underlayAddressFamily` field on the L3VNI or L2VNI resource. When omitted, it defaults to the available family (IPv4 preferred in dual-stack).

### Configuration Fields

| Field | Type | Description | Required |
|-------|------|-------------|----------|
| `asn` | integer | Local ASN for BGP sessions | Yes |
| `evpn.vtepCIDR` | string | CIDR block for VTEP IP allocation | Yes |
| `interfaces` | array | List of underlay interfaces to use for connectivity. Each entry is a discriminated union; today only the `NetworkDevice` type is supported, which moves an existing host network device into the router namespace | Yes |
| `neighbors` | array | List of BGP neighbors to peer with | Yes |
| `nodeSelector` | object | Label selector to target specific nodes (applies to all nodes if omitted) | No |
| `gracefulRestart` | object | Enables BGP Graceful Restart when present. See [Graceful Restart]({{< ref "graceful-restart" >}}). | No |

## L3 VNI Configuration

L3 VNI (Virtual Network Identifier) configurations define EVPN L3 overlays. Each L3VNI creates a separate routing domain and BGP session with the host.

### Basic L3VNI Configuration

```yaml
apiVersion: network.openperouter.io/v1alpha1
kind: L3VNI
metadata:
  name: blue
  namespace: openperouter-system
spec:
  vrf: blue
  hostsession:
    asn: 64514
    hostasn: 64515
    localcidr:
      ipv4: 192.169.11.0/24
  vni: 200

```

### Configuration Fields

| Field | Type | Description | Required |
|-------|------|-------------|----------|
| `vrf` | string | Name of the VRF (Virtual Routing and Forwarding) instance | Yes |
| `vni` | integer | Virtual Network Identifier (1-16777215) | Yes |
| `underlayAddressFamily` | string | VTEP address family for this VNI (`ipv4` or `ipv6`). Defaults to available family (IPv4 preferred in dual-stack). | No |
| `hostsession.asn` | integer | Router ASN for BGP session with host | Yes |
| `hostsession.hostasn` | integer | Host ASN for BGP session | Yes |
| `hostsession.localcidr` | string | CIDR for veth pair IP allocation | Yes |
| `nodeSelector` | object | Label selector to target specific nodes (applies to all nodes if omitted) | No |

### Multiple VNIs Example

You can create multiple VNIs for different network segments:

```yaml
# Production VNI
apiVersion: network.openperouter.io/v1alpha1
kind: L3VNI
metadata:
  name: signal
  namespace: openperouter-system
spec:
  vrf: signal
  vni: 100
  hostsession:
    asn: 64514
    hostasn: 64515
    localcidr:
      ipv4: 192.168.10.0/24
---
# Development VNI
apiVersion: network.openperouter.io/v1alpha1
kind: L3VNI
metadata:
  name: oam
  namespace: openperouter-system
spec:
  vrf: oam
  vni: 200
  hostsession:
    asn: 64514
    hostasn: 64515
    localcidr:
      ipv4: 192.168.20.0/24
```

## What Happens During Reconciliation

When you create or update VNI configurations, OpenPERouter automatically:

1. **Creates Network Interfaces**: Sets up VXLAN interface and Linux VRF named after the VNI
2. **Establishes Connectivity**: Creates veth pair and moves one end to the router's namespace
3. **Adjusts Veth MTU**: Sets the MTU on both veth legs to the underlay NIC's MTU minus 50 bytes to account for VXLan encapsulation overhead
4. **Assigns IP Addresses**: Allocates IPs from the `localcidr` range:
   - Router side: First IP in the CIDR (e.g., `192.169.11.1`)
   - Host side: Each node gets a free IP in the CIDR, starting from the second (e.g., `192.169.11.15`)
5. **Creates BGP Session**: Opens BGP session between router and host using the specified ASNs

## L2VNI Configuration

L2VNIs provide Layer 2 connectivity across nodes using EVPN tunnels. Unlike L3VNIs, L2VNIs extend Layer 2 domains rather than routing domains.

### Configuration Fields

| Field | Type | Description | Required |
|-------|------|-------------|----------|
| `vni` | integer | Virtual Network Identifier for the EVPN tunnel | Yes |
| `vrf` | string | Name of the VRF to associate with this L2VNI | Yes |
| `underlayAddressFamily` | string | VTEP address family for this VNI (`ipv4` or `ipv6`). Defaults to available family (IPv4 preferred in dual-stack). | No |
| `hostmaster.type` | string | Type of host interface management (`linux-bridge` or `ovs-bridge`) | Yes |
| `hostmaster.linuxBridge.autoCreate` | boolean | Whether to automatically create a Linux bridge | No |
| `hostmaster.linuxBridge.name` | string | Name of the Linux bridge to attach to (if not auto-creating) | No |
| `hostmaster.ovsBridge.autoCreate` | boolean | Whether to automatically create an OVS bridge | No |
| `hostmaster.ovsBridge.name` | string | Name of the OVS bridge to attach to (if not auto-creating) | No |
| `nodeSelector` | object | Label selector to target specific nodes (applies to all nodes if omitted) | No |

### L2VNI Example

```yaml
apiVersion: network.openperouter.io/v1alpha1
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

## What Happens During Reconciliation

When you create or update VNI configurations, OpenPERouter automatically:

1. **Creates Network Interfaces**: Sets up VXLAN interface and Linux VRF named after the VNI
2. **Establishes Connectivity**: Creates veth pair and moves one end to the router's namespace
3. **Adjusts Veth MTU**: Sets the MTU on both veth legs to the underlay NIC's MTU minus 50 bytes to account for VXLan encapsulation overhead
4. **Enslaves the veth**: the veth is connected to the bridge corresponding to the l2 domain
5. **Optionally creates a bridge on the host**: if hostmaster.autocreate is set to `true`
6. **Optionally connects the host veth to the bridge on the host**: if hostmaster.autocreate is set to `true` or name
is set

## Per-Node Configuration

All EVPN resources (Underlay with EVPN, L3VNI, and L2VNI) support the optional `nodeSelector` field, which allows you to target specific configurations to specific nodes. This is useful for:

- Multi-rack deployments with different VNIs per rack
- Multi-datacenter clusters with zone-specific configurations
- Selective deployment to worker nodes only
- Hardware-specific configurations

For detailed information and examples, see the [Node Selector Configuration]({{< ref "node-selector.md" >}}) documentation.

## API Reference

For detailed information about all available configuration fields, validation rules, and API specifications, see the [API Reference]({{< ref "api-reference.md" >}}) documentation.
