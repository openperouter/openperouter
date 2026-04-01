# Research: Support Multiple Underlay Interfaces and Neighbors

**Feature**: 006-multi-underlay-neighbors  
**Date**: 2026-04-01  
**Status**: Complete

## Research Areas

### 1. Kubernetes CRD Array Validation Patterns

**Decision**: Use native kubebuilder validation markers for array constraints

**Rationale**:
- Kubernetes CRDs natively support array fields with validation
- Current `Neighbors []Neighbor` field already exists but has `MinItems=1` validation
- Current `Nics []string` field already exists with pattern/length validation
- Validation logic erroneously enforces single-entity in conversion layer, not CRD schema

**Implementation Approach**:
- Remove `MinItems=1` constraint from Neighbors field (or keep if requiring at least one)
- Keep existing pattern/length validators on individual array items
- Remove conversion-layer check `if len(underlays) > 1` in `validate_underlay.go:32-33`
- Remove host conversion assumption `underlay.Spec.Nics[0]` in `host_conversion.go:40`
- Validate uniqueness of interface names and neighbor addresses in webhook validation

**Alternatives Considered**:
- Custom admission webhook only: More complex, loses declarative validation benefits
- Separate CRDs per interface/neighbor: Over-engineering, poor UX for operators

**References**:
- Existing kubebuilder markers in `api/v1alpha1/underlay_types.go`
- Kubernetes API conventions for list fields

---

### 2. FRR Configuration with Multiple BGP Neighbors

**Decision**: FRR natively supports multiple BGP neighbors - iterate and generate config per neighbor

**Rationale**:
- FRR BGP configuration already supports unlimited neighbors via repeated `neighbor` statements
- Current code at `frr_conversion.go:44-57` already iterates `underlay.Spec.Neighbors` array
- No FRR-side limitation exists - constraint is purely in validation layer
- Each neighbor gets independent BGP session with own timers, passwords, filters

**Implementation Approach**:
- No changes needed to FRR template generation logic (already loops over neighbors)
- Verify BFD profile generation handles multiple neighbors correctly
- Ensure router-id assignment works with multiple interfaces
- Test EVPN address-family configuration with multiple neighbors

**FRR Config Pattern**:
```
router bgp 65001
 bgp router-id 10.0.0.1
 neighbor 192.168.1.1 remote-as 65002
 neighbor 192.168.1.1 description TOR-1
 neighbor 192.168.2.1 remote-as 65002
 neighbor 192.168.2.1 description TOR-2
 !
 address-family ipv4 unicast
  neighbor 192.168.1.1 activate
  neighbor 192.168.2.1 activate
 exit-address-family
 !
 address-family l2vpn evpn
  neighbor 192.168.1.1 activate
  neighbor 192.168.2.1 activate
 exit-address-family
!
```

**Alternatives Considered**:
- Peer groups: Over-optimization for initial implementation, can add later
- Dynamic neighbors: Not needed, static config is standard

---

### 3. Multiple Physical Interfaces for Underlay

**Decision**: Move interfaces to Linux network namespaces, configure with netlink

**Rationale**:
- Current code at `host_conversion.go:40` only uses first interface `Nics[0]`
- Physical interfaces moved to router namespace via netlink for isolation
- Each interface needs independent IP configuration
- Interfaces can share BGP sessions (many-to-many relationship per spec clarification)

**Implementation Approach**:
- Update `hostnetwork.UnderlayParams` to store multiple interfaces (currently single string)
- Iterate `underlay.Spec.Nics` array and move each to target namespace
- Configure IP addresses on each interface (may need CIDR per interface or DHCP/existing config)
- FRR listens on all interfaces in namespace automatically

**Network Namespace Pattern**:
```go
for _, nic := range underlay.Spec.Nics {
    // Move interface to namespace
    link, _ := netlink.LinkByName(nic)
    netlink.LinkSetNsFd(link, targetNsFd)
    // Configure in target NS
    netlink.LinkSetUp(link)
}
```

**Alternatives Considered**:
- VLANs on single interface: Doesn't match physical topology use case
- Bonds/aggregation: Different feature, not multi-path

**Open Questions Resolved**:
- Q: Do interfaces need explicit IP config in CRD?
- A: No, assume pre-configured or DHCP - matches current single-interface behavior
- Q: Routing between interfaces?
- A: FRR handles routing, no special kernel routes needed

---

### 4. Hot-Reload vs Restart Decision Criteria

**Decision**: Restart only for initial setup or structural changes; runtime additions are hot-applied

**Rationale** (from spec clarification):
- Current behavior: any underlay change triggers router namespace/container restart
- Optimization goal: minimize service disruption for additive changes
- FRR supports dynamic neighbor addition via `vtysh` commands
- Interface additions require namespace manipulation but can be done live

**Implementation Approach**:

**Restart Required**:
- Initial deployment (no existing router namespace)
- Removing all interfaces or neighbors (teardown scenario)
- Changing fundamental parameters (ASN, router-id CIDR allocation)
- Major EVPN reconfigurations (changing VTEP allocation method)

**Hot-Apply Supported**:
- Adding new interfaces to existing configuration
- Adding new neighbors to existing configuration
- Modifying neighbor parameters (BGP timers, passwords) - can reload via FRR

**Decision Logic**:
```go
func requiresRestart(old, new *Underlay) bool {
    if old == nil {
        return true  // Initial setup
    }
    if old.Spec.ASN != new.Spec.ASN {
        return true  // ASN change
    }
    if len(new.Spec.Neighbors) == 0 && len(new.Spec.Nics) == 0 {
        return true  // Teardown
    }
    // Additions/modifications can be hot-applied
    return false
}
```

**FRR Hot-Reload**:
- Use `vtysh -c "configure" -c "router bgp ..." -c "neighbor ...add"` for neighbor additions
- Use `vtysh -c "clear bgp * soft"` for parameter changes
- Monitor FRR config diff and apply incrementally

**Alternatives Considered**:
- Always restart: Simpler but poor operational experience
- Always hot-apply: Risky for complex changes, hard to validate
- User-controlled flag: Extra complexity, spec says system decides

---

### 5. Containerlab E2E Test Topologies

**Decision**: Use containerlab 0.74.1+ with group support for multi-interface scenarios

**Rationale**:
- Current codebase uses containerlab (version 0.74.1 per git commits)
- Containerlab supports multiple links between nodes
- Recent version (0.74.1) added group support for organizing complex topologies
- Existing E2E tests in `e2etests/` use Ginkgo framework

**Test Topology Pattern**:
```yaml
name: multi-underlay-test
topology:
  nodes:
    router1:
      kind: linux
      image: frrouting/frr:latest
    tor1:
      kind: linux  
      image: frrouting/frr:latest
    tor2:
      kind: linux
      image: frrouting/frr:latest
      
  links:
    # Multiple interfaces from router1 to different TORs
    - endpoints: ["router1:eth1", "tor1:eth1"]
    - endpoints: ["router1:eth2", "tor2:eth1"]
    - endpoints: ["router1:eth3", "tor1:eth2"]  # Redundant path
```

**E2E Test Scenarios**:
1. Deploy topology with 3 interfaces, 4 neighbors
2. Verify all BGP sessions establish
3. Verify data plane connectivity across each interface
4. Add new interface/neighbor dynamically
5. Verify no restart occurred (check container uptime)
6. Remove neighbor, verify cleanup

**Implementation Approach**:
- Create new `.clab.yml` files in `clab/` directory for multi-interface scenarios
- Add Ginkgo test specs in `e2etests/tests/` for multi-entity validation
- Use existing helpers in `e2etests/pkg/` for FRR-K8s integration
- Validate webhook rejection of invalid configs (duplicate IDs, etc.)

**Alternatives Considered**:
- Kind clusters with real network interfaces: More realistic but harder to automate in CI
- Simulated interfaces (veth pairs): Doesn't test real interface movement to namespaces

---

### 6. Backward Compatibility Strategy

**Decision**: Existing single-interface/neighbor configs are valid subsets of new multi-entity schema

**Rationale**:
- Current CRD already uses `[]Neighbor` and `[]string` for arrays
- Single-element arrays are valid arrays
- No schema migration needed - purely additive change
- Existing validation will continue to work

**Validation**:
- Keep `MinItems=1` on Neighbors to require at least one neighbor (per spec clarification: empty rejected)
- Remove conversion-layer single-entity enforcement
- Test existing single-interface configs remain valid

**Migration Path**:
- No user action required
- Existing Underlay resources continue to work unchanged
- Users can update to multi-entity by editing existing resources

**Compatibility Test Plan**:
- E2E test with existing single-interface/neighbor YAML
- Verify no changes needed to config
- Verify update from single to multiple works
- Verify downgrade from multiple to single works

**Alternatives Considered**:
- New CRD version (v1alpha2): Unnecessary complexity, no breaking changes
- Deprecation period: Not needed, purely additive

---

## Summary of Technical Decisions

| Area | Decision | Impact |
|------|----------|--------|
| CRD Validation | Use kubebuilder array validators | Remove conversion-layer checks |
| FRR Config | Iterate neighbors, generate per-neighbor config | No FRR template changes needed |
| Multiple Interfaces | Move all interfaces to namespace via netlink | Update host conversion to loop Nics array |
| Hot-Reload | Restart for structural, hot-apply for additive | Add decision logic in reconciler |
| E2E Testing | Containerlab multi-link topologies | New .clab.yml files and Ginkgo tests |
| Backward Compat | Single-entity configs are valid subsets | No migration needed |

**Next Steps**: Proceed to Phase 1 (data model and contracts definition)
