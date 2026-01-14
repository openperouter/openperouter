---
weight: 45
title: "Node Selector Configuration"
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

### Backward Compatibility

When the `nodeSelector` field is omitted or set to `null`, the configuration applies to all nodes in the cluster, maintaining backward compatibility with existing configurations.

## Node Selector Syntax

The `nodeSelector` field uses Kubernetes label selectors (`metav1.LabelSelector`), supporting both `matchLabels` and `matchExpressions`:

### Match Labels

```yaml
spec:
  nodeSelector:
    matchLabels:
      topology.kubernetes.io/rack: rack-1
      hardware.vendor: dell
```

### Match Expressions

```yaml
spec:
  nodeSelector:
    matchExpressions:
      - key: topology.kubernetes.io/zone
        operator: In
        values:
          - us-east-1a
          - us-east-1b
```

### Combined Selectors

```yaml
spec:
  nodeSelector:
    matchLabels:
      node-role.kubernetes.io/worker: ""
    matchExpressions:
      - key: topology.kubernetes.io/rack
        operator: In
        values:
          - rack-1
          - rack-2
```

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

## Underlay Examples

### Multi-Rack Configuration

Different racks connect to different ToR switches:

```yaml
# Rack 1 nodes connect to ToR 1
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: Underlay
metadata:
  name: underlay-rack-1
  namespace: openperouter-system
spec:
  nodeSelector:
    matchLabels:
      topology.kubernetes.io/rack: rack-1
  asn: 64512
  evpn:
    vtepcidr: 100.65.1.0/24
  nics:
    - toswitch
  neighbors:
    - asn: 64500
      address: 192.168.1.254  # ToR switch for rack 1
      bfd:
        receiveInterval: 300
        transmitInterval: 300
---
# Rack 2 nodes connect to ToR 2
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: Underlay
metadata:
  name: underlay-rack-2
  namespace: openperouter-system
spec:
  nodeSelector:
    matchLabels:
      topology.kubernetes.io/rack: rack-2
  asn: 64512
  evpn:
    vtepcidr: 100.65.2.0/24
  nics:
    - toswitch
  neighbors:
    - asn: 64500
      address: 192.168.2.254  # ToR switch for rack 2
      bfd:
        receiveInterval: 300
        transmitInterval: 300
```

### Multi-Datacenter Configuration

Different datacenters with different ASNs:

```yaml
# Underlay for datacenter-east nodes
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: Underlay
metadata:
  name: underlay-dc-east
  namespace: openperouter-system
spec:
  nodeSelector:
    matchLabels:
      topology.kubernetes.io/zone: us-east-1a
  asn: 64512
  evpn:
    vtepcidr: 100.65.0.0/24
  nics:
    - eth1
  neighbors:
    - asn: 64500
      address: 192.168.10.1  # ToR switch in DC East
---
# Underlay for datacenter-west nodes
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: Underlay
metadata:
  name: underlay-dc-west
  namespace: openperouter-system
spec:
  nodeSelector:
    matchLabels:
      topology.kubernetes.io/zone: us-west-1a
  asn: 64513
  evpn:
    vtepcidr: 100.66.0.0/24
  nics:
    - eth1
  neighbors:
    - asn: 64501
      address: 192.168.20.1  # ToR switch in DC West
```

### Hardware-Specific NIC Configuration

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
```

### Zone-Specific Configuration

Different availability zones have different L3VNI configurations:

```yaml
# L3VNI for us-east-1a
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: L3VNI
metadata:
  name: tenant-b-east
  namespace: openperouter-system
spec:
  nodeSelector:
    matchLabels:
      topology.kubernetes.io/zone: us-east-1a
  vrf: tenant-b
  vni: 6001
  hostsession:
    asn: 64512
    hostasn: 64601
---
# L3VNI for us-west-1a
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: L3VNI
metadata:
  name: tenant-b-west
  namespace: openperouter-system
spec:
  nodeSelector:
    matchLabels:
      topology.kubernetes.io/zone: us-west-1a
  vrf: tenant-b
  vni: 6002
  hostsession:
    asn: 64513
    hostasn: 64601
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
```

## L2VNI Examples

### Selective Deployment

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
  l2gatewayip: 10.100.0.1/24
```

### Per-Rack Gateway Configuration

Different racks have different L2 gateway IPs:

```yaml
# L2VNI for rack-1
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: L2VNI
metadata:
  name: storage-rack-1
  namespace: openperouter-system
spec:
  nodeSelector:
    matchLabels:
      topology.kubernetes.io/rack: rack-1
  vni: 10200
  vrf: storage
  hostmaster:
    type: linux-bridge
    linuxBridge:
      name: br-storage
  l2gatewayip: 10.200.1.1/24
---
# L2VNI for rack-2
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: L2VNI
metadata:
  name: storage-rack-2
  namespace: openperouter-system
spec:
  nodeSelector:
    matchLabels:
      topology.kubernetes.io/rack: rack-2
  vni: 10201
  vrf: storage
  hostmaster:
    type: linux-bridge
    linuxBridge:
      name: br-storage
  l2gatewayip: 10.200.2.1/24
```

## L3Passthrough Examples

### Edge Nodes Only

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
```

### Per-Security-Zone Configuration

Different security zones have different passthrough configurations:

```yaml
# L3Passthrough for DMZ nodes
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: L3Passthrough
metadata:
  name: dmz-passthrough
  namespace: openperouter-system
spec:
  nodeSelector:
    matchLabels:
      security-zone: dmz
  hostsession:
    asn: 64512
    hostasn: 64710
---
# L3Passthrough for internal nodes
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: L3Passthrough
metadata:
  name: internal-passthrough
  namespace: openperouter-system
spec:
  nodeSelector:
    matchLabels:
      security-zone: internal
  hostsession:
    asn: 64512
    hostasn: 64720
```

## Labeling Nodes

Before using node selectors, you need to label your nodes appropriately. Here are some examples:

### Label by Rack

```bash
kubectl label node worker-1 topology.kubernetes.io/rack=rack-1
kubectl label node worker-2 topology.kubernetes.io/rack=rack-1
kubectl label node worker-3 topology.kubernetes.io/rack=rack-2
kubectl label node worker-4 topology.kubernetes.io/rack=rack-2
```

### Label by Hardware Vendor

```bash
kubectl label node worker-1 hardware.vendor=dell
kubectl label node worker-2 hardware.vendor=dell
kubectl label node worker-3 hardware.vendor=hp
kubectl label node worker-4 hardware.vendor=hp
```

### Label by Security Zone

```bash
kubectl label node edge-1 security-zone=dmz
kubectl label node edge-2 security-zone=dmz
kubectl label node worker-1 security-zone=internal
kubectl label node worker-2 security-zone=internal
```

### Verify Labels

Check the labels on a node:

```bash
kubectl get node worker-1 --show-labels
```

Or filter nodes by label:

```bash
kubectl get nodes -l topology.kubernetes.io/rack=rack-1
```

## Troubleshooting

### Check Which Nodes Match a Selector

To verify which nodes would be selected by a configuration, use the label selector with `kubectl get nodes`:

```bash
# For matchLabels
kubectl get nodes -l topology.kubernetes.io/rack=rack-1

# For matchExpressions with In operator
kubectl get nodes -l 'topology.kubernetes.io/zone in (us-east-1a,us-east-1b)'
```

### View Resource Status

Check the status of your resources to see if there are any conflicts or validation errors:

```bash
kubectl get underlay -n openperouter-system
kubectl describe underlay underlay-rack-1 -n openperouter-system
```

### Common Issues

**Underlay Conflict**: If you see an error about overlapping Underlay selectors, ensure that each node matches only one Underlay resource.

**No Nodes Match**: If a resource applies to zero nodes, verify:
- Node labels are set correctly
- The label selector syntax is valid
- Nodes exist in the cluster

**VNI Conflicts**: If L2VNI or L3VNI resources conflict, check:
- VNI numbers are unique across all VNI resources
- L2VNIs with the same VRF aren't applied to the same nodes

## API Reference

For detailed information about all available configuration fields, validation rules, and API specifications, see the [API Reference]({{< ref "api-reference.md" >}}) documentation.

## Related Documentation

- [Underlay Configuration]({{< ref "configuration/#underlay-configuration" >}})
- [EVPN Configuration]({{< ref "evpn.md" >}})
- [Passthrough Configuration]({{< ref "passthrough.md" >}})
- [Enhancement Proposal: Per-Node Configuration](https://github.com/openperouter/openperouter/blob/main/enhancements/per-node-configuration.md)
