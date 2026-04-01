# Feature Specification: Support Multiple Underlay Interfaces and Neighbors

**Feature Branch**: `006-multi-underlay-neighbors`  
**Created**: 2026-04-01  
**Status**: Draft  
**Input**: User description: "Currently the API allows us only to set up one single underlay for interface for the underlay and to define only one neighbor. I would like to remove that constraint and to allow multiple neighbors and multiple interfaces. This means that we need to relax some checks and most importantly to adopt the CI and the development environment to do this and to add end to end tests for it."

## Clarifications

### Session 2026-04-01

- Q: Maximum system limits for interfaces and neighbors? → A: No hard limit, constrained only by available system resources
- Q: Interface-Neighbor Relationship? → A: Many-to-many - each neighbor can use any interface, multiple neighbors can share an interface
- Q: Empty configuration behavior (zero interfaces or neighbors)? → A: Reject with validation error - at least one interface or neighbor required
- Q: Restart decision criteria? → A: Restart only for initial setup or structural changes; runtime additions can be hot-applied
- Q: Concurrent configuration updates handling? → A: Kubernetes reconciliation - changes are always queued

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Configure Multiple Underlay Interfaces (Priority: P1)

A network administrator needs to configure multiple underlay interfaces for a network deployment to support different network paths, redundancy, or traffic segregation. They want to define all interface configurations through the API without workarounds or separate calls.

**Why this priority**: This is the core capability that enables basic multi-interface scenarios. Without this, users cannot configure redundant paths or segregated traffic flows, which are fundamental networking requirements.

**Independent Test**: Can be fully tested by submitting an API request with multiple interface configurations and verifying all interfaces are accepted, validated, and stored correctly. Delivers immediate value for users needing multi-path configurations.

**Acceptance Scenarios**:

1. **Given** an API endpoint for underlay configuration, **When** a user submits a request with 3 different underlay interface configurations, **Then** all 3 interfaces are accepted and validated successfully
2. **Given** an API endpoint for underlay configuration, **When** a user submits a request with 2 interfaces where one has invalid parameters, **Then** the system returns a validation error identifying the specific invalid interface
3. **Given** an existing single-interface configuration, **When** a user updates it to include multiple interfaces, **Then** all interfaces are configured and the single-interface constraint no longer applies

---

### User Story 2 - Configure Multiple Neighbors (Priority: P2)

A network administrator needs to define multiple BGP neighbors or peer relationships for a network node to establish connections with several other nodes in the network topology. They want to configure all neighbor relationships in a single coherent request.

**Why this priority**: Essential for real-world network topologies where nodes connect to multiple peers. This is slightly lower priority than interfaces because it depends on having the underlying interface infrastructure in place.

**Independent Test**: Can be fully tested by submitting an API request with multiple neighbor configurations and verifying all neighbors are accepted, validated, and can be referenced correctly. Delivers value for users building multi-peer topologies.

**Acceptance Scenarios**:

1. **Given** an API endpoint for neighbor configuration, **When** a user submits a request with 5 different neighbor configurations, **Then** all 5 neighbors are accepted and validated successfully
2. **Given** an API endpoint for neighbor configuration, **When** a user submits a request with duplicate neighbor identifiers, **Then** the system returns a validation error indicating the duplicate
3. **Given** an existing single-neighbor configuration, **When** a user updates it to include multiple neighbors, **Then** all neighbors are configured and the single-neighbor constraint no longer applies

---

### User Story 3 - Combined Multi-Interface Multi-Neighbor Configuration (Priority: P3)

A network administrator configures a complete network deployment with multiple underlay interfaces and multiple neighbors in a single API request, representing a real production topology with redundant paths and multiple peer connections.

**Why this priority**: This represents the complete use case but can be built incrementally after P1 and P2 are working. It validates that both features work together correctly.

**Independent Test**: Can be fully tested by submitting a single API request containing both multiple interfaces and multiple neighbors, then verifying the complete topology is configured correctly. Delivers the complete multi-entity configuration capability.

**Acceptance Scenarios**:

1. **Given** an API endpoint supporting both interfaces and neighbors, **When** a user submits a configuration with 3 interfaces and 4 neighbors, **Then** all entities are accepted, validated, and properly associated
2. **Given** a combined configuration request, **When** one interface configuration is invalid but all neighbor configurations are valid, **Then** the system returns a specific error for the invalid interface and does not apply any changes
3. **Given** a production environment, **When** a user deploys a complete topology with multiple interfaces and neighbors via the API, **Then** the deployment completes successfully and all connectivity works as specified

---

### User Story 4 - Apply Configuration Changes Without Restart (Priority: P4)

A network operator needs to add or modify interface and neighbor configurations in a production environment. They want to understand which changes will require service disruption (restart) versus which can be applied seamlessly to minimize downtime and maintain service availability.

**Why this priority**: This is an operational efficiency concern that builds on the core multi-entity capabilities. While important for production use, it can be addressed after the basic multi-entity support is working.

**Independent Test**: Can be fully tested by submitting various configuration change scenarios (add interface, remove interface, modify neighbor, etc.) and verifying the system correctly identifies which require restart and which do not. Delivers operational efficiency by minimizing service disruptions.

