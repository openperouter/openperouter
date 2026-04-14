---
weight: 45
title: "Multiple Interfaces and Neighbors"
description: "Configure OpenPERouter with multiple underlay interfaces and BGP neighbors"
icon: "article"
date: "2026-04-09T10:00:00+02:00"
lastmod: "2026-04-09T10:00:00+02:00"
toc: true
---

OpenPERouter supports configuring multiple physical network interfaces and multiple BGP neighbors in a single Underlay resource. This enables production-grade deployments with redundancy, multi-path networking, and connections to multiple Top-of-Rack switches.

## Overview

The multi-interface and multi-neighbor feature allows you to:

- **Connect to multiple ToR switches** for redundancy and load distribution
- **Use multiple physical NICs** for bandwidth aggregation or network segregation
- **Establish multiple BGP sessions** to different neighbors or different sessions to the same neighbor
- **Create production-ready topologies** with full redundancy at both the interface and BGP session layers

## Basic Concepts

### Multiple Interfaces

When you specify multiple interfaces in the `nics` array, OpenPERouter moves all listed interfaces into the router namespace. These interfaces can be used for:

- **Redundant paths**: Multiple connections to the same ToR switch
- **Multi-ToR connectivity**: Dedicated interfaces for different ToR switches
- **Traffic segregation**: Separate interfaces for different traffic types

All interfaces share the same BGP configuration and neighbors can reach the router through any interface.

### Multiple Neighbors

When you specify multiple neighbors in the `neighbors` array, OpenPERouter establishes separate BGP sessions with each neighbor. This enables:

- **Dual-ToR deployments**: Peer with two ToR switches simultaneously
- **Multiple sessions per ToR**: Establish redundant BGP sessions to the same ToR
- **Multi-ASN topologies**: Peer with neighbors in different autonomous systems

## Configuration Examples

### Example 1: Single Interface, Multiple Neighbors

Connect to multiple ToR switches through a single shared interface:

```yaml
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: Underlay
metadata:
  name: underlay
  namespace: openperouter-system
spec:
  asn: 64514
  
  # Single shared interface
  nics:
  - "toswitch"
  
  # Multiple BGP neighbors (dual-ToR setup)
  neighbors:
  - asn: 64512
    address: "192.168.11.2"  # ToR-1 primary session
  - asn: 64512
    address: "192.168.11.3"  # ToR-1 secondary session
  - asn: 64513
    address: "192.168.12.2"  # ToR-2 primary session
  - asn: 64513
    address: "192.168.12.3"  # ToR-2 secondary session
  
  evpn:
    vtepcidr: "100.65.0.0/24"
```

**Use Case**: Datacenter with dual-ToR switches connected via a single bonded or trunk interface.

### Example 2: Multiple Interfaces, Single Neighbor

Use multiple interfaces to connect to a single ToR switch:

```yaml
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: Underlay
metadata:
  name: underlay
  namespace: openperouter-system
spec:
  asn: 64514
  
  # Multiple interfaces for bandwidth/redundancy
  nics:
  - "eth1"
  - "eth2"
  - "eth3"
  
  # Single BGP neighbor
  neighbors:
  - asn: 64512
    address: "192.168.11.2"
    bfd:
      receiveInterval: 300
      transmitInterval: 300
      detectMultiplier: 3
  
  evpn:
    vtepcidr: "100.65.0.0/24"
```

**Use Case**: Servers with multiple NICs connected to a single ToR for link aggregation or multi-path routing.

### Example 3: Multiple Interfaces AND Multiple Neighbors (Production)

Full redundancy with dedicated interfaces per ToR:

```yaml
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: Underlay
metadata:
  name: underlay
  namespace: openperouter-system
spec:
  asn: 64514
  
  # Dedicated interfaces per ToR
  nics:
  - "toswitch"   # Connects to ToR-1
  - "toswitch2"  # Connects to ToR-2
  
  # Multiple sessions to each ToR
  neighbors:
  - asn: 64512
    address: "192.168.11.2"  # ToR-1 session 1
  - asn: 64512
    address: "192.168.11.3"  # ToR-1 session 2
  - asn: 64513
    address: "192.168.12.2"  # ToR-2 session 1
  - asn: 64513
    address: "192.168.12.3"  # ToR-2 session 2
  
  evpn:
    vtepcidr: "100.65.0.0/24"
```

