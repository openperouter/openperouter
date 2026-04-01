# Underlay API Contracts

**Feature**: 006-multi-underlay-neighbors  
**Status**: Design Complete

## Overview

This directory contains the API contract specifications for the multi-interface and multi-neighbor underlay feature.

## Files

- **underlay-crd-schema.md**: Complete CRD schema definition with validation rules
- **examples.yaml**: Comprehensive YAML examples covering all use cases and edge cases

## API Contract Summary

### Core Principle

**Backward Compatible Extension**: Existing single-interface/single-neighbor configurations remain valid. The API simply removes artificial limits on array sizes.

### Interface Contract

**Endpoint**: Kubernetes API Server  
**Resource**: `underlays.openpe.openperouter.github.io/v1alpha1`  
**Operations**: CREATE, READ, UPDATE, DELETE (standard Kubernetes operations)

### Request Validation

**Two-Stage Validation**:

1. **Kubernetes API Server** (CRD schema):
   - Type checking (uint32, string, etc.)
   - Range validation (ASN 1-4294967295, port 0-16384)
   - Pattern matching (interface names)
   - Required field presence
   - minItems array constraints

2. **Admission Webhook** (`/validate-openperouter-io-v1alpha1-underlay`):
   - Uniqueness checks (neighbor addresses, nic names)
   - Cross-field validation (ASN conflicts, EVPN requirements)
   - Format validation (CIDR parsing, IP address validation)
   - Node selector overlap detection

### Response Contract

**Success** (HTTP 200):
```json
{
  "apiVersion": "openpe.openperouter.github.io/v1alpha1",
  "kind": "Underlay",
  "metadata": {
    "name": "underlay-example",
    "namespace": "default",
    "resourceVersion": "12345",
    "generation": 1
  },
  "spec": { ... }
}
```

**Validation Failure** (HTTP 400):
```json
{
  "kind": "Status",
  "apiVersion": "v1",
  "status": "Failure",
  "message": "admission webhook denied the request: <error details>",
  "reason": "Invalid",
  "code": 400
}
```

**Conflict** (HTTP 409):
```json
{
  "kind": "Status",
  "status": "Failure",
  "message": "Operation cannot be fulfilled: resource version conflict",
  "reason": "Conflict",
  "code": 409
}
```

## Usage Patterns

### Pattern 1: Redundant Paths with Multiple Interfaces

**Use Case**: Multiple physical links to same ToR for bandwidth aggregation and failover

```yaml
spec:
  asn: 65001
  neighbors:
  - asn: 65002
    address: "192.168.1.1"
  nics:
  - "eth1"
  - "eth2"  # Multiple interfaces, single neighbor
```

**Behavior**:
- FRR establishes one BGP session to 192.168.1.1
- Traffic can flow over both eth1 and eth2
- FRR load-balances based on routing table (ECMP if configured)

### Pattern 2: Multi-ToR Connectivity

**Use Case**: Connections to multiple ToR switches for fault tolerance

```yaml
spec:
  asn: 65001
  neighbors:
  - asn: 65002
    address: "192.168.1.1"  # TOR-1
  - asn: 65002
    address: "192.168.2.1"  # TOR-2
  nics:
  - "eth0"
```

**Behavior**:
- FRR establishes BGP sessions to both ToRs
- Routes learned from both neighbors
- Automatic failover if one ToR fails

### Pattern 3: Full Mesh with Multiple Interfaces and Neighbors

**Use Case**: Maximum redundancy with multiple paths to multiple ToRs

```yaml
spec:
  asn: 65001
  neighbors:
  - asn: 65002
    address: "192.168.1.1"  # TOR-1
  - asn: 65002
    address: "192.168.1.2"  # TOR-1 secondary
  - asn: 65003
    address: "192.168.2.1"  # TOR-2
  nics:
  - "eth1"  # Path to TOR-1
  - "eth2"  # Path to TOR-2
```

**Behavior**:
- BGP sessions to all neighbors
- Traffic spreads across all paths
- Survives multiple link or ToR failures

### Pattern 4: Hot-Add Neighbors/Interfaces

**Use Case**: Add capacity without service disruption

**Step 1** - Initial deployment:
```yaml
spec:
  neighbors:
  - asn: 65002
    address: "192.168.1.1"
  nics:
  - "eth0"
```

**Step 2** - Update to add more (hot-applied, no restart):
```yaml
spec:
  neighbors:
  - asn: 65002
    address: "192.168.1.1"  # Existing
  - asn: 65002
    address: "192.168.1.2"  # NEW - hot-added
  nics:
  - "eth0"  # Existing
  - "eth1"  # NEW - hot-added
```

**Behavior**:
- Controller detects change is additive
- New interface moved to namespace without restart
- New BGP neighbor added via `vtysh` without restart
- Existing sessions remain established