**Acceptance Scenarios**:

1. **Given** an existing multi-interface configuration, **When** a user adds a new interface via API, **Then** the system indicates whether this change requires restart or can be applied dynamically
2. **Given** an existing multi-neighbor configuration, **When** a user modifies a neighbor parameter, **Then** the system clearly communicates the restart requirement before applying the change
3. **Given** a running router configuration, **When** a user applies a configuration change that does not require restart, **Then** the change takes effect immediately without service interruption
4. **Given** a running router configuration, **When** a user applies a configuration change that requires restart, **Then** the user is informed of the restart requirement and can choose when to apply the change

---

### User Story 5 - Comprehensive End-to-End Testing (Priority: P2)

Development and QA teams need to validate multi-interface and multi-neighbor configurations in realistic network topologies using end-to-end tests. These tests must run in both CI and development environments to ensure the feature works correctly before production deployment.

**Why this priority**: E2E testing is critical for validating the feature works in realistic scenarios with actual network connectivity. This has P2 priority because it's essential for quality assurance but can be developed in parallel with the basic multi-entity API support (P1).

**Independent Test**: Can be fully tested by creating E2E test scenarios with multiple interfaces and neighbors, running them in CI/dev environments, and verifying all connectivity and configuration aspects work correctly. Delivers confidence in feature quality.

**Acceptance Scenarios**:

1. **Given** a CI environment with containerlab or equivalent, **When** an E2E test deploys a topology with 3 interfaces and 4 neighbors, **Then** all interfaces come up and all neighbor relationships establish successfully
2. **Given** a development environment, **When** a developer runs E2E tests locally for multi-interface scenarios, **Then** the tests complete successfully with real network connectivity verification
3. **Given** an E2E test suite, **When** tests validate data plane connectivity across all configured interfaces, **Then** traffic flows correctly through each interface path
4. **Given** E2E tests for configuration changes, **When** tests verify restart vs. no-restart scenarios, **Then** the behavior matches the documented restart requirements
5. **Given** the CI pipeline, **When** a pull request includes multi-interface/neighbor changes, **Then** E2E tests automatically validate the changes work in a realistic topology

---

### Edge Cases

- What happens when a user submits zero interfaces or neighbors (empty configuration)? → System rejects with validation error requiring at least one entity
- How does the system handle duplicate interface identifiers in a single request?
- How does the system handle duplicate neighbor identifiers in a single request?
- What happens when interface or neighbor configurations reference non-existent resources?
- How does the system validate configuration limits (e.g., maximum number of interfaces or neighbors)?
- How does the system handle partial updates when some entities are valid and others are invalid?
- What happens when a user tries to remove all interfaces or neighbors (transition back to zero)?
- How does the system handle concurrent configuration updates to the same resource? → Kubernetes reconciliation automatically queues changes for sequential processing
- Which configuration changes require router namespace/container restart vs. which can be applied without restart? → Restart required for initial setup or structural changes; runtime additions are hot-applied
- How does the system determine when adding a new interface requires restart vs. hot-add? → New interface additions during runtime are hot-applied without restart
- How does the system determine when adding a new neighbor requires restart vs. dynamic reconfiguration? → New neighbor additions during runtime are hot-applied without restart
- What happens when a user modifies existing interface or neighbor parameters - does this always require restart? → Parameter modifications are considered structural changes and may require restart
- How do E2E tests handle test environment resource limits for large numbers of interfaces and neighbors?
- What happens when E2E tests experience network connectivity issues during multi-interface testing?
- How do E2E tests validate correct interface selection for traffic flows in multi-path scenarios?
- How do E2E tests clean up test topologies to avoid state pollution between test runs?

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: API MUST accept configuration requests containing multiple underlay interface definitions
- **FR-002**: API MUST accept configuration requests containing multiple neighbor definitions
- **FR-003**: API MUST validate each underlay interface configuration independently
- **FR-004**: API MUST validate each neighbor configuration independently
- **FR-005**: API MUST enforce unique identifiers for interfaces within a single configuration request
- **FR-006**: API MUST enforce unique identifiers for neighbors within a single configuration request
- **FR-007**: API MUST maintain backward compatibility with existing single-interface configurations
- **FR-008**: API MUST maintain backward compatibility with existing single-neighbor configurations
- **FR-009**: Validation errors MUST identify which specific interface or neighbor configuration is invalid
- **FR-010**: System MUST support atomic configuration updates (all or nothing) for multi-entity requests
- **FR-011**: API MUST support combined requests containing both multiple interfaces and multiple neighbors
- **FR-012**: System MUST handle interfaces and neighbors constrained only by available system resources (no hard-coded limits)
- **FR-013**: API MUST return clear error messages when system resource constraints prevent accepting additional entities
- **FR-014**: Existing validation rules MUST be updated to work with multiple entities rather than assuming single entities
- **FR-015**: System MUST persist multiple interface and neighbor configurations correctly
- **FR-016**: System MUST clearly indicate when a configuration change requires router namespace/container restart
- **FR-017**: System MUST support hot-applying runtime additions of interfaces and neighbors without restart
- **FR-018**: System MUST require restart only for initial setup or structural changes (e.g., removing all interfaces/neighbors, major topology reconfigurations)
- **FR-019**: Configuration responses MUST inform users whether the change will take effect immediately or requires restart
- **FR-020**: End-to-end tests MUST validate multi-interface configurations in realistic network topologies
- **FR-021**: End-to-end tests MUST validate multi-neighbor configurations with actual peer connectivity
- **FR-022**: End-to-end tests MUST validate combined scenarios with multiple interfaces and multiple neighbors
- **FR-023**: CI environment MUST support running end-to-end tests with multi-interface and multi-neighbor topologies
- **FR-024**: Development environment MUST support local testing of multi-interface and multi-neighbor scenarios
- **FR-025**: End-to-end tests MUST verify data plane connectivity across all configured interfaces and neighbors
- **FR-026**: End-to-end tests MUST validate restart vs. no-restart scenarios for configuration changes
- **FR-027**: System MUST support many-to-many relationships between interfaces and neighbors (each neighbor can use any interface, multiple neighbors can share an interface)
- **FR-028**: API MUST reject configuration requests with zero interfaces and zero neighbors with a clear validation error indicating at least one entity is required
- **FR-029**: System MUST handle concurrent configuration updates via Kubernetes reconciliation loops, ensuring changes are queued and applied sequentially

