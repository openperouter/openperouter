# Per-Node Configuration with Node Selectors

## Summary

This enhancement proposes adding node selector support to all OpenPERouter CRDs (`Underlay`, `L3VNI`, `L2VNI`, and `L3Passthrough`), enabling different nodes to have different network configurations. This allows OpenPERouter to support heterogeneous cluster topologies including multi-datacenter, multi-rack, and mixed-hardware environments where different nodes may need different BGP configurations, VNI assignments, or passthrough settings.

## Motivation

Currently, OpenPERouter applies all CRD configurations cluster-wide, meaning all nodes in the cluster receive the same configuration. This creates operational limitations in real-world deployments:

**Underlay limitations:**
- **Multi-rack deployments**: Different nodes are physically connected to different ToR (Top of Rack) switches but cannot be configured to peer with their respective ToRs
- **Multi-datacenter clusters**: Nodes distributed across availability zones or datacenters need to peer with location-specific BGP routers
- **Hardware heterogeneity**: Different server models have different NIC naming conventions (e.g., Dell uses `eno1`, HP uses `em1`), preventing a one-size-fits-all configuration
- **Topology flexibility**: Cannot accommodate nodes in different physical network topologies within the same cluster

**L3VNI/L2VNI limitations:**
- **Per-rack VNI isolation**: Different racks may need separate VNI configurations for network segmentation
- **Zone-specific VNIs**: Nodes in different availability zones may require different VNI configurations for compliance or performance reasons
- **Selective VNI deployment**: Some workloads on specific nodes may need access to certain VNIs while others don't
- **Gateway placement**: L2 gateway IPs may need to be configured only on specific nodes based on network topology

**L3Passthrough limitations:**
- **Selective passthrough**: Only specific nodes should participate in direct BGP fabric communication
- **Security zones**: Different security zones may require different passthrough configurations
- **Specialized workloads**: Only nodes running certain workloads should have passthrough capabilities

### Goals

- Enable per-node configuration for all OpenPERouter CRDs using Kubernetes node selectors
- Support heterogeneous network configurations across different hardware platforms, racks, and zones
- Allow multiple instances of each CRD to coexist, each targeting specific node subsets
- Maintain backward compatibility with existing cluster-wide configurations
- Prevent configuration conflicts through validation

### Non-Goals

- Dynamic node selector updates based on workload scheduling (this is handled by node labels)
- Handle external events unrelated to node labels, like interfaces names and similar ones
- Support multiple underlays per node

## Proposal

### User Stories

**Underlay stories:**
- **As a cluster administrator**, I want each rack's nodes to peer with their local ToR switch so that network traffic stays within the rack when possible and I can configure rack-specific ASNs.
- **As a cluster administrator**, I want nodes in different datacenters to connect to datacenter-local BGP routers so that cross-datacenter BGP traffic is minimized and datacenter-specific network policies can be enforced.
- **As a cluster administrator**, I want to configure different NIC names for different server vendors so that I can use vendor-specific interface naming without requiring identical hardware across the cluster.

**L3VNI/L2VNI stories:**
- **As a cluster administrator**, I want to configure different VNIs for different racks so that each rack has isolated network segments for multi-tenancy.
- **As a cluster administrator**, I want nodes in specific zones to use zone-specific VNI configurations so that compliance requirements for data locality are met.
- **As a cluster administrator**, I want worker nodes (not control plane nodes) to have VNIs configured so that control plane traffic remains isolated.

**L3Passthrough stories:**
- **As a cluster administrator**, I want only edge nodes to have L3Passthrough configured so that only designated nodes participate in direct BGP fabric communication.
- **As a cluster administrator**, I want different security zones to have different passthrough configurations so that traffic policies can be enforced based on node location.

## Design Details

### API Changes

Add an optional `NodeSelector` field to all CRD Spec structures. This field will be identical across all CRDs to maintain consistency.

#### Underlay

```go
type UnderlaySpec struct {
    // NodeSelector specifies which nodes this Underlay applies to.
    // If empty or not specified, applies to all nodes (backward compatible).
    // Multiple Underlays with overlapping node selectors will be rejected.
    // +optional
    NodeSelector *metav1.LabelSelector `json:"nodeSelector,omitempty"`

    // ... rest of fields
}
```

#### L3VNI

```go
type L3VNISpec struct {
    // NodeSelector specifies which nodes this L3VNI applies to.
    // If empty or not specified, applies to all nodes (backward compatible).
    // Multiple L3VNIs can match the same node (unlike Underlay).
    // +optional
    NodeSelector *metav1.LabelSelector `json:"nodeSelector,omitempty"`

    // ... rest of fields
}
```

#### L2VNI

```go
type L2VNISpec struct {
    // NodeSelector specifies which nodes this L2VNI applies to.
    // If empty or not specified, applies to all nodes (backward compatible).
    // Multiple L2VNIs can match the same node (unlike Underlay).
    // +optional
    NodeSelector *metav1.LabelSelector `json:"nodeSelector,omitempty"`

    // ... rest of fields
}
```

