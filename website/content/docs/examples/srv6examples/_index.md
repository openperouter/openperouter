---
weight: 53
title: "SRv6 L3VPN Examples"
description: "Integration examples with BGP-speaking components using SRv6 L3VPN"
icon: "article"
date: "2025-06-15T15:03:22+02:00"
lastmod: "2025-06-15T15:03:22+02:00"
toc: true
---

This section provides practical examples of integrating OpenPERouter with
various BGP-speaking components commonly used in Kubernetes environments,
using SRv6 as the data plane.

## Overview

OpenPERouter behaves exactly like a physical Provider Edge (PE) router,
enabling seamless integration with any BGP-speaking component. When using
SRv6 L3VPN, traffic is encapsulated using SRv6 instead of VXLAN, and the
control plane uses BGP VPNv4/VPNv6 with SRv6 SIDs instead of EVPN.

## Prerequisites

All examples in this section assume you have:

- OpenPERouter installed and configured
  (see [Installation]({{< ref "installation" >}}))
- A [development environment]({{< ref "../../contributing/devenv.md" >}})
  with SRv6-capable leaves
- Basic understanding of BGP, SRv6, and IS-IS concepts

## Development Environment Setup

The examples use a development environment with the following topology:

This environment provides:

- SRv6-capable leaf switches with IS-IS and BGP peering
- A kind cluster for testing OpenPERouter integration

## Base OpenPERouter Configuration

Before running any integration examples, you need to configure
OpenPERouter with the appropriate underlay, SRv6, and L3VPN settings.

### Underlay Configuration

Configure the underlay with IS-IS, SRv6 locator, and eBGP multihop
neighbors:

```yaml
apiVersion: network.openperouter.io/v1alpha1
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

**Configuration Details:**

- **ASN**: 64514 (OpenPERouter's ASN)
- **Neighbors**: eBGP multihop peers at `2001:db8:1234::1` and
  `2001:db8:1234::2` (ASN 64520)
- **Interface**: `toswitch1` (network interface to the fabric)
- **Router ID CIDR**: `10.0.0.0/24` (router ID allocation range,
  incremented by the node index for each node)
- **Tunnel Endpoint CIDR**: `2001:db8:1234:5678::/64` (SRv6 tunnel
  endpoint allocation, incremented by the node index for each node)
- **IS-IS**: Level 1, with passive-only advertisement
- **SRv6 Locator**: `fd00:0:32::/48` with uSID format `usid-f3216`
  (incremented by the node index for each node)

### L3VPN Configurations

Create two L3VPNs with explicit route distinguishers and route targets:

```yaml
apiVersion: network.openperouter.io/v1alpha1
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
---
apiVersion: network.openperouter.io/v1alpha1
kind: L3VPN
metadata:
  name: blue
  namespace: openperouter-system
spec:
  hostsession:
    asn: 64514
    hostasn: 64515
    localcidr:
      ipv4: 192.169.11.0/24
  vrf: blue
  rdAssignedNumber: 200
  exportRTs:
  - "64514:200"
  importRTs:
  - "64520:200"
```

**L3VPN Details:**

- **Red L3VPN**: VRF `red`, RD assigned number 100, export RT `64514:100`,
  import RT `64520:100`
- **Blue L3VPN**: VRF `blue`, RD assigned number 200, export RT
  `64514:200`, import RT `64520:200`
- **Host ASN**: 64515 (for BGP sessions with host components)

## Available Examples

### MetalLB Integration

Learn how to integrate OpenPERouter with MetalLB to advertise LoadBalancer
services over SRv6 L3VPN.

**Key Features:**

- LoadBalancer service advertisement
- SRv6-encapsulated route generation
- Cross-fabric service reachability via SRv6

[View MetalLB Integration Example →]({{< ref "metallb" >}})

### Layer2 Integration

Configure OpenPERouter for Layer 2 scenarios with SRv6 as the underlay.

**Key Features:**

- Layer 2 VNI over SRv6 underlay
- Direct pod-to-pod connectivity
- Pod-to-external L3 connectivity

[View Layer2 Integration Example →]({{< ref "layer2" >}})
