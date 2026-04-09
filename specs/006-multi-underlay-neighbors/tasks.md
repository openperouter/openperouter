# Tasks: Support Multiple Underlay Interfaces and Neighbors

**Input**: Design documents from `/specs/006-multi-underlay-neighbors/`
**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/

**Organization**: Tasks are grouped by user story to enable independent implementation and testing of each story.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (e.g., US1, US2, US3)
- Include exact file paths in descriptions

## Path Conventions

Repository root:
- `api/v1alpha1/` - CRD definitions
- `internal/conversion/` - Validation and conversion logic
- `internal/webhooks/` - Admission webhook handlers
- `internal/controller/` - Kubernetes reconciliation logic
- `internal/hostnetwork/` - Network namespace setup
- `e2etests/tests/` - End-to-end tests
- `clab/` - Containerlab topology definitions

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Project preparation and dependency verification

- [X] T001 Review current validation logic in internal/conversion/validate_underlay.go to identify single-entity constraints
- [X] T002 Review current host conversion logic in internal/conversion/host_conversion.go to identify Nics[0] assumptions
- [X] T003 [P] Review FRR conversion logic in internal/conversion/frr_conversion.go to verify neighbor iteration
- [X] T004 [P] Review webhook validation in internal/webhooks/underlay_webhook.go for extension points

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Core infrastructure changes that MUST be complete before ANY user story can be implemented

**⚠️ CRITICAL**: No user story work can begin until this phase is complete

- [X] T005 Remove single-underlay validation check at internal/conversion/validate_underlay.go:32-33
- [X] T006 Update ValidateUnderlays function to accept multiple underlays per node in internal/conversion/validate_underlay.go
- [X] T007 [P] Add uniqueness validation for neighbor addresses in internal/webhooks/underlay_webhook.go
- [X] T008 [P] Add uniqueness validation for nic names in internal/webhooks/underlay_webhook.go
- [X] T009 Add validation to reject empty configuration (zero interfaces and neighbors) in internal/webhooks/underlay_webhook.go
- [X] T010 Update error messages to identify specific invalid interface/neighbor in internal/webhooks/underlay_webhook.go

**Checkpoint**: Foundation ready - user story implementation can now begin in parallel

---

## Phase 3: User Story 1 - Configure Multiple Underlay Interfaces (Priority: P1) 🎯 MVP

**Goal**: Enable users to configure multiple underlay interfaces for network paths, redundancy, or traffic segregation

**Independent Test**: Submit API request with 3 interface configurations, verify all accepted and validated

### Implementation for User Story 1

- [X] T011 [US1] Remove Nics[0] array index assumption in internal/conversion/host_conversion.go:40
- [X] T012 [US1] Update hostnetwork.UnderlayParams to store array of interfaces instead of single string in internal/hostnetwork/setup.go
- [X] T013 [US1] Implement iteration over underlay.Spec.Nics array in internal/conversion/host_conversion.go
- [X] T014 [US1] Update network namespace setup to move all interfaces to router namespace in internal/hostnetwork/setup.go
- [X] T015 [US1] Verify each interface configuration independently during validation in internal/conversion/validate_underlay.go
- [X] T016 [US1] Add error handling for interface-specific validation failures in internal/webhooks/underlay_webhook.go
- [X] T017 [US1] Update backward compatibility handling to accept single-interface configs as valid subset in internal/conversion/validate_underlay.go

**Checkpoint**: At this point, multiple interfaces can be configured via API and moved to router namespace

---

## Phase 4: User Story 2 - Configure Multiple Neighbors (Priority: P2)

**Goal**: Enable users to define multiple BGP neighbor relationships for network topology with multiple peers

**Independent Test**: Submit API request with 5 neighbor configurations, verify all accepted and validated

### Implementation for User Story 2

- [X] T018 [US2] Verify FRR conversion already iterates underlay.Spec.Neighbors array correctly in internal/conversion/frr_conversion.go:44-57
- [X] T019 [US2] Verify BFD profile generation handles multiple neighbors in internal/conversion/frr_conversion.go
- [X] T020 [US2] Verify router-id assignment works with multiple neighbors in internal/conversion/frr_conversion.go
- [X] T021 [US2] Test EVPN address-family configuration with multiple neighbors in internal/conversion/frr_conversion.go
- [X] T022 [US2] Verify each neighbor configuration independently during validation in internal/conversion/validate_underlay.go
- [X] T023 [US2] Add error handling for neighbor-specific validation failures in internal/webhooks/underlay_webhook.go
- [X] T024 [US2] Update backward compatibility handling to accept single-neighbor configs as valid subset in internal/conversion/validate_underlay.go
- [X] T025 [US2] Verify ASN conflict detection works for all neighbors in internal/conversion/validate_underlay.go

