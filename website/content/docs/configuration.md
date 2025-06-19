---
weight: 40
title: "Configuration"
description: "How to configure OpenPERouter"
icon: "article"
date: "2025-06-15T15:03:22+02:00"
lastmod: "2025-06-15T15:03:22+02:00"
toc: true
---

OpenPERouter requires two main configuration components: the **Underlay** configuration for external router connectivity and **VNI** configurations for EVPN overlays.

All Custom Resources (CRs) must be created in the same namespace where OpenPERouter is deployed (typically `openperouter-system`).

## Underlay Configuration

The underlay configuration establishes BGP sessions with external routers (typically Top-of-Rack switches) and defines the VTEP IP allocation strategy.

### Basic Underlay Configuration

```yaml
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: Underlay
metadata:
  name: underlay
  namespace: openperouter-system
spec:
  asn: 64514
  vtepcidr: 100.65.0.0/24
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
| `vtepcidr` | string | CIDR block for VTEP IP allocation | Yes |
| `nics` | array | List of network interface names to move to router namespace | Yes |
| `neighbors` | array | List of BGP neighbors to peer with | Yes |

### Multiple Neighbors Example

You can configure multiple BGP neighbors for redundancy:

```yaml
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: Underlay
metadata:
  name: underlay
  namespace: openperouter-system
spec:
  asn: 64514
  vtepcidr: 100.65.0.0/24
  nics:
    - toswitch1
    - toswitch2
  neighbors:
    - asn: 64512
      address: 192.168.11.2
    - asn: 64512
      address: 192.168.12.2
```

### VTEP IP Allocation

The `vtepcidr` field defines the IP range used for VTEP (Virtual Tunnel End Point) addresses. OpenPERouter automatically assigns a unique VTEP IP to each node from this range. For example, with `100.65.0.0/24`:

- Node 1: `100.65.0.1`
- Node 2: `100.65.0.2`
- Node 3: `100.65.0.3`
- etc.

## VNI Configuration

VNI (Virtual Network Identifier) configurations define EVPN overlays. Each VNI creates a separate routing domain and BGP session with the host.

### Basic VNI Configuration

```yaml
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: VNI
metadata:
  name: blue
  namespace: openperouter-system
spec:
  asn: 64514
  vni: 200
  localcidr: 192.169.11.0/24
  hostasn: 64515
```

### Configuration Fields

| Field | Type | Description | Required |
|-------|------|-------------|----------|
| `asn` | integer | Router ASN for BGP session with host | Yes |
| `vni` | integer | Virtual Network Identifier (1-16777215) | Yes |
| `localcidr` | string | CIDR for veth pair IP allocation | Yes |
| `hostasn` | integer | Host ASN for BGP session | Yes |

### Multiple VNIs Example

You can create multiple VNIs for different network segments:

```yaml
# Production VNI
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: VNI
metadata:
  name: signal
  namespace: openperouter-system
spec:
  asn: 64514
  vni: 100
  localcidr: 192.168.10.0/24
  hostasn: 64515
---
# Development VNI
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: VNI
metadata:
  name: oam
  namespace: openperouter-system
spec:
  asn: 64514
  vni: 200
  localcidr: 192.168.20.0/24
  hostasn: 64515
```

## What Happens During Reconciliation

When you create or update VNI configurations, OpenPERouter automatically:

1. **Creates Network Interfaces**: Sets up VXLAN interface and Linux VRF named after the VNI
2. **Establishes Connectivity**: Creates veth pair and moves one end to the router's namespace
3. **Assigns IP Addresses**: Allocates IPs from the `localcidr` range:
   - Router side: First IP in the CIDR (e.g., `192.169.11.1`)
   - Host side: Second IP in the CIDR (e.g., `192.169.11.2`)
4. **Establishes BGP Session**: Opens BGP session between router and host using the specified ASNs

## API Reference

For detailed information about all available configuration fields, validation rules, and API specifications, see the [API Reference](../api-reference.md) documentation.
