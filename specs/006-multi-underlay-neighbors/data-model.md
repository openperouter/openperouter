# Data Model: Support Multiple Underlay Interfaces and Neighbors

**Feature**: 006-multi-underlay-neighbors  
**Date**: 2026-04-01  
**Status**: Design Complete

## Entity Model

### Underlay (CRD)

Primary Kubernetes custom resource representing underlay network configuration.

**Fields**:
- `metadata` (ObjectMeta): Standard Kubernetes metadata
  - `name`: Underlay instance identifier
  - `namespace`: Kubernetes namespace
  
- `spec` (UnderlaySpec):
  - `nodeSelector` (LabelSelector, optional): Nodes this underlay applies to
  - `asn` (uint32, required): Local BGP AS number (1-4294967295)
  - `routerIDCIDR` (string, optional): IPv4 CIDR for router ID assignment per node (default: "10.0.0.0/24")
  - `neighbors` ([]Neighbor, required): List of BGP neighbors (min 1 per validation, max: resource-limited)
  - `nics` ([]string, optional): Physical interface names to move to router namespace (pattern: `^[a-zA-Z][a-zA-Z0-9._-]*$`, maxLength: 15)
  - `evpn` (EVPNConfig, optional): EVPN-VXLAN configuration

- `status` (UnderlayStatus):
  - Currently empty, future: per-node reconciliation status

**Changes from Current**:
- `neighbors` array: Remove `MinItems=1` constraint from clarification (spec now requires min 1)
- `nics` array: Already supports multiple, but conversion layer assumes single - fix consumption

**Validation Rules**:
1. At least one neighbor OR one interface must be specified (spec clarification: FR-028)
2. ASN must be unique from all neighbor ASNs
3. If node selector overlaps with another Underlay, validation fails
4. Neighbors array must not contain duplicate addresses
5. Nics array must not contain duplicate interface names
6. EVPN config: exactly one of vtepCIDR or vtepInterface must be set

---

### Neighbor (Embedded Type)

Represents a BGP neighbor relationship.

**Fields**:
- `asn` (uint32, required): Remote AS number (1-4294967295)
- `hostASN` (*uint32, optional): AS number for host namespace BGP component (defaults to `asn`)
- `address` (string, required): IP address of neighbor
- `port` (*uint16, optional): BGP port (default: 179, range: 0-16384)
- `password` (*string, optional): BGP authentication password
- `bfd` (*BFDConfig, optional): BFD configuration for fast failure detection
- `ebgpMultihop` (*uint8, optional): eBGP multihop TTL value

**Relationships**:
- Many-to-many with Interfaces: Each neighbor can be reached via any interface; multiple neighbors can share an interface (spec clarification)
- One-to-one with BFDProfile: Each neighbor optionally gets a BFD profile

**Identity**:
- Uniqueness: address field must be unique within an Underlay's neighbors array
- No explicit ID field - address serves as natural key

**Validation Rules**:
1. Address must be valid IPv4 or IPv6
2. Port range: 0-16384 (if specified)
3. ASN must differ from Underlay ASN (eBGP requirement)
4. BFD parameters must be valid if specified

---

### EVPNConfig (Embedded Type)

EVPN-VXLAN configuration for the underlay.

**Fields**:
- `vtepCIDR` (string, optional): CIDR for VTEP IP allocation (mutually exclusive with vtepInterface)
- `vtepInterface` (string, optional): Existing interface name to use for VTEP source (mutually exclusive with vtepCIDR)

**Validation Rules**:
- Exactly one of vtepCIDR or vtepInterface must be set (XOR constraint)
- vtepCIDR must be valid CIDR notation if specified
- vtepInterface must match pattern `^[a-zA-Z][a-zA-Z0-9._-]*$` with maxLength 15

---

### BFDConfig (Embedded Type)

Bidirectional Forwarding Detection configuration for fast neighbor failure detection.

**Fields**:
- `enabled` (bool): Enable BFD for this neighbor
- `minRx` (*uint32, optional): Minimum receive interval (ms)
- `minTx` (*uint32, optional): Minimum transmit interval (ms)
- `multiplier` (*uint8, optional): Detection multiplier

---

## Data Model Diagram

