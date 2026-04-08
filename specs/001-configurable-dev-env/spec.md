# Feature Specification: Configurable Development Environment

**Feature Branch**: `001-configurable-dev-env`
**Created**: 2026-02-23
**Status**: Draft
**Input**: User description: "Configurable development environment with declarative topology configuration system for containerlab-based testing"

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Declarative Topology Configuration (Priority: P1)

As a developer or test engineer, I want to define network topology behavior at a high level using a declarative configuration file, so that I can set up complex network environments without manually specifying low-level details like IP addresses, MAC addresses, and VTEP assignments.

**Why this priority**: This is the core capability that all other features depend on. Without the ability to declare topology intent and have the system generate configurations, no other functionality is possible.

**Independent Test**: Can be fully tested by providing a topology configuration file alongside a containerlab topology file and verifying that the system generates correct FRR configurations, IP allocations, and setup scripts for all nodes.

**Acceptance Scenarios**:

1. **Given** a containerlab topology file defining nodes and links, and an environment configuration file defining node roles and BGP settings, **When** the user runs the configuration tool, **Then** all point-to-point links receive automatically allocated IP addresses (IPv4 /31 and IPv6 /127 subnets) from configurable base ranges.
2. **Given** an environment configuration with edge-leaf nodes declaring VRFs, **When** the configuration is applied, **Then** each edge leaf receives a unique VTEP IP, unique MAC addresses for VXLAN interfaces, and correct FRR configuration for the declared VRFs and VNIs.
3. **Given** an environment configuration with pattern-based node matching (e.g., `leaf[AB]`), **When** the configuration is applied, **Then** all nodes matching the pattern receive the same role and behavioral configuration with unique resource allocations.

---

### User Story 2 - Configuration Summary Output (Priority: P2)

As a developer, I want to see a human-readable summary of all applied configurations after generation, so that I can verify the topology is configured correctly and use it as documentation.

**Why this priority**: Provides essential feedback for verifying correctness and debugging issues. Without visibility into what was generated, users cannot validate the configuration.

**Independent Test**: Can be tested by running the configuration tool and verifying the summary output includes per-node details (IPs, interfaces, VRFs, BGP peers) and a topology overview.

**Acceptance Scenarios**:

1. **Given** a configuration has been applied, **When** the user views the summary, **Then** it displays the topology overview (node count, link count, patterns matched), per-node details (role, router ID, VTEP IP, interfaces, VRFs, BGP peers), and resource allocation summary.
2. **Given** a previously applied configuration with a persisted state file, **When** the user requests the summary without re-running generation, **Then** the summary is regenerated from the saved state.
3. **Given** configuration issues or edge cases exist, **When** the summary is displayed, **Then** warnings and errors are clearly listed.

---

### User Story 3 - Configuration Introspection (Priority: P2)

As a test engineer, I want to programmatically query the generated topology configuration, so that end-to-end tests can dynamically discover topology parameters instead of using hardcoded values.

**Why this priority**: Decouples tests from specific topology implementations, enabling topology changes without requiring test modifications. This is critical for maintainability of the test suite.

**Independent Test**: Can be tested by loading a generated configuration state and querying for specific node properties (VTEP IPs, link IPs, interface assignments) and verifying correct values are returned.

**Acceptance Scenarios**:

1. **Given** a generated topology state, **When** a test queries for a node's VTEP IP by node name, **Then** the correct allocated VTEP IP is returned.
2. **Given** a generated topology state, **When** a test queries for the IP address of a link between two specific nodes, **Then** the correct allocated link IP (IPv4 or IPv6) is returned.
3. **Given** a generated topology state, **When** a query is made with an IP address, **Then** the system returns which node and interface owns that IP (reverse lookup).
4. **Given** a generated topology state, **When** a query is made with a node pattern (e.g., `leaf.*`), **Then** all matching nodes are returned.

---

### User Story 4 - Machine-Readable Output (Priority: P3)

