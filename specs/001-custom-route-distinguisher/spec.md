# Feature Specification: Configurable RD Assigned Numbers

**Feature Branch**: `distinguisher`

**Created**: 2026-10-01

**Status**: Draft

**Input**: User description: "I want to allow the user to specify its onw route distinguisher on l2 / l3 vni and l3vpn as we do with route reflectors"

## Clarifications

### Session 2026-10-01

- Q: When one L2VNI, L3VNI, or L3VPN selects several nodes, how should its user-supplied route distinguisher be applied? → A: Configure the assigned-number part; combine it with each node's router ID, as L3VPN already does for SRv6.
- Q: For L2VNI and L3VNI, should a configured RD assigned number also change the default export route target when no route targets are specified? → A: No. Keep default route targets unchanged.
- Q: If a VNI uses an RD assigned number different from its VNI, should its VNI number still be reserved against L3VPN assigned-number collisions? → A: Yes. Keep the existing number restriction and also reject duplicate effective RDs.
- Q: How should collisions with FRR's runtime-assigned automatic VNI RDs be handled? → A: Keep automatic RD behavior unchanged; reject collisions among configured assigned numbers and retain existing VNI/L3VPN number restrictions.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Set an RD for an L2VNI (Priority: P1)

An operator integrating an L2 overlay with a BGP fabric chooses the assigned-number part of the route distinguisher (RD) used for that overlay's locally originated EVPN routes. Each node combines that number with its own router ID. Routes reflected by a route reflector retain the originating node's RD.

**Why this priority**: An L2VNI is the simplest way to demonstrate that an operator-selected RD assigned number reaches the advertised route.

**Independent Test**: Configure one L2VNI with an explicit RD assigned number on two originating nodes, then observe each node's router ID paired with that number in its advertised EVPN routes, including after route reflection.

**Acceptance Scenarios**:

1. **Given** an L2VNI with a valid RD assigned number, **when** a node originates routes for that L2VNI, **then** those routes use that node's router ID and the specified number as their RD.
2. **Given** an L2VNI without an RD assigned number, **when** a node originates routes for it, **then** the RD remains automatically derived as before.
3. **Given** an L2VNI with an RD assigned number and no explicit route targets, **when** it originates routes, **then** its default route targets remain the same as before the number was configured.

---

### User Story 2 - Set an RD for an L3VNI (Priority: P2)

An operator chooses the RD assigned number for routes originated by an L3VNI while retaining its existing VNI and route-target settings.

**Why this priority**: L3VNI needs the same operator control as L2VNI, with its current routing-domain behavior preserved.

**Independent Test**: Configure one L3VNI with an RD assigned number and verify that its originated routes carry the node's router ID and that number while its VNI and route targets remain as configured.

**Acceptance Scenarios**:

1. **Given** an L3VNI with a valid RD assigned number, **when** a node originates routes for it, **then** those routes use that node's router ID and the specified number as their RD.
2. **Given** an L3VNI without an RD assigned number, **when** a node originates routes for it, **then** it retains the current automatically derived RD.
3. **Given** an L3VNI with an RD assigned number and no explicit route targets, **when** it originates routes, **then** its default route targets remain the same as before the number was configured.

---

### User Story 3 - Set an RD for an L3VPN (Priority: P3)

An operator uses the existing `rdAssignedNumber` setting on an L3VPN to choose the variable part of its RD. Each node pairs that number with its own router ID.

**Why this priority**: L3VPN provides the established behavior that L2VNI and L3VNI should follow; the feature should keep its routes and route targets compatible.

**Independent Test**: Configure one L3VPN with its required RD assigned number and verify that its originated VPN routes carry the node's router ID and that number, while its route-target behavior is unchanged.

**Acceptance Scenarios**:

1. **Given** an L3VPN with a valid `rdAssignedNumber`, **when** a node originates VPN routes, **then** those routes use that node's router ID and the specified number as their RD.
2. **Given** an L3VPN with no explicit export route targets, **when** it originates routes, **then** the existing default export route target remains based on its assigned number.
3. **Given** one L2VNI, L3VNI, or L3VPN selecting two nodes with different router IDs, **when** both nodes originate routes, **then** each uses its own router ID with the configured assigned number, yielding distinct RDs.

### Edge Cases