## Interface-Neighbor Relationship

**Many-to-Many**:

- Each neighbor can be reached via any interface (FRR routing decision)
- Multiple neighbors can share a single interface (standard BGP behavior)
- No explicit binding between specific interfaces and specific neighbors

**Example**:
```
Interfaces: eth1, eth2, eth3
Neighbors: 192.168.1.1, 192.168.2.1

Possible traffic flows:
  192.168.1.1 via eth1
  192.168.1.1 via eth2  (FRR chooses based on routing table)
  192.168.2.1 via eth2
  192.168.2.1 via eth3
```

## Error Handling

### Client-Side Validation

Before submitting, clients should check:

1. **Uniqueness**: No duplicate addresses in neighbors, no duplicate names in nics
2. **ASN Conflicts**: Local ASN differs from all neighbor ASNs
3. **EVPN XOR**: If EVPN configured, exactly one of vtepcidr or vtepInterface
4. **Valid Formats**: IP addresses, CIDRs, interface name patterns

### Server-Side Errors

| Error Code | Condition | User Action |
|------------|-----------|-------------|
| 400 Invalid | Duplicate neighbor address | Remove duplicate or change address |
| 400 Invalid | Duplicate nic name | Remove duplicate or change name |
| 400 Invalid | ASN conflict (local == remote) | Change local or remote ASN |
| 400 Invalid | Invalid CIDR format | Fix CIDR notation |
| 400 Invalid | Invalid IP address | Fix IP address format |
| 400 Invalid | Node selector overlap | Adjust node selectors to be mutually exclusive |
| 409 Conflict | Resource version mismatch | Re-fetch resource and retry update |
| 404 Not Found | Resource doesn't exist | Check name/namespace |

## Observability

### Status Fields

**Future Enhancement** (not in initial implementation):

```yaml
status:
  conditions:
  - type: Ready
    status: "True"
    reason: AllNodesReconciled
  nodeStatus:
  - nodeName: node-1
    phase: Running
    neighborsEstablished: 3
    interfacesConfigured: 2
  - nodeName: node-2
    phase: Running
    neighborsEstablished: 3
    interfacesConfigured: 2
```

**Current**: Status remains empty, check FRR status via `kubectl exec` and `vtysh`

### Events

Kubernetes events emitted by controller:

- `UnderlayReconciled`: Underlay successfully applied to node
- `UnderlayReconciliationFailed`: Error during reconciliation
- `UnderlayRestartRequired`: Configuration change requires restart
- `UnderlayHotApplied`: Configuration change applied without restart

## Best Practices

### 1. Start with Single Entities, Expand Incrementally

```yaml
# Initial deployment
neighbors: [192.168.1.1]
nics: [eth0]

# After validation, add more
neighbors: [192.168.1.1, 192.168.1.2]
nics: [eth0, eth1]
```

### 2. Use BFD for Fast Failure Detection

```yaml
neighbors:
- address: "192.168.1.1"
  bfd:
    enabled: true
    minRx: 300
    minTx: 300
    multiplier: 3
```

### 3. Use Node Selectors for Rack/Zone Isolation

```yaml
nodeSelector:
  matchLabels:
    rack: "rack-1"
```

Different racks get different Underlays with different ToR connections.

### 4. Document Topology in Annotations

```yaml
metadata:
  annotations:
    topology: "eth1->TOR1, eth2->TOR2, eth3->TOR1-backup"
```

### 5. Test in Dev Before Production

- Deploy in dev namespace first
- Validate all BGP sessions establish
- Verify data plane connectivity
- Test hot-add/remove scenarios
- Then promote to production

## Versioning and Compatibility

**Current Version**: v1alpha1

**Stability**: Alpha - API may change before v1beta1

**Deprecation Policy**: Breaking changes allowed in alpha, but backward compatibility maintained for this feature

**Future Versions**:
- v1alpha2: Potential status field enhancements
- v1beta1: API stabilization
- v1: GA release

## Support and Troubleshooting

### Check Validation Errors

```bash
kubectl apply -f underlay.yaml
# Look for webhook denial messages
```

### Verify Underlay Applied to Nodes

```bash
kubectl get underlays -A
kubectl describe underlay <name>
```

### Check FRR BGP Status

```bash
kubectl exec -it <router-pod> -- vtysh -c "show bgp summary"
kubectl exec -it <router-pod> -- vtysh -c "show ip bgp neighbors"
```

### Check Interface Movement

```bash
kubectl exec -it <router-pod> -- ip link show
# Verify interfaces are in router namespace
```

### Debug Hot-Apply vs Restart

Check controller logs for restart decision:

```bash
kubectl logs -n openperouter-system <controller-pod> | grep -i restart
```

Look for log messages indicating hot-apply or restart path taken.