```
Underlay CRD (1)
    |
    ├── spec.nodeSelector? (0..1) ────────> LabelSelector
    |
    ├── spec.neighbors (1..N) ────────────> []Neighbor
    |       └── neighbor.address (unique)
    |       └── neighbor.bfd? (0..1) ────> BFDConfig
    |
    ├── spec.nics (0..N) ─────────────────> []string (interface names, unique)
    |       └── Many-to-many relationship with Neighbors (via FRR routing)
    |
    └── spec.evpn? (0..1) ────────────────> EVPNConfig
            └── vtepCIDR XOR vtepInterface
```

**Cardinality**:
- 1 Underlay : N Neighbors (1..resource-limit, spec requires min 1)
- 1 Underlay : N Nics (0..resource-limit)
- M Neighbors : N Nics (many-to-many via FRR routing table)
- 1 Neighbor : 0..1 BFDConfig

---

## State Transitions

### Underlay Lifecycle

```
    [Created] 
        |
        | Webhook validates spec
        | (unique neighbors, unique nics, ASN checks)
        |
        v
    [Validated] ─── ValidationError ──> [Rejected]
        |
        | Controller reconciles
        | (assigns to nodes via nodeSelector)
        |
        v
    [Reconciling]
        |
        | Per-node: Convert API -> FRR config + host network config
        |            Check restart requirement
        |
        v
    [Deployed on Nodes]
        |
        |<─── User Updates Spec ────|
        |                           |
        | Webhook re-validates      |
        | Controller reconciles     |
        | Decision: Hot-apply vs Restart
        |                           |
        v                           |
    [Updated] ─────────────────────|
        |
        | User Deletes
        v
    [Terminating]
        |
        | Cleanup router namespace, move interfaces back
        v
    [Deleted]
```

### Reconciliation Decision Tree

```
Update Detected
    |
    ├──> No previous config? ──> RESTART (initial setup)
    |
    ├──> ASN changed? ──> RESTART (fundamental parameter)
    |
    ├──> Router ID CIDR changed? ──> RESTART (IP reallocation)
    |
    ├──> All neighbors + nics removed? ──> RESTART (teardown)
    |
    ├──> EVPN vtepCIDR <-> vtepInterface switched? ──> RESTART (VTEP reconfiguration)
    |
    └──> Otherwise (additions, neighbor param changes) ──> HOT-APPLY
            |
            ├──> New neighbors added ──> Add via vtysh
            |
            ├──> New interfaces added ──> Move to namespace, configure
            |
            └──> Neighbor params changed ──> Reload FRR config, soft reset BGP
```

---

## Validation Rules (Comprehensive)

### CRD-Level Validation (Kubebuilder Markers)

Enforced by Kubernetes API server before webhook:

1. **ASN**: Range 1-4294967295
2. **Nics**: Each element matches `^[a-zA-Z][a-zA-Z0-9._-]*$`, max length 15
3. **Neighbor ASN**: Range 1-4294967295
4. **Neighbor Port**: Range 0-16384 (if specified)
5. **EVPN XOR**: Either vtepCIDR or vtepInterface, not both (XValidation rule)

### Webhook Validation (Custom Logic)

Enforced by admission webhook in `internal/webhooks/underlay_webhook.go`:

1. **Uniqueness Checks**:
   - Neighbor addresses must be unique within neighbors array
   - Nic names must be unique within nics array
   - No duplicate Underlay per node (via nodeSelector overlap check)

2. **Cross-Field Validation**:
   - Local ASN must differ from all neighbor ASNs (eBGP requirement)
   - If EVPN is nil but L3VNIs/L2VNIs exist → Error
   - At least one neighbor OR one nic must be specified

3. **Format Validation**:
   - VTEP CIDR must parse as valid CIDR
   - Neighbor addresses must parse as valid IPs
   - Router ID CIDR must parse as valid CIDR

4. **Resource Limits**:
   - No hard-coded max for neighbors/nics (resource-constrained)
   - Practical CI/dev testing: 10 interfaces, 20 neighbors

### Runtime Validation (Conversion Layer)

Enforced during API-to-internal-config conversion in `internal/conversion/`:

**REMOVED Validations** (current bugs):
- ~~`if len(underlays) > 1` check~~ → Allow per-node filtering to handle multiple
- ~~`Nics[0]` array index access~~ → Iterate all nics

**Retained Validations**:
- EVPN required when VNIs present
- Valid CIDR formats for IP allocation
- Interface names exist on host (runtime check)

---

## Data Transformations

