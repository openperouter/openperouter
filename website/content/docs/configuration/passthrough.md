---
weight: 50
title: "Passthrough Configuration"
description: "How to configure OpenPERouter for L3 Passthrough"
icon: "article"
date: "2025-01-27T10:00:00+02:00"
lastmod: "2025-01-27T10:00:00+02:00"
toc: true
---

## Underlay Configuration

For passthrough mode, the underlay configuration is simpler than EVPN mode as it doesn't require VTEP IP allocation. The configuration focuses on establishing BGP sessions with external routers.

### Basic Underlay Configuration

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
  neighbors:
    - asn: 64512
      address: 192.168.11.2
```

### Configuration Fields

| Field | Type | Description | Required |
|-------|------|-------------|----------|
| `asn` | integer | Local ASN for BGP sessions | Yes |
| `nics` | array | List of network interface names to move to router namespace | Yes |
| `neighbors` | array | List of BGP neighbors to peer with | Yes |
| `nodeSelector` | object | Label selector to target specific nodes (applies to all nodes if omitted) | No |

**Note**: Unlike EVPN mode, passthrough mode does not require an `evpn` field in the underlay configuration since no VTEP IP allocation is needed.

## L3 Passthrough Configuration

L3 Passthrough configurations define direct BGP connectivity between the host and the fabric without encapsulation. Each L3Passthrough creates a BGP session with BGP-speaking components on the host.

### Basic L3Passthrough Configuration

```yaml
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

### Configuration Fields

| Field | Type | Description | Required |
|-------|------|-------------|----------|
| `hostsession.asn` | integer | Router ASN for BGP session with host | Yes |
| `hostsession.hostasn` | integer | Host ASN for BGP session | Yes |
| `hostsession.localcidr.ipv4` | string | IPv4 CIDR for veth pair IP allocation | No |
| `hostsession.localcidr.ipv6` | string | IPv6 CIDR for veth pair IP allocation | No |
| `nodeSelector` | object | Label selector to target specific nodes (applies to all nodes if omitted) | No |

### Dual Stack Configuration

You can configure both IPv4 and IPv6 for dual stack support:

```yaml
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: L3Passthrough
metadata:
  name: passthrough-dual
  namespace: openperouter-system
spec:
  hostsession:
    asn: 64514
    hostasn: 64515
    localcidr:
      ipv4: 192.169.10.0/24
      ipv6: 2001:db8:10::/64
```

### IP Allocation Strategy

The IP addresses for the veth pair are allocated from the configured `localcidr`:

- **Router side**: Always gets the first IP in the CIDR (e.g., `192.169.10.1`)
- **Host side**: Each node gets a different IP from the CIDR, starting from the second value (e.g., `192.169.10.2`)

This consistent allocation strategy ensures that BGP-speaking components on the host can always use the same router IP address for establishing BGP sessions.

## What Happens During Reconciliation

When you create or update L3Passthrough configurations, OpenPERouter automatically:

1. **Creates Veth Pair**: Sets up a veth pair named `pt-host` (host side) and `pt-ns` (router side)
2. **Assigns IP Addresses**: Allocates IPs from the `localcidr` range:
   - Router side: First IP in the CIDR (e.g., `192.169.10.1`)
   - Host side: Second IP in the CIDR (e.g., `192.169.10.2`)
3. **Establishes BGP Session**: Opens BGP session between router and host using the specified ASNs
4. **Configures Route Advertisement**: Sets up route advertisement for both IPv4 and IPv6 address families

## Per-Node Configuration

L3Passthrough resources support the optional `nodeSelector` field, which allows you to target specific configurations to specific nodes. This is useful for:

- Configuring passthrough only on edge nodes
- Security zone-based configurations
- Selective deployment based on node roles or capabilities

For detailed information and examples, see the [Node Selector Configuration]({{< ref "node-selector.md" >}}) documentation.

## API Reference

For detailed information about all available configuration fields, validation rules, and API specifications, see the [API Reference]({{< ref "api-reference.md" >}}) documentation.