#### L3Passthrough

```go
type L3PassthroughSpec struct {
    // NodeSelector specifies which nodes this L3Passthrough applies to.
    // If empty or not specified, applies to all nodes (backward compatible).
    // Multiple L3Passthroughs can match the same node (unlike Underlay).
    // +optional
    NodeSelector *metav1.LabelSelector `json:"nodeSelector,omitempty"`

    // ... rest of fields
}
```

### Example Configurations

#### Underlay: Multi-Rack Configuration

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

#### Underlay: Multi-Datacenter Configuration

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

#### Underlay: Hardware-Specific NIC Configuration

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

#### L3VNI: Per-Rack VNI Configuration

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

#### L3VNI: Zone-Specific Configuration

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

#### L2VNI: Selective Deployment

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
    type: bridge
    autocreate: true
  l2gatewayip: 10.100.0.1/24
```

#### L2VNI: Per-Rack Gateway Configuration

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
    name: br-storage
    type: bridge
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
    name: br-storage
    type: bridge
  l2gatewayip: 10.200.2.1/24
```

#### L3Passthrough: Edge Nodes Only

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

#### L3Passthrough: Per-Security-Zone Configuration

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

#### Multiple Instances on Same Node

Example showing multiple L3VNIs configured on the same set of nodes for different tenants:

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

In this example, all worker nodes (with `node-role.kubernetes.io/worker` label) will have three L3VNIs configured, one for each tenant. This is allowed and expected behavior for L3VNI, L2VNI, and L3Passthrough resources.

### Controller Implementation

#### Node Matching Logic

All controllers (Underlay, L3VNI, L2VNI, and L3Passthrough) will be enhanced with:

1. **Node Label Watching**: Add a watch for Node resource label changes to trigger reconciliation
2. **Selector Matching**: For each node, determine which resource instance applies by matching node labels against all resource `nodeSelector` fields
3. **Configuration Application**: Generate and apply configuration only on nodes matched by the resource's selector
4. **Dynamic Updates**: Reconcile when:
   - Resources are created/updated/deleted
   - Node labels change
   - Nodes are added/removed from the cluster

#### Conflict Resolution

**Underlay-specific restriction:**

Only one Underlay can match a given node. This is enforced in the reconcile loop, which validates configurations, updates status conditions, and skips invalid configurations.

**L3VNI, L2VNI, and L3Passthrough:**

Multiple instances can match the same node (for multi-tenancy), but the controller validation logic will check:
- Two L2VNIs with the same VRF on one node
- VNI number conflicts across resource types
- Incompatible configurations

**Cross-CRD matching:**

Different CRD types can freely match the same node (e.g., a node can have one Underlay, multiple L3VNIs, multiple L2VNIs, and multiple L3Passthroughs).

### Backward Compatibility

**Default Behavior**: When `nodeSelector` is `nil` or not specified, the CRDs instances applies to all nodes in the cluster, maintaining backward compatibility with existing configurations.

**Migration Path**:
N/A

### Validation Rules

Validation occurs in the reconcile loop and performs cluster-wide consistency checks:

1. **No Overlapping Selectors for Underlay**: Multiple Underlays cannot match the same node
2. **Selector Validity**: Node selector must be a valid `metav1.LabelSelector` for all CRD types
3. **No conflicts per node**: L3VNI, L2VNI, and L3Passthrough can have multiple instances per node, but they cannot conflict (e.g., same VRF)
4. **Cluster-wide conflict detection**: Detects cross-resource conflicts:
   - VNI conflicts across different resource types
   - Configuration drift between related resources
   - Race condition detection (e.g., two Underlays created simultaneously with overlapping selectors)
5. **Failure handling**:
   - Log errors with conflict details
   - Update status conditions
   - Skip invalid configuration application
   - Requeue for retry

### Scalability Considerations

- **Node Watch Overhead**: The controllers watch Node resources for label changes. In clusters with frequent node label updates, this may increase reconciliation frequency. We can improve this by making each daemonset pod reconcile only on the running node using controller-runtime cache filter mechanism.
- **Selector Evaluation**: For each reconciliation, the controller evaluates node selectors against the node where the daemonset is running, so reconciliation is done in parallel between the nodes.

## References

- Current APIs:
- Underlay API: `api/v1alpha1/underlay_types.go`
- L3VNI API: `api/v1alpha1/l3vni_types.go`
- L2VNI API: `api/v1alpha1/l2vni_types.go`
- L3Passthrough API: `api/v1alpha1/l3passthrough_types.go`
- Example Configuration: `examples/evpn/calico/openpe.yaml:16`
- Kubernetes Label Selectors: https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/
- DaemonSet Node Selection: https://kubernetes.io/docs/concepts/workloads/controllers/daemonset/#running-pods-on-select-nodes