As a developer or CI/CD pipeline maintainer, I want to retrieve the configuration summary in a structured format, so that I can integrate topology information into automated workflows and scripts.

**Why this priority**: Enables automation and tooling integration. Lower priority because the programmatic query interface (User Story 3) covers most automation use cases.

**Independent Test**: Can be tested by requesting the summary in structured output format and validating the output parses correctly and contains all topology information.

**Acceptance Scenarios**:

1. **Given** a generated topology state, **When** the user requests the summary in structured format, **Then** a valid, parseable output is returned containing all node, link, and resource information.

---

### User Story 5 - Multiple Topology Variations (Priority: P3)

As a developer, I want to maintain multiple logical configurations for the same physical topology, so that I can test different network behaviors (e.g., EVPN vs SRv6) without recreating the containerlab topology.

**Why this priority**: Supports the long-term extensibility goal but is not required for initial functionality.

**Independent Test**: Can be tested by applying two different environment configuration files to the same containerlab topology and verifying each produces distinct, correct configurations.

**Acceptance Scenarios**:

1. **Given** a single containerlab topology file and two different environment configuration files, **When** each configuration is applied separately, **Then** distinct valid configurations are generated for each, sharing the same physical topology but differing in logical behavior.
2. **Given** multiple environment configurations exist, **When** a specific configuration is applied, **Then** only that configuration's state is active and queryable.

---

### Edge Cases

- What happens when a pattern in the environment configuration matches no nodes in the containerlab topology?
- When a containerlab topology contains nodes not covered by any pattern in the environment configuration, the system MUST emit a warning identifying the unmatched nodes but proceed with configuration generation for all matched nodes.
- When two patterns in the environment configuration overlap and match the same node, the system MUST reject the configuration with an error identifying the conflicting patterns and the affected node(s).
- How does the system behave when the IP address range is exhausted for the configured subnet?
- What happens when the environment configuration references an interface name not present in the containerlab topology?
- How does the system handle re-running configuration generation when a state file already exists (idempotency)?

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: System MUST read a containerlab topology file to discover nodes, their types, and physical links between them.
- **FR-002**: System MUST read an environment configuration file that defines node roles, BGP settings, VRF declarations, and IP allocation ranges.
- **FR-003**: System MUST classify nodes as either "edge" (host tunnel endpoints, manage VRFs) or "transit" (passthrough routers) based on configuration.
- **FR-004**: System MUST automatically allocate IPv4 (/31) and IPv6 (/127) addresses for all point-to-point links from configurable base ranges.
- **FR-005**: System MUST automatically allocate IPv4 (/24) and IPv6 (/64) subnets for switch/broadcast network segments.
- **FR-006**: System MUST automatically assign unique VTEP IPs to edge-leaf nodes from a dedicated address range.
- **FR-007**: System MUST automatically generate unique MAC addresses for VXLAN interfaces using locally administered format.
- **FR-008**: System MUST automatically assign unique BGP router IDs to all BGP-enabled nodes.
- **FR-009**: System MUST apply configuration to node groups using pattern matching (e.g., `leaf[AB]`, `spine`, `leaf.*`).
- **FR-010**: System MUST generate FRR configuration files for each router node based on the declared configuration.
- **FR-011**: System MUST generate node-specific setup scripts for host-level configuration (e.g., VXLAN interfaces).
- **FR-012**: System MUST persist allocated resources to a state file to support idempotent operations and introspection.
- **FR-013**: System MUST provide a human-readable configuration summary after applying configuration, showing topology overview, per-node details, and resource allocations.
- **FR-014**: System MUST provide a structured output format for the configuration summary for machine consumption.
- **FR-015**: System MUST support querying the topology state for node properties (VTEP IP, link IPs, interfaces) by node name.
- **FR-016**: System MUST support reverse lookup of IP addresses to identify the owning node and interface.
- **FR-017**: System MUST support querying nodes by pattern matching.
- **FR-018**: System MUST produce idempotent results — re-running configuration generation with the same inputs MUST produce the same resource allocations.
- **FR-019**: System MUST provide a programmatic query interface for use in automated tests to dynamically discover topology parameters.
- **FR-020**: System MUST report warnings when configuration patterns match no nodes or when nodes are unmatched by any pattern.
- **FR-021**: System MUST report errors when the IP address pool is exhausted or when referenced interfaces do not exist in the topology.
- **FR-022**: System MUST reject the configuration with an error if any node matches more than one pattern, identifying the conflicting patterns and affected node(s).

