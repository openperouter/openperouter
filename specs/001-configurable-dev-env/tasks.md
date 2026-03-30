# Tasks: Configurable Development Environment

**Input**: Design documents from `/specs/001-configurable-dev-env/`
**Prerequisites**: plan.md (required), spec.md (required), research.md, data-model.md, contracts/

**Tests**: Tests are included as part of implementation tasks (table-driven unit tests alongside the code they test), following the project's existing testing conventions.

**Organization**: Tasks are grouped by user story to enable independent implementation and testing of each story.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (e.g., US1, US2, US3)
- Include exact file paths in descriptions

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Create project structure, CLI scaffold, and shared type definitions

- [x] T001 Create directory structure: `cmd/clab-config/`, `internal/clabconfig/`, `internal/clabconfig/templates/`, `internal/clabconfig/testdata/`, `internal/clabconfig/testdata/expected/`
- [x] T002 Create CLI entry point with cobra root command in `cmd/clab-config/main.go` — register `apply`, `summary`, and `query` subcommands as stubs
- [x] T003 Add `build-clab-config` target to `Makefile` that builds `bin/clab-config` from `cmd/clab-config/`

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Input parsing, type definitions, and validation logic that ALL user stories depend on

**CRITICAL**: No user story work can begin until this phase is complete

- [x] T004 [P] Define EnvironmentConfig, IPRanges, NodeConfig, VRFConfig, BGPConfig, and PeerConfig Go types with YAML struct tags in `internal/clabconfig/config.go`. Implement `LoadConfig(path string) (*EnvironmentConfig, error)` for YAML parsing. Include unit tests for valid and invalid configs.
- [x] T005 [P] Define ClabTopology, ClabNode, and ClabLink Go types with YAML struct tags in `internal/clabconfig/clab.go`. Implement `LoadClab(path string) (*ClabTopology, error)` for YAML parsing. Parse `endpoints` strings into node:interface pairs. Include unit tests.
- [x] T006 [P] Create test fixtures in `internal/clabconfig/testdata/`: `basic.clab.yml` (minimal topology with 2 leaves, 1 spine, 1 switch, hosts) and `basic-config.yaml` (matching environment config). Model after `clab/singlecluster/kind.clab.yml`.
- [x] T007 Define TopologyState, NodeState, InterfaceState, VRFState, LinkState, BroadcastNetwork, BroadcastMember, BGPState, and BGPPeerState types in `internal/clabconfig/state.go`. Include JSON struct tags for serialization. Implement `SaveState(path string)` and `LoadState(path string)` methods.
- [x] T008 Implement input validation in `internal/clabconfig/validate.go`: pattern compilation (Go regexp), overlapping pattern detection (FR-022), interface existence checks against clab topology (FR-021), VNI uniqueness, role constraints (no VRFs on transit nodes), IP range capacity checks (FR-021), unmatched node warnings (FR-020). Include unit tests for each validation rule.

**Checkpoint**: Foundation ready — all input types parsed, validated, and state types defined. User story implementation can begin.

---

## Phase 3: User Story 1 — Declarative Topology Configuration (Priority: P1) MVP

**Goal**: Read clab topology + environment config, allocate all resources deterministically, generate FRR configs and setup scripts, persist state, and wire into the `apply` CLI subcommand.

**Independent Test**: Run `clab-config apply --clab testdata/basic.clab.yml --config testdata/basic-config.yaml --output-dir /tmp/out` and verify generated FRR configs, setup scripts, and state file are correct.

### Implementation for User Story 1

