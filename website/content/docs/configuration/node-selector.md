---
weight: 45
title: "Running different configurations on different nodes"
description: "How to configure per-node resources using node selectors"
icon: "article"
date: "2026-01-13T10:00:00+02:00"
lastmod: "2026-01-13T10:00:00+02:00"
toc: true
---

## Overview

Node selectors enable you to target specific OpenPERouter configurations to specific nodes in your cluster. This allows you to support heterogeneous cluster topologies including multi-datacenter, multi-rack, and mixed-hardware environments.

All OpenPERouter Custom Resource Definitions (Underlay, L3VNI, L2VNI, and L3Passthrough) support the optional `nodeSelector` field.

### When to Use Node Selectors

Node selectors are useful in scenarios such as:

- **Multi-rack deployments**: Different nodes connect to different Top-of-Rack (ToR) switches
- **Multi-datacenter clusters**: Nodes in different availability zones need location-specific BGP configurations
- **Hardware heterogeneity**: Different server models with different NIC naming conventions
- **Per-rack VNI isolation**: Different racks need separate VNI configurations
- **Selective deployment**: Only specific nodes should have certain configurations

## Node Selector Syntax

The `nodeSelector` field uses Kubernetes label selectors (`metav1.LabelSelector`), supporting both `matchLabels` and `matchExpressions`:

For more information on label selectors, see the [Kubernetes documentation](https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/).

## Validation Rules

Different CRD types have different validation rules for node selectors:

### Underlay

**Only one Underlay can match a given node.** If multiple Underlay resources would match the same node, the controller will reject the configuration and update the status conditions with an error.

### L3VNI, L2VNI, and L3Passthrough

**Multiple instances can match the same node.** This enables multi-tenancy scenarios where different VNIs or passthrough configurations coexist on the same nodes.

However, the controller validates that configurations don't conflict:
- Two L2VNIs with the same VRF on one node will be rejected
- VNI number conflicts across resource types will be detected
- Other incompatible configurations will be validated

## Underlay Example

Different NIC naming across vendor hardware:

```yaml
# For Dell servers with specific NIC naming
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: Underlay
metadata:
  name: underlay-dell-hardware
  namespace: openperouter-system
spec:
  nodeSelector:
    matchLabels:
      hardware.vendor: dell
  asn: 64512
  evpn:
    vtepcidr: 100.65.0.0/24
  nics:
    - eno1
    - eno2
  neighbors:
    - asn: 64500
      address: 192.168.10.1
---
# For HP servers with different NIC naming
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: Underlay
metadata:
  name: underlay-hp-hardware
  namespace: openperouter-system
spec:
  nodeSelector:
    matchLabels:
      hardware.vendor: hp
  asn: 64512
  evpn:
    vtepcidr: 100.65.0.0/24
  nics:
    - em1
    - em2
  neighbors:
    - asn: 64500
      address: 192.168.10.1
```

## L3VNI Examples

### Per-Rack VNI Configuration

Different racks use different L3VNIs for network segmentation:

```yaml
# L3VNI for rack-1 nodes
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: L3VNI
metadata:
  name: tenant-a-rack-1
  namespace: openperouter-system
spec:
  nodeSelector:
    matchLabels:
      topology.kubernetes.io/rack: rack-1
  vrf: tenant-a
  vni: 5001
  vxlanport: 4789
  hostsession:
    asn: 64512
    hostasn: 64600
    localcidr:
      ipv4: 192.169.1.0/24
---
# L3VNI for rack-2 nodes
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: L3VNI
metadata:
  name: tenant-a-rack-2
  namespace: openperouter-system
spec:
  nodeSelector:
    matchLabels:
      topology.kubernetes.io/rack: rack-2
  vrf: tenant-a
  vni: 5002
  vxlanport: 4789
  hostsession:
    asn: 64512
    hostasn: 64600
    localcidr:
      ipv4: 192.169.2.0/24
```

### Multiple VNIs on Same Nodes

Multiple L3VNIs can be configured on the same set of nodes for multi-tenancy:

```yaml
# Tenant A VNI on worker nodes
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: L3VNI
metadata:
  name: tenant-a-vni
  namespace: openperouter-system
spec:
  nodeSelector:
    matchLabels:
      node-role.kubernetes.io/worker: ""
  vrf: tenant-a
  vni: 5001
  hostsession:
    asn: 64512
    hostasn: 64600
    localcidr:
      ipv4: 192.169.5.0/24
---
# Tenant B VNI on the same worker nodes
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: L3VNI
metadata:
  name: tenant-b-vni
  namespace: openperouter-system
spec:
  nodeSelector:
    matchLabels:
      node-role.kubernetes.io/worker: ""
  vrf: tenant-b
  vni: 5002
  hostsession:
    asn: 64512
    hostasn: 64601
    localcidr:
      ipv4: 192.169.6.0/24
---
# Tenant C VNI on the same worker nodes
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: L3VNI
metadata:
  name: tenant-c-vni
  namespace: openperouter-system
spec:
  nodeSelector:
    matchLabels:
      node-role.kubernetes.io/worker: ""
  vrf: tenant-c
  vni: 5003
  hostsession:
    asn: 64512
    hostasn: 64602
    localcidr:
      ipv4: 192.169.7.0/24
```

## L2VNI Examples

L2VNI configured only on worker nodes, not control plane:

```yaml
# L2VNI for worker nodes only
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: L2VNI
metadata:
  name: app-network
  namespace: openperouter-system
spec:
  nodeSelector:
    matchLabels:
      node-role.kubernetes.io/worker: ""
  vni: 10100
  vxlanport: 4789
  hostmaster:
    type: linux-bridge
    linuxBridge:
      autoCreate: true
  l2gatewayips:
    - 10.100.0.1/24
```

## L3Passthrough Examples

L3Passthrough configured only on edge nodes that participate in direct BGP fabric:

```yaml
# L3Passthrough for edge nodes
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: L3Passthrough
metadata:
  name: edge-passthrough
  namespace: openperouter-system
spec:
  nodeSelector:
    matchLabels:
      node-role.kubernetes.io/edge: ""
  hostsession:
    asn: 64512
    hostasn: 64700
    localcidr:
      ipv4: 192.169.20.0/24
```