### Key Entities

- **Node**: A network device in the topology (router, switch, host) with a name, role, and set of interfaces. Can be classified as edge or transit.
- **Link**: A connection between two nodes, consisting of two interfaces. Can be point-to-point (between routers) or broadcast (via a switch).
- **VRF**: A virtual routing and forwarding instance declared on edge nodes, with a name, VNI, and set of associated interfaces.
- **BGP Peer**: A BGP adjacency between two nodes, characterized by ASN, address family support (IPv4/IPv6/EVPN), and optional BFD enablement.
- **Topology State**: The persisted record of all resource allocations (IPs, MACs, router IDs) and configuration decisions, enabling introspection and idempotent re-generation.
- **Environment Configuration**: The declarative file defining logical network behavior — node roles, routing, VRFs, and IP ranges.
- **Containerlab Topology**: The file defining physical infrastructure — nodes, links, container images, and bind mounts.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Developers can set up a complete multi-node network topology with full routing configuration by editing a single declarative configuration file, without manually specifying any IP addresses, MAC addresses, or router IDs.
- **SC-002**: Adding a new topology variation (e.g., a new test scenario) requires creating only one new configuration file, with no duplication of IP allocation logic or FRR template code.
- **SC-003**: End-to-end tests contain zero hardcoded topology parameters (IP addresses, VTEP IPs, interface names) — all values are obtained through the query interface.
- **SC-004**: Re-running configuration generation on an unchanged topology and configuration produces identical output 100% of the time (idempotency).
- **SC-005**: A developer unfamiliar with the topology can understand the deployed configuration within 5 minutes by reading the configuration summary output.
- **SC-006**: The configuration summary output accurately reflects the actual applied configuration for 100% of nodes, interfaces, and resource allocations.
- **SC-007**: The system correctly handles topologies with at least 10 nodes and 20 links without allocation conflicts or errors.

## Clarifications

### Session 2026-02-23

- Q: What happens when two patterns overlap and match the same node? → A: Configuration error — reject with an error identifying conflicting patterns and affected nodes.
- Q: How should the system handle containerlab nodes not covered by any configuration pattern? → A: Warn and skip — emit a warning for unmatched nodes but proceed with generation for matched nodes.

## Assumptions

- The containerlab topology file format (.clab.yml) is stable and the system can rely on its structure for node and link discovery.
- Pattern matching uses standard regular expression or glob syntax consistent with the existing containerlab naming conventions.
- IPv4 and IPv6 dual-stack is required for all link allocations.
- The state file format can evolve between versions; backward compatibility of state files is not required for the initial release.
- Graph-based output (Mermaid, ASCII art) is explicitly out of scope for this feature and will be addressed in a follow-up.
- SRv6 transport support is a future use case that the architecture should accommodate but does not need to implement.

## Scope Boundaries

### In Scope

- Declarative environment configuration file format and parsing
- Automatic IP (v4/v6), VTEP, MAC, and router ID allocation
- FRR configuration generation for edge and transit nodes
- Node setup script generation
- Pattern-based node matching and configuration application
- Configuration summary output (human-readable and structured)
- State persistence and idempotent re-generation
- Query interface for programmatic access (both CLI and library)
- Integration path for e2e tests to use the query interface

### Out of Scope

- Graph-based topology visualization (Mermaid, ASCII art) — planned as a follow-up
- SRv6 transport implementation — future enhancement
- Containerlab topology file generation (the .clab.yml is authored manually)
- Runtime monitoring or operational management of deployed topologies
- Multi-user or collaborative topology editing