- [x] T009 [P] [US1] Implement deterministic IP allocation for point-to-point links in `internal/clabconfig/allocator.go`: sort links alphabetically by (nodeA, nodeB), allocate sequential /31 IPv4 and /127 IPv6 subnets from configured ranges using `go-cidr`. Include unit tests verifying deterministic ordering and idempotency (FR-018).
- [x] T010 [P] [US1] Implement broadcast network allocation in `internal/clabconfig/allocator.go`: identify bridge/switch nodes in clab topology, allocate /24 IPv4 and /64 IPv6 subnets, assign IPs to all connected members sorted by node name. Include unit tests.
- [x] T011 [P] [US1] Implement VTEP IP allocation in `internal/clabconfig/allocator.go`: allocate from configured VTEP range for edge-leaf nodes only, sorted by node name. Include unit tests.
- [x] T012 [P] [US1] Implement router ID allocation in `internal/clabconfig/allocator.go`: allocate from configured router ID range for all BGP-enabled nodes, sorted by node name. Include unit tests.
- [x] T013 [P] [US1] Implement deterministic MAC address generation in `internal/clabconfig/allocator.go`: hash node name + VNI to produce locally administered MAC addresses (`02:xx:xx:xx:xx:xx`). Include unit tests verifying uniqueness and determinism (FR-018).
- [x] T014 [US1] Implement BGP peer resolution in `internal/clabconfig/allocator.go`: for each node's BGP peer patterns, find matching nodes, resolve concrete peer addresses from allocated link IPs, and build BGPPeerState entries. Include unit tests.
- [x] T015 [US1] Implement orchestrator function `Allocate(clab *ClabTopology, config *EnvironmentConfig) (*TopologyState, []string, error)` in `internal/clabconfig/allocator.go` that: matches patterns to nodes, validates inputs, calls all allocation functions in order, resolves BGP peers, and returns the complete TopologyState plus a list of warnings. Include integration test using testdata fixtures.
- [x] T016 [P] [US1] Create FRR template for edge-leaf nodes in `internal/clabconfig/templates/edge-leaf.frr.tmpl`: BGP configuration with VRF sections, EVPN address family, VTEP network advertisement, BFD support. Model after existing `e2etests/pkg/infra/testdata/leaf.tmpl`. Embed using `embed.FS` in `internal/clabconfig/generator.go`.
- [x] T017 [P] [US1] Create FRR template for transit nodes in `internal/clabconfig/templates/transit.frr.tmpl`: BGP configuration with peer relationships, EVPN relay, no VRF/VXLAN. Embed using `embed.FS` in `internal/clabconfig/generator.go`.
- [x] T018 [P] [US1] Create setup script template in `internal/clabconfig/templates/setup.sh.tmpl`: sysctl settings, VTEP IP on loopback, VRF creation, bridge/VXLAN interface setup per VRF. Model after existing `clab/leafA/setup.sh`. Embed using `embed.FS` in `internal/clabconfig/generator.go`.
- [x] T019 [US1] Implement `Generate(state *TopologyState) (map[string]GeneratedFiles, error)` in `internal/clabconfig/generator.go` that executes templates against each node's state to produce FRR configs and setup scripts. Include golden-file tests comparing output against expected files in `internal/clabconfig/testdata/expected/`.
- [x] T020 [US1] Implement the `apply` subcommand in `cmd/clab-config/apply.go`: parse `--clab`, `--config`, `--output-dir` flags, call LoadClab, LoadConfig, validate, Allocate, Generate, write files to output dir per node, save state file, print summary to stdout. Wire into cobra in `main.go`.
- [x] T021 [US1] Create sample `environment-config.yaml` for the singlecluster topology in `clab/singlecluster/environment-config.yaml` matching the existing topology's behavior (leafA/leafB as edge-leaf, spine and leafkind as transit).

**Checkpoint**: `clab-config apply` is fully functional. Given a clab topology and environment config, it generates all FRR configs, setup scripts, and a state file. User Story 1 is independently testable.

---

## Phase 4: User Story 2 — Configuration Summary Output (Priority: P2)

**Goal**: Produce a human-readable summary of the applied configuration, displayable after `apply` and from a saved state file via `summary` subcommand.

**Independent Test**: Run `clab-config summary --state topology-state.json` and verify the output contains topology overview, per-node details, and resource allocation summary.

### Implementation for User Story 2