### API → FRR Config

**Input**: Underlay CRD spec  
**Output**: FRR configuration file

```go
// Pseudo-code transformation
func UnderlayToFRRConfig(underlay Underlay, nodeIndex int) FRRConfig {
    routerID := allocateRouterID(underlay.Spec.RouterIDCIDR, nodeIndex)
    
    neighbors := []FRRNeighbor{}
    for _, n := range underlay.Spec.Neighbors {
        neighbors = append(neighbors, FRRNeighbor{
            Address: n.Address,
            RemoteAS: n.ASN,
            // BFD, password, etc.
        })
    }
    
    return FRRConfig{
        RouterID: routerID,
        LocalAS: underlay.Spec.ASN,
        Neighbors: neighbors,  // Multiple neighbors
        EVPN: underlay.Spec.EVPN != nil,
        // ...
    }
}
```

**Key Change**: Loop `underlay.Spec.Neighbors` instead of assuming single neighbor

### API → Host Network Config

**Input**: Underlay CRD spec  
**Output**: Host network namespace configuration

```go
func UnderlayToHostConfig(underlay Underlay, targetNS string) HostConfig {
    interfaces := []string{}
    for _, nic := range underlay.Spec.Nics {
        interfaces = append(interfaces, nic)  // All interfaces, not just [0]
    }
    
    return HostConfig{
        Namespace: targetNS,
        Interfaces: interfaces,  // Multiple interfaces
        // ...
    }
}
```

**Key Change**: Store all nics, iterate when moving to namespace

---

## Indexing and Querying

### Primary Indexes

1. **By Node**: `Underlay.Spec.NodeSelector` → Nodes
   - Controller maintains index of which Underlays apply to which nodes
   - Webhook validates no overlapping node selectors

2. **By Name**: `Underlay.Metadata.Name` (standard K8s)
   - Unique within namespace

### Query Patterns

1. **Get Underlays for Node**:
   ```go
   func UnderlaysForNode(node Node, allUnderlays []Underlay) []Underlay {
       filtered := []Underlay{}
       for _, u := range allUnderlays {
           if nodeSelectorMatches(u.Spec.NodeSelector, node.Labels) {
               filtered = append(filtered, u)
           }
       }
       return filtered
   }
   ```

2. **Validate No Overlap**:
   ```go
   func ValidateNoOverlap(underlays []Underlay, nodes []Node) error {
       for _, node := range nodes {
           matching := UnderlaysForNode(node, underlays)
           if len(matching) > 1 {
               return fmt.Errorf("node %s matched by multiple underlays", node.Name)
           }
       }
       return nil
   }
   ```

---

## Backward Compatibility

### Schema Compatibility

**Current Schema** (simplified):
```go
type UnderlaySpec struct {
    ASN uint32
    Neighbors []Neighbor  // Already array
    Nics []string         // Already array
}
```

**New Schema**: Identical structure, validation changes only

**Migration**: None required
- Existing single-element arrays `[neighbor1]` remain valid
- Existing single-element nics `["eth0"]` remain valid
- Update validation to allow multiple elements

### Conversion Compatibility

**Before**:
```go
underlay.Spec.Nics[0]  // Assumes exactly one
```

**After**:
```go
for _, nic := range underlay.Spec.Nics {
    // Handle each interface
}
```

**Testing**:
- Existing single-interface configs must pass all tests unchanged
- E2E test: Deploy old single-interface YAML, verify works
- E2E test: Update single→multi, verify smooth transition

---

## Summary

**Core Changes**:
1. Remove validation enforcing single Underlay per node (conversion layer)
2. Remove assumption `Nics[0]` in host conversion
3. Add uniqueness validation for neighbor addresses and nic names
4. Support iterating multiple neighbors in FRR config generation
5. Support iterating multiple nics in host network setup
6. Add hot-apply vs restart decision logic

**Data Integrity**:
- Webhook ensures no duplicate neighbors/nics within an Underlay
- Node selector validation ensures no Underlay overlap per node
- ASN validation ensures eBGP requirements met
- EVPN XOR validation ensures consistent VTEP configuration

**Performance Considerations**:
- No database - all in etcd via Kubernetes
- Validation performance: O(N×M) where N=underlays, M=nodes (acceptable for cluster scale)
- Neighbor iteration: O(N) where N=neighbors per underlay (expected <20, fast)