**Checkpoint**: At this point, multiple neighbors can be configured via API and FRR config generates correctly

---

## Phase 5: User Story 5 - Comprehensive End-to-End Testing (Priority: P2)

**Goal**: Validate multi-interface and multi-neighbor configurations in realistic network topologies with containerlab

**Independent Test**: Run E2E tests in CI/dev environments, verify all connectivity and configuration aspects

### E2E Test Infrastructure

- [X] T026 [US5] Update existing containerlab topology to add 2nd leaf node in clab/*.clab.yml
- [X] T027 [US5] Add connections from both leaf nodes to TOR switches in clab/*.clab.yml
- [X] T028 [US5] Ensure all kind nodes connect to both leaf nodes in containerlab topology in clab/*.clab.yml
- [ ] T029 [US5] Verify containerlab topology deploys successfully with 2 leaf nodes locally

### New Single-Session Test

- [X] T030 [P] [US5] Create new single-session E2E test file in e2etests/tests/singlesession_test.go
- [X] T031 [US5] Implement test case: deploy single interface and single neighbor configuration in e2etests/tests/singlesession_test.go
- [X] T032 [US5] Implement test case: verify BGP session establishes with TOR in e2etests/tests/singlesession_test.go
- [X] T033 [US5] Implement test case: verify L3 connectivity from leaf1 to pod (ping) in e2etests/tests/singlesession_test.go
- [X] T034 [US5] Implement test case: verify L3 connectivity from leaf2 to pod (ping) in e2etests/tests/singlesession_test.go
- [X] T035 [US5] Implement test case: verify routing tables on both leafs in e2etests/tests/singlesession_test.go

### Transform Existing Tests to Multi-Session (Multi-NIC AND Multi-Neighbor)

- [X] T036 [P] [US5] Update sessions test to use multi-nic and multi-neighbor configuration (3 interfaces, 4 neighbors) in e2etests/tests/sessions_test.go
- [X] T037 [P] [US5] Update hostconfiguration test to use multi-nic and multi-neighbor configuration in e2etests/tests/hostconfiguration_test.go
- [X] T038 [P] [US5] Update webhooks test to validate multi-nic and multi-neighbor configurations in e2etests/tests/webhooks_test.go
- [X] T039 [US5] Add L3 connectivity validation from both leaf nodes to all transformed tests in e2etests/tests/*.go
- [X] T040 [US5] Verify data plane connectivity across all configured interfaces in transformed tests in e2etests/tests/*.go
- [X] T041 [US5] Add test case for verifying all BGP sessions establish across multiple TORs in e2etests/tests/sessions_test.go

### E2E Test Execution and Validation

- [ ] T042 [US5] Run new single-session test locally and verify it passes
- [ ] T043 [US5] Run all transformed multi-session tests locally and verify they pass
- [ ] T044 [US5] Verify E2E tests run in CI environment successfully
- [ ] T045 [US5] Document E2E test execution in quickstart.md for developers

**Checkpoint**: E2E tests validate both single-session baseline and multi-session comprehensive scenarios

---

## Phase 6: User Story 3 - Combined Multi-Interface Multi-Neighbor Configuration (Priority: P3)

**Goal**: Enable complete network deployments with both multiple interfaces and multiple neighbors in single API request

**Independent Test**: Submit API request with 3 interfaces and 4 neighbors, verify complete topology configures correctly (validated by transformed E2E tests in US5)

### Implementation for User Story 3

- [X] T046 [US3] Test combined multi-interface multi-neighbor validation in internal/webhooks/underlay_webhook.go
- [X] T047 [US3] Verify atomic update behavior (all or nothing) for combined multi-entity requests in internal/webhooks/underlay_webhook.go
- [X] T048 [US3] Test FRR configuration generation with combined multi-interface multi-neighbor scenario in internal/conversion/frr_conversion.go
- [X] T049 [US3] Test host network configuration with combined multi-interface multi-neighbor scenario in internal/conversion/host_conversion.go
- [X] T050 [US3] Verify partial update rejection when some entities valid, others invalid in internal/webhooks/underlay_webhook.go

**Checkpoint**: Complete multi-interface multi-neighbor configurations work end-to-end (E2E coverage provided by transformed tests in Phase 5)

---

## Phase 7: User Story 4 - Apply Configuration Changes Without Restart (Priority: P4)

**Goal**: Minimize service disruption by hot-applying runtime additions while requiring restart only for structural changes

**Independent Test**: Add new interface/neighbor dynamically, verify no restart occurred (check container uptime)

### Restart Decision Logic

- [ ] T051 [US4] Implement restart decision function in internal/controller/routerconfiguration/reconcile.go
- [ ] T052 [US4] Add logic to detect initial setup (no previous config) requiring restart in internal/controller/routerconfiguration/reconcile.go
- [ ] T053 [US4] Add logic to detect structural changes (ASN change, router ID CIDR change) requiring restart in internal/controller/routerconfiguration/reconcile.go
- [ ] T054 [US4] Add logic to detect teardown (all entities removed) requiring restart in internal/controller/routerconfiguration/reconcile.go
- [ ] T055 [US4] Add logic to detect EVPN vtepCIDR/vtepInterface switch requiring restart in internal/controller/routerconfiguration/reconcile.go

### Hot-Apply Implementation

- [ ] T056 [US4] Implement hot-apply for new interface additions using netlink in internal/hostnetwork/setup.go
- [ ] T057 [US4] Implement hot-apply for new neighbor additions using vtysh in internal/frr/reload.go
- [ ] T058 [US4] Implement FRR config diff and incremental apply logic in internal/frr/reload.go
- [ ] T059 [US4] Add FRR soft BGP reset for parameter changes in internal/frr/reload.go
- [ ] T060 [US4] Add logging to indicate restart vs hot-apply decision in internal/controller/routerconfiguration/reconcile.go
- [ ] T061 [US4] Update configuration response to inform users about restart requirement in internal/webhooks/underlay_webhook.go

### Hot-Apply Testing

- [ ] T062 [US4] Add E2E test: add new interface, verify no restart, verify connectivity in e2etests/tests/hotapply_test.go
- [ ] T063 [US4] Add E2E test: add new neighbor, verify no restart, verify BGP session in e2etests/tests/hotapply_test.go
- [ ] T064 [US4] Add E2E test: modify neighbor parameter, verify behavior matches restart criteria in e2etests/tests/hotapply_test.go
- [ ] T065 [US4] Add E2E test: structural change (ASN change), verify restart occurs in e2etests/tests/hotapply_test.go

**Checkpoint**: Configuration changes intelligently apply restart only when necessary

---

## Phase 8: Polish & Cross-Cutting Concerns

**Purpose**: Improvements that affect multiple user stories and final quality checks

- [X] T066 [P] Update CRD schema documentation in api/v1alpha1/underlay_types.go comments
- [X] T067 [P] Update API contracts examples with multi-entity scenarios in specs/006-multi-underlay-neighbors/contracts/examples.yaml
- [X] T068 [P] Run `make manifests` to regenerate CRD YAML
- [X] T069 [P] Run `make generate` to update generated code
- [X] T070 Run unit tests: `go test ./internal/conversion/... ./internal/webhooks/...`
- [ ] T071 Run E2E test suite: `make e2e-test`
- [ ] T072 Verify backward compatibility: deploy single-interface/neighbor config and test
- [ ] T073 Performance test: submit config with 10 interfaces and 20 neighbors, verify <2s validation
- [X] T074 [P] Update user-facing documentation at website/ with multi-entity examples
- [X] T075 [P] Update quickstart.md with final test commands and topology details
- [ ] T076 Code review and cleanup: remove dead code, add comments where needed
- [ ] T077 Final validation: run complete quickstart.md workflow

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies - can start immediately
- **Foundational (Phase 2)**: Depends on Setup completion - BLOCKS all user stories
- **User Stories (Phase 3-7)**: All depend on Foundational phase completion
  - User stories can then proceed in parallel (if staffed)
  - Or sequentially in priority order (P1 → P2/P2 → P3 → P4)
- **Polish (Phase 8)**: Depends on all desired user stories being complete

### User Story Dependencies

- **User Story 1 (P1)**: Can start after Foundational (Phase 2) - No dependencies on other stories
- **User Story 2 (P2)**: Can start after Foundational (Phase 2) - Independent of US1 (different files)
- **User Story 5 (P2)**: Can start in parallel with US1/US2 - Tests validate their implementations
- **User Story 3 (P3)**: Depends on US1 and US2 being complete - Tests combined functionality
- **User Story 4 (P4)**: Depends on US1, US2, US3 being complete - Optimizes their behavior

### Within Each User Story

- Tasks within same file must be sequential
- Tasks marked [P] can run in parallel (different files)
- Tests should be run after implementation tasks complete
- Story complete before moving to next priority

### Parallel Opportunities

- **Phase 1**: T003, T004 can run in parallel
- **Phase 2**: T007, T008 can run in parallel
- **Phase 3**: All US1 tasks are sequential (same files)
- **Phase 4**: All US2 tasks are sequential (same files)
- **Phase 5**: T030, T036, T037, T038 can run in parallel (different test files)
- **Phase 8**: T066, T067, T068, T069, T074, T075 can run in parallel

**Cross-Story Parallelization**:
- US1 (Phase 3) and US2 (Phase 4) can run in parallel after Phase 2
- US5 (Phase 5) can start E2E infrastructure (T026-T029) in parallel with US1/US2
- With 3 developers: Dev1=US1, Dev2=US2, Dev3=US5 after Foundational phase completes

---

## Parallel Example: User Story 5 (E2E Testing)

```bash
# Launch infrastructure setup in parallel:
Task: "Update containerlab topology for 2nd leaf node"

# Launch new test files in parallel after infrastructure ready:
Task: "Create singlesession_test.go"  # Developer A
Task: "Transform sessions_test.go"     # Developer B  
Task: "Transform hostconfiguration_test.go"  # Developer C
Task: "Transform webhooks_test.go"     # Developer D
```

---

## Implementation Strategy

### MVP First (User Story 1 Only)

1. Complete Phase 1: Setup (Tasks T001-T004)
2. Complete Phase 2: Foundational (Tasks T005-T010) - CRITICAL
3. Complete Phase 3: User Story 1 (Tasks T011-T017)
4. **STOP and VALIDATE**: Test multi-interface configuration independently
5. Deploy/demo if ready

**Value Delivered**: Users can configure multiple underlay interfaces for redundant paths

### Incremental Delivery

1. Complete Setup + Foundational (T001-T010) → Foundation ready
2. Add User Story 1 (T011-T017) → Test independently → **MVP READY**
3. Add User Story 2 (T018-T025) → Test independently → **Multi-neighbor support added**
4. Add User Story 5 (T026-T045) → Test independently → **E2E validation complete**
5. Add User Story 3 (T046-T050) → Test independently → **Full feature integrated** (E2E covered by transformed tests)
6. Add User Story 4 (T051-T065) → Test independently → **Production-ready with hot-apply**
7. Complete Polish (T066-T077) → **Release ready**

Each increment adds value without breaking previous functionality.

### Parallel Team Strategy

With multiple developers:

1. **Week 1**: Team completes Setup + Foundational together (T001-T010)
2. **Week 2-3**: Once Foundational is done:
   - Developer A: User Story 1 (T011-T017)
   - Developer B: User Story 2 (T018-T025)
   - Developer C: User Story 5 infrastructure (T026-T029)
3. **Week 4**: E2E test development:
   - Developer A: Single-session test (T030-T035)
   - Developer B: Transform sessions test (T036, T041)
   - Developer C: Transform hostconfig + webhooks tests (T037-T040)
4. **Week 5**: User Story 3 + 4
5. **Week 6**: Polish and final validation

---

## Notes

- [P] tasks = different files, no dependencies
- [Story] label maps task to specific user story for traceability
- Each user story should be independently completable and testable
- Commit after each task or logical group
- Stop at any checkpoint to validate story independently
- Run `make test` frequently during implementation
- Run `make e2e-test` after each user story completion
- Keep backward compatibility in mind for all validation changes
- Focus on US1 (P1) for MVP before expanding to other stories