- [x] T022 [US2] Implement `Summary() string` method on TopologyState in `internal/clabconfig/state.go`: generate formatted text output showing topology overview (node count, link count, patterns matched), per-node details (role, router ID, VTEP IP, interfaces with peer info and IPs, VRFs with VNIs, BGP peers with ASN and address), and resource allocation summary. Include unit test comparing output against expected summary string.
- [x] T023 [US2] Implement `summary` subcommand in `cmd/clab-config/summary.go`: parse `--state` flag, call LoadState, print Summary() to stdout. Wire into cobra in `main.go`.

**Checkpoint**: Summary output is available both after `apply` and via `summary --state`. User Story 2 is independently testable.

---

## Phase 5: User Story 3 — Configuration Introspection (Priority: P2)

**Goal**: Provide programmatic query methods on TopologyState and a CLI `query` subcommand for retrieving specific topology information.

**Independent Test**: Load a state file and verify that GetNodeVTEP, GetLinkIP, FindIPOwner, and GetNodesByPattern all return correct values.

### Implementation for User Story 3

- [x] T024 [P] [US3] Implement query methods on TopologyState in `internal/clabconfig/state.go`: `GetNodeVTEP(nodeName string) (string, error)`, `GetLinkIP(nodeName, peerName string, family IPFamily) (string, error)`, `FindIPOwner(ip string) (nodeName, iface string, err error)`, `GetNodesByPattern(pattern string) ([]string, error)`. Define `IPFamily` type (IPv4/IPv6 enum). Include table-driven unit tests for each method covering found, not-found, and edge cases.
- [x] T025 [US3] Implement `query` subcommand in `cmd/clab-config/query.go`: parse `--state` flag, dispatch to query types (`node-vtep`, `link-ip`, `ip-owner`, `nodes`), print results to stdout. Wire into cobra in `main.go`.

**Checkpoint**: Both Go API and CLI query interface are functional. User Story 3 is independently testable.

---

## Phase 6: User Story 4 — Machine-Readable Output (Priority: P3)

**Goal**: Enable structured (JSON) output for the configuration summary for automation and CI/CD integration.

**Independent Test**: Run `clab-config summary --state topology-state.json -o json` and verify the output is valid JSON containing all topology information.

### Implementation for User Story 4

- [x] T026 [US4] Add `-o` flag to `summary` subcommand in `cmd/clab-config/summary.go`: when `-o json`, serialize TopologyState to JSON (using `encoding/json` with existing struct tags) and print to stdout. Include test verifying JSON output parses correctly and contains expected fields.

**Checkpoint**: Machine-readable output is available. User Story 4 is independently testable.

---

## Phase 7: User Story 5 — Multiple Topology Variations (Priority: P3)

**Goal**: Verify and document that multiple logical configurations work correctly with the same physical topology.

**Independent Test**: Apply two different environment configs to the same clab topology and verify distinct outputs.

### Implementation for User Story 5

- [x] T027 [P] [US5] Create a second test fixture `internal/clabconfig/testdata/alternate-config.yaml` with different ASNs, different VRF names/VNIs, and different IP ranges, targeting the same `basic.clab.yml` topology.
- [x] T028 [US5] Add integration test in `internal/clabconfig/allocator_test.go` that loads `basic.clab.yml` with both `basic-config.yaml` and `alternate-config.yaml`, verifies that each produces distinct TopologyState with no shared allocations, and that `--output-dir` isolation works correctly.

**Checkpoint**: Multiple topology variations are verified. User Story 5 is independently testable.

---

## Phase 8: Examples

**Purpose**: Provide complete, working examples for both singlecluster and multicluster topologies