**Use Case**: Production datacenter deployment with dual-homed servers connecting to dual ToR switches.

## Validation Requirements

OpenPERouter enforces the following validation rules:

### Required Fields

- **At least one neighbor** must be configured in the `neighbors` array
- **At least one NIC** must be configured in the `nics` array (unless using Multus network attachments)

### Uniqueness Constraints

- **Neighbor addresses must be unique**: No two neighbors can have the same IP address
  ```yaml
  # INVALID - duplicate address
  neighbors:
  - asn: 64512
    address: "192.168.11.2"
  - asn: 64513
    address: "192.168.11.2"  # Error: duplicate!
  ```

- **NIC names must be unique**: No duplicate interface names
  ```yaml
  # INVALID - duplicate NIC
  nics:
  - "toswitch"
  - "eth1"
  - "toswitch"  # Error: duplicate!
  ```

### ASN Requirements

- **Local ASN must differ from all neighbor ASNs**: The Underlay only supports eBGP (external BGP), not iBGP
  ```yaml
  # INVALID - ASN conflict
  spec:
    asn: 64514
    neighbors:
    - asn: 64514  # Error: same as local ASN!
      address: "192.168.11.2"
  ```

### EVPN Requirements

- **Either vtepcidr OR vtepInterface**: Exactly one must be specified (see [EVPN Configuration]({{< ref "evpn.md" >}}))

### Per-Node Constraint

- **Only one Underlay per node**: Use `nodeSelector` to target different configurations to different nodes

## Migration from Single to Multi

Existing single-interface, single-neighbor configurations remain fully compatible. You can migrate incrementally by adding neighbors and interfaces over time.

### Migration Example

**Before (existing configuration)**:
```yaml
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: Underlay
metadata:
  name: underlay
  namespace: openperouter-system
spec:
  asn: 64514
  neighbors:
  - asn: 64512
    address: "192.168.11.2"
  nics:
  - "toswitch"
  evpn:
    vtepcidr: "100.65.0.0/24"
```

**After (upgraded to multi-neighbor/interface)**:
```yaml
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: Underlay
metadata:
  name: underlay
  namespace: openperouter-system
spec:
  asn: 64514
  
  # Added new neighbors (hot-applied without restart)
  neighbors:
  - asn: 64512
    address: "192.168.11.2"  # Existing
  - asn: 64512
    address: "192.168.11.3"  # NEW: Added
  - asn: 64513
    address: "192.168.12.2"  # NEW: Added
  
  # Added new interfaces (hot-applied without restart)
  nics:
  - "toswitch"   # Existing
  - "toswitch2"  # NEW: Added
  
  evpn:
    vtepcidr: "100.65.0.0/24"
```

Simply update the Underlay resource with `kubectl apply`. The controller will add the new neighbors and interfaces without restarting the router namespace or disrupting existing BGP sessions.

## Advanced Configuration

### BGP Session Tuning

You can configure different parameters for each neighbor:

```yaml
neighbors:
# Neighbor with fast failure detection
- asn: 64512
  address: "192.168.11.2"
  holdTime: "90s"
  keepaliveTime: "30s"
  bfd:
    receiveInterval: 300
    transmitInterval: 300
    detectMultiplier: 3

# Neighbor with longer timers
- asn: 64513
  address: "192.168.12.2"
  holdTime: "180s"
  keepaliveTime: "60s"

# eBGP multi-hop neighbor
- asn: 64515
  address: "10.0.1.1"
  ebgpMultiHop: true
```

### BGP Authentication

Secure BGP sessions with MD5 passwords:

```yaml
neighbors:
# Using inline password
- asn: 64512
  address: "192.168.11.2"
  password: "secure-bgp-password"

# Using Kubernetes secret (recommended)
- asn: 64513
  address: "192.168.12.2"
  passwordSecret: "bgp-tor2-secret"
```

To create the password secret:

```bash
kubectl create secret generic bgp-tor2-secret \
  --type=kubernetes.io/basic-auth \
  --from-literal=password='your-secure-password' \
  -n openperouter-system
```

### Per-Node Configuration

Target different multi-interface/neighbor configurations to different nodes:

```yaml
# Rack-A configuration
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: Underlay
metadata:
  name: underlay-rack-a
  namespace: openperouter-system
spec:
  nodeSelector:
    matchLabels:
      topology.kubernetes.io/zone: "rack-a"
  
  asn: 64514
  neighbors:
  - asn: 64512
    address: "192.168.1.2"  # Rack-A ToR
  - asn: 64513
    address: "192.168.2.2"  # Rack-A ToR
  nics:
  - "toswitch"
  evpn:
    vtepcidr: "100.65.0.0/24"

---
# Rack-B configuration
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: Underlay
metadata:
  name: underlay-rack-b
  namespace: openperouter-system
spec:
  nodeSelector:
    matchLabels:
      topology.kubernetes.io/zone: "rack-b"
  
  asn: 64514
  neighbors:
  - asn: 64514
    address: "192.168.3.2"  # Rack-B ToR
  - asn: 64515
    address: "192.168.4.2"  # Rack-B ToR
  nics:
  - "toswitch"
  evpn:
    vtepcidr: "100.65.0.0/24"
```

See [Node Selector Configuration]({{< ref "node-selector.md" >}}) for more details.

## Verification

### Check BGP Sessions

```bash
# Get router pod
ROUTER_POD=$(kubectl get pod -n openperouter-system -l app=router -o jsonpath='{.items[0].metadata.name}')

# View BGP summary - should show all neighbors
kubectl exec -n openperouter-system $ROUTER_POD -- vtysh -c "show bgp summary"

# Check individual neighbor status
kubectl exec -n openperouter-system $ROUTER_POD -- vtysh -c "show bgp neighbors 192.168.11.2"
```

### Verify Interfaces

```bash
# List interfaces in router namespace
kubectl exec -n openperouter-system $ROUTER_POD -- ip link show

# Check interface status
kubectl exec -n openperouter-system $ROUTER_POD -- ip addr
```

### Test Connectivity

```bash
# Ping BGP neighbors
kubectl exec -n openperouter-system $ROUTER_POD -- ping -c 3 192.168.11.2
kubectl exec -n openperouter-system $ROUTER_POD -- ping -c 3 192.168.12.2

# Check routes received from neighbors
kubectl exec -n openperouter-system $ROUTER_POD -- vtysh -c "show ip bgp"
```

## Troubleshooting

### BGP Sessions Not Establishing

If BGP neighbors show "Idle" or "Connect" instead of "Established":

1. Verify network connectivity:
   ```bash
   kubectl exec -n openperouter-system $ROUTER_POD -- ping -c 3 192.168.11.2
   ```

2. Check FRR logs:
   ```bash
   kubectl exec -n openperouter-system $ROUTER_POD -- cat /var/log/frr/bgpd.log
   ```

3. Verify ToR switch configuration matches your Underlay spec

### Validation Errors

If `kubectl apply` rejects your configuration:

- Ensure all neighbor addresses are unique
- Verify all NIC names are unique
- Check that local ASN differs from all neighbor ASNs
- Confirm at least one neighbor and one NIC are specified

### Interface Not Found

If interfaces aren't appearing in the router namespace:

1. Verify interface names match actual interfaces on the node:
   ```bash
   kubectl debug node/<node-name> -it --image=nicolaka/netshoot -- ip link
   ```

2. Check controller logs for errors:
   ```bash
   kubectl logs -n openperouter-system deployment/openperouter-controller-manager
   ```

## Best Practices

1. **Use BFD for fast failure detection**: Enable BFD on all neighbors for sub-second failover
2. **Configure redundant neighbors**: Peer with at least two ToR switches for high availability
3. **Use node selectors**: Target specific configurations to specific racks or zones
4. **Monitor BGP state**: Set up alerts for BGP session state changes
5. **Test failover scenarios**: Verify traffic continues flowing when one ToR or interface fails
6. **Incremental migration**: Add neighbors/interfaces one at a time in production
7. **Use secrets for passwords**: Store BGP passwords in Kubernetes secrets, not inline

## See Also

- [Underlay Configuration]({{< ref "_index.md#underlay-configuration" >}}) - Basic underlay concepts
- [EVPN Configuration]({{< ref "evpn.md" >}}) - VTEP and EVPN setup
- [Node Selector Configuration]({{< ref "node-selector.md" >}}) - Per-node targeting
- [API Reference]({{< ref "api-reference.md" >}}) - Complete API specification
