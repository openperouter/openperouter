---
weight: 50
title: "Passthrough Examples"
description: "Integration examples with BGP-speaking components"
icon: "article"
date: "2025-06-15T15:03:22+02:00"
lastmod: "2025-06-15T15:03:22+02:00"
toc: true
---

## Prerequisites

All examples in this section assume you have:

- OpenPERouter installed and configured (see [Installation]({{< ref "installation" >}}))
- A [development environment]({{< ref "../../contributing/devenv.md" >}}) with two L3 VNIs available from the fabric
- Basic understanding of BGP and EVPN concepts

## Development Environment Setup

The examples use a development environment with the following topology:

![](/images/openpedevenv.svg)

This environment provides:

- Leaf switches (leafA and leafB) with BGP peering
- A kind cluster for testing OpenPERouter integration
- A host connected to the default vrf of leafA

## Base OpenPERouter Configuration

Before running any integration examples, you need to configure OpenPERouter with the appropriate underlay settings.

### Underlay Configuration

Configure the underlay to peer with the `kind-leaf` node:

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

**Configuration Details:**

- **ASN**: 64514 (OpenPERouter's ASN)
- **Interface**: `toswitch` (network interface to the fabric)
- **Neighbor**: 192.168.11.2 with ASN 64512 (kind-leaf node)

### Passthrough Configuration

Create one passthrough configuration:

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


## Available Examples

### MetalLB Integration

Learn how to integrate OpenPERouter with MetalLB to advertise LoadBalancer services across the EVPN fabric.

**Key Features:**

- LoadBalancer service advertisement
- BGP route generation
- Cross-fabric service reachability

[View MetalLB Integration Example →]({{< ref "metallb" >}})