- [x] T029 [P] Create `clab/singlecluster/environment-config.yaml`: complete environment configuration for the singlecluster topology matching existing behavior — leafA/leafB as edge-leaf with red/blue VRFs, spine as transit, leafkind as transit. Include IP ranges matching the current `ip_map.txt` allocations.
- [x] T030 [P] Create `clab/multicluster/environment-config.yaml`: complete environment configuration for the multicluster topology — leafA/leafB as edge-leaf, spine as transit, leafkind-a/leafkind-b as transit. Adapt IP ranges for the multicluster layout.
- [x] T031 Generate and validate singlecluster outputs: run `clab-config apply` against `clab/singlecluster/kind.clab.yml` + `clab/singlecluster/environment-config.yaml`, verify generated FRR configs and setup scripts produce equivalent behavior to the existing handwritten configs in `clab/leafA/`, `clab/leafB/`, `clab/singlecluster/spine/`, `clab/singlecluster/leafkind/`.
- [x] T032 Generate and validate multicluster outputs: run `clab-config apply` against `clab/multicluster/kind.clab.yml` + `clab/multicluster/environment-config.yaml`, verify generated configs match existing behavior in `clab/multicluster/spine/`, `clab/multicluster/leafkind-a/`, `clab/multicluster/leafkind-b/`.

---

## Phase 9: Documentation

**Purpose**: Update project documentation to describe the new configuration tool and workflow

- [x] T033 [P] Create `website/content/docs/contributing/clab-config.md`: document the `clab-config` tool — purpose, installation (`make build-clab-config`), the `apply`/`summary`/`query` subcommands with flags and examples, the environment-config.yaml schema with annotated example, and the generated file structure.
- [x] T034 [P] Update `website/content/docs/contributing/devenv.md`: add a section explaining the new declarative configuration workflow — how to use `clab-config apply` to regenerate topology configs, how environment-config.yaml relates to kind.clab.yml, and how to create new topology variations by writing a new environment-config.yaml.
- [x] T035 [P] Create `clab/README.md` (or update if exists): document the clab directory structure, explain the relationship between .clab.yml and environment-config.yaml files, list available topology variations (singlecluster, multicluster), and provide quick-start commands for generating and deploying each topology.
- [x] T036 [P] Add inline usage help to the CLI: add `Long` descriptions and `Example` fields to each cobra command in `cmd/clab-config/apply.go`, `cmd/clab-config/summary.go`, and `cmd/clab-config/query.go` so that `clab-config apply --help` provides a complete usage example with sample paths.

---

## Phase 10: Polish & Cross-Cutting Concerns (DEFERRED)

**Purpose**: E2e test migration, build integration, and final validation

**Note**: T037-T041 are deferred. The e2e tests rely on hardcoded IP addresses matching the handwritten configs. Migrating requires: (1) switching the actual clab deployment to use clab-config-generated configs, (2) updating all host setup scripts with new IPs, (3) adding kind-nodes peer-group support to the transit template. This should be done as a separate follow-up PR.

- [ ] T037 [P] Create `e2etests/pkg/infra/topology.go`: implement a `LoadTopology()` function that calls `clabconfig.Load()` with the singlecluster clab and environment config paths, providing a shared TopologyState for all e2e tests.
- [ ] T038 Migrate hardcoded values in `e2etests/pkg/infra/leaf.go`: replace `VTEPIP`, `SpineAddress`, `HostARedIPv4`, `HostABlueIPv4`, etc. constants with queries against the loaded TopologyState from `topology.go`.
- [ ] T039 Migrate hardcoded links in `e2etests/pkg/infra/routers.go`: replace the `init()` function's manual `links.Add()` calls with link data derived from the loaded TopologyState.
- [ ] T040 Run `make test` and `make lint` to verify all existing tests pass with the migrated e2e infrastructure code.
- [ ] T041 Run quickstart.md validation: execute the quickstart workflow against `clab/singlecluster/` to verify end-to-end correctness.

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies — can start immediately
- **Foundational (Phase 2)**: Depends on Phase 1 completion — BLOCKS all user stories
- **User Stories (Phases 3-7)**: All depend on Phase 2 completion
  - US1 (Phase 3) must complete before US2 (Phase 4) because Summary depends on TopologyState from Allocate
  - US3 (Phase 5) can start after Phase 2 (query methods only need state types) but the CLI query needs LoadState from Phase 3
  - US4 (Phase 6) depends on US2 (extends summary subcommand)
  - US5 (Phase 7) depends on US1 (needs apply working)
