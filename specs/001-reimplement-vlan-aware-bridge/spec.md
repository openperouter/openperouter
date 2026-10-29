# Feature Specification: Reimplement Existing VNIs with VLAN-Aware Bridges

**Feature Branch**: `main` (existing branch; no branch-creation hook configured)

**Created**: 2026-10-27

**Status**: Draft

**Input**: User description: "Reimplement the current API only using a VLAN-aware bridge with an internal VLAN tag. No API change, no new feature, and no trunk or user-facing VLAN-tag support. The current e2e tests must run with the new implementation."

**Reference**: [L2VNI VLAN Trunks proposal](https://github.com/fedepaol/openperouter/blob/l2vnitagged/enhancements/l2vni-vlan-tag.md), limited to “Reimplementing the Current API,” “Standalone L2VNIs and L3VNIs,” and implementation step 1. Trunk proposals and alternatives are excluded.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Keep Existing Workload Attachments Working (Priority: P1)

As a cluster operator, I apply existing L2VNI configuration and connect workloads through the same host interfaces, without configuring VLANs or changing manifests.

**Why this priority**: Preserving existing workload connectivity is the primary compatibility requirement.

**Independent Test**: Apply existing standalone L2VNI scenarios and verify host attachments and cross-node connectivity using the current e2e tests.

**Acceptance Scenarios**:

1. **Given** an L2VNI without a routing domain, **When** workloads attach on different nodes, **Then** they retain existing Layer 2 connectivity without requiring a gateway or routing domain.
2. **Given** an L2VNI using a managed or external Linux or OVS host bridge, **When** reconciled, **Then** its dedicated host attachment retains its existing name, ownership behavior, and untagged traffic.
3. **Given** an L2VNI without `hostMaster`, **When** reconciled, **Then** its dedicated host interface remains available for external attachment with the same untagged behavior.

---

### User Story 2 - Keep Existing Routing and Gateways Working (Priority: P1)

As a cluster operator, I retain routed connectivity, gateways, and routing policy when the internal bridge model changes.

**Why this priority**: Existing deployments also depend on Layer 3 services.

**Independent Test**: Run existing routed EVPN scenarios, including multiple L2VNIs sharing an L3VNI and L2VNIs attached to an L3VPN routing domain.

**Acceptance Scenarios**:

1. **Given** an L2VNI referencing an L3VNI or L3VPN with gateway addresses, **When** workloads send traffic, **Then** gateway reachability and existing east/west and north/south routing remain functional.
2. **Given** multiple L2VNIs referencing the same L3VNI, **When** workloads communicate between subnets, **Then** existing routing and separation of their Layer 2 domains are preserved.
3. **Given** existing route targets, route distinguisher settings, VXLAN ports, and underlay address-family selections, **When** reconciled, **Then** effective routing and encapsulation behavior remains unchanged.

---

### User Story 3 - Retain Lifecycle and Recovery Behavior (Priority: P2)

As a maintainer, I validate the replacement through the current e2e suite, including cleanup and recovery, without adapting tests to a new feature or weakening their assertions.

**Why this priority**: Reliability must extend to reconciliation and recovery scenarios already supported.

**Independent Test**: Run existing restart, bridge-refresh, workload-migration, and resource lifecycle scenarios with current expectations.

**Acceptance Scenarios**:

1. **Given** configured VNIs, **When** reconciliation repeats or components restart as exercised by existing tests, **Then** forwarding and advertisements recover within those tests' existing deadlines, without duplicate configuration.
2. **Given** multiple VNIs, **When** one is deleted and recreated, **Then** its owned resources are cleaned up and recreated without disrupting unrelated VNIs or deleting externally owned host bridges.
3. **Given** the replacement and current e2e suite, **When** run in each currently supported deployment mode with its normal filters, **Then** all applicable tests pass without changing manifests, assertions, timeouts, or skips to accommodate the replacement.

### Edge Cases

- An L2VNI has neither routing domain nor gateway addresses: preserve disconnected Layer 2 forwarding.
- Dedicated bridges reuse the same internal VLAN ID: preserve distinct forwarding domains and VNI mappings.
- L2VNIs and L3VNIs use the same VXLAN UDP port: preserve coexistence and forwarding.
- A VNI exceeds 4094: preserve the existing VNI range; do not equate VNI numbers with internal VLAN IDs.
- Gateways use IPv4, IPv6, or dual-stack addresses, or a VNI selects an IPv6 underlay: preserve supported connectivity and packet-size behavior.
- A referenced routing-domain resource is missing: preserve existing validation and failure reporting.
- Reconciliation resumes after partial setup or restart: restore desired state without forwarding into an unintended VNI.
- A silent workload or a workload moving between nodes relies on neighbor/route refresh: preserve reachability and route correctness.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The system MUST preserve public configuration and status contracts, including resource kinds, fields, defaults, validation, and immutability. Existing Kubernetes manifests and static configuration MUST require no changes.
- **FR-002**: The Linux VNI implementation MUST use VLAN-aware bridges with an internal VLAN mapped to the configured VNI for both existing L2VNIs and L3VNIs. This is the required internal change.
- **FR-003**: Standalone L2VNIs MUST retain dedicated bridges, VXLAN devices, and host veth pairs; L3VNIs MUST retain dedicated bridges and VXLAN devices. VXLAN devices MUST use the reference proposal's `external vnifilter` model. These are the bounded architectural constraints of this reimplementation.
- **FR-004**: L2VNI host attachments MUST carry untagged traffic in both directions. VLAN classification MUST remain internal to the router namespace and require no workload or host VLAN configuration.
- **FR-005**: Internal VLAN IDs MUST remain invisible to public configuration and status, MAY be reused on separate dedicated bridges, and MUST NOT require node-wide allocation or reduce the accepted VNI range.
- **FR-006**: Existing `hostMaster` behavior MUST remain unchanged for managed and external Linux and OVS bridges and omitted `hostMaster`, including host interface names, attachment, and ownership-aware cleanup.
- **FR-007**: The system MUST preserve disconnected L2VNI forwarding and existing integration with L3VNI and L3VPN routing domains, including gateways, anycast behavior, and routing-domain isolation.
- **FR-008**: The system MUST preserve route advertisement/import policy, route targets, route distinguisher settings, node selection, VXLAN port selection, and underlay address-family selection, including L2VNI/L3VNI coexistence on the same VXLAN port.
- **FR-009**: Reconciliation, deletion, recreation, component recovery, and workload movement MUST preserve behavior and deadlines asserted by existing tests. Removing one VNI MUST NOT disrupt another or remove externally owned host bridges.
- **FR-010**: The replacement MUST pass the current e2e suite with existing manifests, assertions, timeouts, and deployment-mode filters. New skips, relaxed expectations, or rewritten scenarios MUST NOT establish compatibility.
- **FR-011**: The change MUST NOT introduce trunk resources or references, user-selected VLAN tags, shared host trunks, pooled VLAN allocation, shared bridges across VNIs, or other new public capabilities.

### Key Entities

- **Existing L2VNI**: A workload Layer 2 domain identified by VNI, with optional host attachment, routing-domain reference, gateways, and routing policy. Its public definition remains unchanged.
- **Existing L3VNI**: A routed overlay associated with a VRF and routing policy. Its public definition and relationships remain unchanged.
- **Internal VLAN association**: Router-private classification connecting a dedicated bridge domain to its existing VNI; not a new user-managed entity.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: 100% of applicable current e2e tests pass under existing deployment-mode filters and deadlines, with zero compatibility-driven changes to manifests, assertions, timeouts, or skips.
- **SC-002**: Operators use existing manifests and static configuration with zero new fields, resource kinds, or configuration steps; public configuration and status schemas have zero changes.
- **SC-003**: All existing connectivity, routing-policy, packet-size, isolation, and recovery expectations exercised by the current suite are satisfied with the replacement enabled.
- **SC-004**: All existing host-attachment choices remain usable without configuring VLANs, and traffic captured at host-facing VNI attachments contains zero VLAN headers introduced by the router.

## Assumptions

- The baseline is the current checkout's API, static configuration, and e2e suite. Coverage means all tests applicable under each deployment mode's existing filters, without newly added exclusions.
- This is an internal replacement. A later trunk proposal does not justify public API changes or additional abstractions here.
- The target is the Linux bridge/VXLAN path used by existing pod and host deployment modes. A new Grout dataplane design is not requested; existing supported behavior must remain intact.
- Validation depends on an environment supporting the selected Linux VXLAN model. Confirm kernel and routing-stack prerequisites during planning; the reference proposal states a minimum Linux kernel version of 5.18.
- Exact internal VLAN values, gateway interface layout, reconciliation mechanics, and configuration plumbing are planning decisions within FR-002 through FR-005.
- Existing restart and lifecycle behavior is required. Hitless conversion of a running legacy dataplane and automated rollback across software versions are not additional acceptance requirements.
- No new performance target or benchmark project is requested. Existing packet-size and timing expectations remain the baseline.
- The checked-in constitution contains unfilled template placeholders and supplies no additional ratified project principles.