### Key Entities

- **Underlay Interface**: Represents a network interface configuration for the underlay network, including interface identifier, network parameters, and association with the parent resource. Can exist in multiples per configuration. Multiple neighbors can share the same interface.
- **Neighbor**: Represents a peer relationship or BGP neighbor configuration, including neighbor identifier, connection parameters, and routing information. Can exist in multiples per configuration. Each neighbor can use any available interface (many-to-many relationship with interfaces).

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Users can successfully configure underlay interfaces limited only by available system resources
- **SC-002**: Users can successfully configure neighbors limited only by available system resources
- **SC-003**: API validation for multi-entity configurations completes in the same time frame as single-entity validations (no significant performance degradation)
- **SC-004**: 100% of existing single-interface and single-neighbor configurations continue to work without modification
- **SC-005**: End-to-end tests cover all combinations of multiple interfaces and multiple neighbors
- **SC-006**: Configuration errors are identified and reported within 2 seconds of API request submission
- **SC-007**: Zero production incidents related to multi-entity configuration validation after deployment
- **SC-008**: Users receive clear feedback about whether their configuration change requires a restart before the change is applied
- **SC-009**: Configuration changes that can be applied without restart are documented and consistently applied across all scenarios
- **SC-010**: E2E tests achieve 100% coverage of multi-interface and multi-neighbor configuration scenarios
- **SC-011**: E2E tests run successfully in CI environment for every pull request affecting interface or neighbor configuration
- **SC-012**: E2E tests verify data plane connectivity for all configured interfaces within test execution time
- **SC-013**: Development environment supports running full E2E test suite locally with consistent results to CI

## Assumptions

- Existing single-interface and single-neighbor configurations represent a valid subset of the new multi-entity capability and will continue to work unchanged
- The current API structure supports extending configuration payloads to include arrays/lists of interfaces and neighbors
- Validation logic currently checking for "exactly one" interface or neighbor can be updated to check "one or more" instead
- CI and development environments have sufficient resources to test multiple-entity scenarios
- End-to-end tests will use representative production-like topologies with multiple interfaces and neighbors
- Performance requirements remain the same for multi-entity configurations as for single-entity configurations
- System does not enforce hard-coded limits on the number of interfaces and neighbors; actual capacity depends on available system resources (memory, CPU, network stack capacity)
- Duplicate interface or neighbor identifiers within a single request are considered configuration errors
- When validation fails for any entity in a multi-entity request, no changes are applied (atomic transaction behavior)
- Currently, any underlay configuration change triggers a router namespace/container restart; this behavior will be made more selective
- Runtime additions of interfaces and neighbors can be hot-applied without requiring restart
- Restart is required only for initial setup or structural changes (e.g., removing all entities, major reconfigurations)
- Users value service continuity and prefer configuration changes that avoid restarts when safe and feasible
- Hot-apply capability for runtime additions provides operational efficiency while maintaining system stability
- CI environment has containerlab or equivalent network topology simulation capability
- Development environment can replicate CI network testing capabilities for local validation
- E2E tests can create realistic multi-node network topologies with multiple connections between nodes
- Test environments have sufficient resources (CPU, memory, network interfaces) to support multi-interface and multi-neighbor scenarios
- E2E test execution time remains reasonable (under 10 minutes per test suite) even with complex topologies
- Network simulation tools support the required number of interfaces and neighbors for comprehensive testing
- E2E tests can reliably verify data plane connectivity and routing behavior in automated fashion
- System uses Kubernetes for configuration management with reconciliation loops handling state changes
- Kubernetes reconciliation naturally provides queueing and sequential processing of configuration updates
- Declarative configuration model means users specify desired state and system reconciles to achieve it