- **Examples (Phase 8)**: Depends on US1 completion (needs `apply` working to generate and validate configs)
- **Documentation (Phase 9)**: Can start after US1 (tool exists); best after US2-US4 so docs cover all subcommands
- **Polish (Phase 10)**: Depends on US1, US3, and Examples completion (needs apply, query API, and validated example configs)

### User Story Dependencies

- **US1 (P1)**: Can start after Phase 2 — no dependencies on other stories
- **US2 (P2)**: Depends on US1 (needs TopologyState populated by Allocate)
- **US3 (P2)**: Can start after Phase 2 for Go API; CLI query needs state file from US1
- **US4 (P3)**: Depends on US2 (extends summary subcommand with `-o json`)
- **US5 (P3)**: Depends on US1 (needs apply to work for testing multiple configs)
- **Examples**: Depends on US1 (T029, T030 can start once apply works; T031, T032 need apply to generate)
- **Documentation**: Can start in parallel once US1 is done; ideally after US4 for complete coverage

### Within Each User Story

- Models/types before logic
- Allocation logic before generation
- Templates before generator
- Library before CLI subcommand
- Core implementation before integration

### Parallel Opportunities

- T004, T005, T006 can all run in parallel (different files, independent types)
- T009, T010, T011, T012, T013 can all run in parallel (independent allocation functions in same file — coordinate on file access)
- T016, T017, T018 can all run in parallel (independent template files)
- T024 (query methods) can run in parallel with US2 work (T022, T023)
- T029, T030 can run in parallel (independent example config files)
- T033, T034, T035, T036 can all run in parallel (independent documentation files)
- T037 (topology.go) can run in parallel with T027 (alternate test fixture)

---

## Parallel Example: User Story 1

```bash
# Launch allocation functions in parallel (T009-T013):
Task: "Implement P2P IP allocation in internal/clabconfig/allocator.go"
Task: "Implement broadcast network allocation in internal/clabconfig/allocator.go"
Task: "Implement VTEP IP allocation in internal/clabconfig/allocator.go"
Task: "Implement router ID allocation in internal/clabconfig/allocator.go"
Task: "Implement MAC address generation in internal/clabconfig/allocator.go"

# Launch templates in parallel (T016-T018):
Task: "Create edge-leaf FRR template in internal/clabconfig/templates/edge-leaf.frr.tmpl"
Task: "Create transit FRR template in internal/clabconfig/templates/transit.frr.tmpl"
Task: "Create setup script template in internal/clabconfig/templates/setup.sh.tmpl"
```

---

## Implementation Strategy

### MVP First (User Story 1 Only)

1. Complete Phase 1: Setup
2. Complete Phase 2: Foundational (CRITICAL — blocks all stories)
3. Complete Phase 3: User Story 1
4. **STOP and VALIDATE**: Run `clab-config apply` against `clab/singlecluster/` and verify generated configs match expected behavior
5. Deploy/demo if ready

### Incremental Delivery

1. Complete Setup + Foundational → Foundation ready
2. Add User Story 1 → Test with real topology → **MVP!**
3. Add User Story 2 → Summary output available
4. Add User Story 3 → Query API for tests
5. Add User Story 4 → JSON output for automation
6. Add User Story 5 → Multiple config variations verified
7. Examples → Singlecluster and multicluster environment configs validated
8. Documentation → Tool docs, devenv updates, clab README, CLI help
9. Polish → E2e test migration, full integration

---

## Notes

- [P] tasks = different files, no dependencies
- [Story] label maps task to specific user story for traceability
- Each user story should be independently completable and testable
- Commit after each task or logical group
- Stop at any checkpoint to validate story independently
- The existing `internal/ipam/` package is NOT reused directly — the new allocator operates at a different level (topology-wide allocation vs per-node veth allocation). However, it uses the same `go-cidr` library.
- FRR templates should be modeled after existing templates in `e2etests/pkg/infra/testdata/` but generalized for any topology