- An empty, malformed, or out-of-range RD assigned number is rejected with a clear error identifying the affected resource.
- Two resources on the same node have the same configured RD assigned number: the combination is rejected, including a VNI colliding with an L3VPN.
- An automatically derived VNI RD happens to match a configured RD: admission cannot predict FRR's runtime allocation, so this combination is not pre-rejected by the new validation. Operators can inspect the observed RD when diagnosing a conflict.
- An L3VPN assigned number equals an L2VNI or L3VNI VNI on the same node: the existing restriction still rejects the combination even if the VNI's configured RD assigned number differs.
- An operator updates or removes an optional L2VNI or L3VNI assigned number: subsequently originated routes use the new number or the previous automatic derivation, respectively.
- A resource selects multiple originating nodes: each node uses its own router ID, so the configured assigned number can be shared without giving those nodes the same RD.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: Operators MUST be able to set an optional RD assigned number on each L2VNI and L3VNI, using the same concept as the existing required `rdAssignedNumber` on L3VPN.
- **FR-002**: When an RD assigned number is configured, locally originated routes for that resource MUST use the originating node's router ID as the RD administrator part and the configured number as its assigned-number part.
- **FR-003**: When the optional number is absent from an L2VNI or L3VNI, that resource MUST retain its current automatic RD derivation without a change to existing configurations. L3VPN MUST retain its existing required-number behavior.
- **FR-004**: The system MUST accept RD assigned numbers from 1 through 65535 and reject absent, malformed, or out-of-range values where the number is required, or malformed and out-of-range values where it is optional.
- **FR-005**: The system MUST reject duplicate configured RD assigned numbers among resources originating routes on the same node, including L3VPNs. Existing VNI uniqueness and VNI-versus-L3VPN assigned-number restrictions MUST remain in force even if a VNI's configured RD assigned number differs from its VNI. Automatically derived VNI RDs remain managed by FRR and are outside the new pre-admission collision check.
- **FR-006**: Configuring an RD assigned number on an L2VNI or L3VNI MUST NOT change its explicit or default import or export route targets, VNI, VRF, or node selection.
- **FR-007**: An L3VPN's existing assigned number MUST continue to determine its default export route target as well as its RD.
- **FR-008**: Updating or removing an optional L2VNI or L3VNI RD assigned number MUST update subsequent local route advertisements to use the new configured or automatically derived RD.
- **FR-009**: Routes carrying a configured RD assigned number MUST remain identifiable by their originating node's RD when distributed through an existing route reflector; no RD setting on the reflector is required.
- **FR-010**: Operator documentation MUST show the RD assigned number for L2VNI, L3VNI, and L3VPN and explain automatic fallback, route-target independence for VNIs, and same-node uniqueness rules.
- **FR-011**: A resource selected on multiple nodes MUST use the same configured assigned number with each node's own router ID, yielding distinct RDs when those router IDs differ.

### Key Entities

- **L2VNI**: Layer 2 overlay with a VNI, route targets, node selection, and an optional RD assigned number for originated EVPN routes.
- **L3VNI**: Layer 3 overlay with a VNI, routing domain, route targets, node selection, and an optional RD assigned number.
- **L3VPN**: Layer 3 VPN with an existing required RD assigned number, route targets, and node selection.
- **Route distinguisher**: A BGP identifier composed here from the originating node's router ID and an assigned number; it is separate from route targets, which govern import and export policy.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: In each of the three supported resource types, an operator can set one valid RD assigned number and observe that number paired with the originating node's router ID on 100% of the resource's originated routes.
- **SC-002**: Existing VNI examples without a configured RD assigned number retain their previously observed RDs and route targets in 100% of regression checks; configured examples retain their previous default route targets in 100% of checks.
- **SC-003**: 100% of invalid assigned-number examples, duplicate configured numbers on one node, and existing VNI-versus-L3VPN numeric collisions in the acceptance checks are rejected with an error identifying the affected resource.
- **SC-004**: An operator can use the documented examples to configure an RD assigned number for each of the three resource types without additional guidance.
- **SC-005**: In a test where one resource selects two originating nodes with different router IDs, 100% of its observed routes carry distinct RDs containing the same configured assigned number.

## Assumptions

- The requested new setting is the assigned-number part for L2VNI and L3VNI. L3VPN already provides this control through its required `rdAssignedNumber`.
- The new VNI setting is optional and leaves existing VNI resources with their current RD behavior unless a number is supplied.
- The existing L3VPN assigned number remains required and continues to supply its default export route target.
- Existing VNI-versus-L3VPN number reservations remain in place because default route targets may still use those numbers.
- FRR allocates automatic VNI RD suffixes at runtime; this feature preserves that behavior and validates only configured assigned-number collisions ahead of time.
- Route targets and RDs serve different purposes; the confirmed behavior is that changing the VNI RD assigned number leaves explicit and default route targets unchanged.
- The route reflector reference describes the deployment in which users want to observe these RDs; this feature does not add a new RD setting to the route reflector.
- Router IDs are unique per node in the target deployment, so pairing each router ID with the configured assigned number yields distinct RDs across selected nodes.
