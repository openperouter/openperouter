---

description: "Implementation tasks for configurable RD assigned numbers"
---

# Tasks: Configurable RD Assigned Numbers

**Input**: [plan.md](plan.md), [spec.md](spec.md), [research.md](research.md), [data-model.md](data-model.md), [contracts/overlay-rd-api.md](contracts/overlay-rd-api.md), and [quickstart.md](quickstart.md)

**Tests**: The specification requires routing, validation, compatibility, and leaf-observed e2e coverage. Write focused tests before the behavior they check and confirm they fail for the intended reason.

**Organization**: Shared API and per-node collision handling precede the L2VNI, L3VNI, and L3VPN story phases. FRR's automatic RD is preserved when a VNI field is omitted.

## Phase 1: Setup

**Purpose**: Establish the existing behavior and the presence-aware public fields.

- [X] T001 Run the existing focused conversion, validation, FRR rendering, and webhook tests in `internal/conversion/frr_conversion_test.go`, `internal/conversion/validate_vni_test.go`, `internal/frr/frr_test.go`, and `internal/webhooks/l2vni_webhook_test.go` to record the pre-change baseline.
- [X] T002 Add optional pointer `spec.rdAssignedNumber` fields with 1–65535 Kubebuilder bounds and L3VPN-consistent descriptions in `api/v1alpha1/l2vni_types.go` and `api/v1alpha1/l3vni_types.go`; keep the required field in `api/v1alpha1/l3vpn_types.go` unchanged.

---

## Phase 2: Foundation — Per-Node RD Uniqueness

**Purpose**: Make configured RD numbers safe across selected L2VNIs, L3VNIs, and L3VPNs without changing existing VNI-number reservations.

- [X] T003 [P] Add table-driven tests for configured-number collisions across all three resource kinds, omitted VNI numbers, retained VNI-versus-L3VPN restrictions, and reuse on disjoint selected nodes in `internal/conversion/validate_vni_test.go`.
- [X] T004 [P] Add create/update rejection tests, including a resource-identifying error, for cross-kind configured-number collisions in `internal/webhooks/l2vni_webhook_test.go`, `internal/webhooks/l3vni_webhook_test.go`, and `internal/webhooks/l3vpn_webhook_test.go`.
- [X] T005 [P] Add reconciliation tests proving a colliding resource is reported and excluded while a valid resource still configures, without weakening existing numeric filters, in `internal/controller/routerconfiguration/reconcile_test.go`.
- [X] T006 Validate present VNI numbers in 1–65535 and implement one per-node configured-RD-number filter that uses only present VNI fields and required L3VPN numbers, reports both resource identities, and preserves the existing VNI-number checks in `internal/conversion/validate_vni.go`; use it from admission's node-selected validation path.
- [X] T007 Apply the same configured-RD-number filter after existing numeric filters in `internal/controller/routerconfiguration/reconcile.go`; preserve partial reconciliation and verify T003–T005 pass.

**Checkpoint**: Configured RD numbers are unique per selected node in admission and reconciliation; omitted VNI fields do not enter this new comparison.

---

## Phase 3: User Story 1 — Set an RD for an L2VNI (Priority: P1) 🎯 MVP

**Goal**: A selected node advertises L2VNI routes with `<its-router-ID>:<rdAssignedNumber>`; omission retains FRR's automatic RD and all existing route targets.

**Independent test**: Configure one L2VNI on two nodes, create one pod per node, and find both Type-2 MAC/IP routes under their respective originating-node RDs on a receiving fabric leaf. Check the originating VTEP and route targets in each RD entry.

### Tests

- [X] T008 [P] [US1] Add conversion tests for configured/omitted L2VNI numbers, two router IDs, RD-only L2VNIs with empty explicit RT lists, and unchanged RTs in `internal/conversion/frr_conversion_test.go`.
- [X] T009 [P] [US1] Add FRR rendering tests for the configured `vni`-stanza `rd` command, omitted-command fallback, and unchanged RT commands in `internal/frr/frr_test.go` with expected output in `internal/frr/testdata/TestL2VNIWithRDAssignedNumber.golden`.
- [X] T010 [P] [US1] Add tests for an EVPN route matcher that checks Type-2/Type-5 route identity, next hop, and RTs within the expected RD entry in `e2etests/pkg/frr/evpn_test.go`.

### Implementation

- [X] T011 [US1] Pass the node router ID into L2VNI conversion, derive the configured RD with the existing `routeDistinguisher` function, and retain an RD-only VNI without changing RT derivation in `internal/conversion/frr_conversion.go`.
- [X] T012 [US1] Carry the optional L2VNI RD through `internal/frr/config.go` and render `rd <router-ID>:<assigned-number>` only when present in `internal/frr/templates/underlay_evpn.tmpl`; verify T008–T009 pass.
- [X] T013 [US1] Implement the RD-scoped Type-2 and Type-5 EVPN route matcher using `EVPNData.Entries` in `e2etests/pkg/frr/evpn.go`; verify T010 passes.
- [X] T014 [US1] Add an admission test for present zero, negative, and above-65535 L2VNI values plus valid 1 and 65535 in `internal/webhooks/l2vni_webhook_test.go`; confirm omitted values remain valid.
- [X] T015 [US1] Run `make generate-all-ci` and verify the L2VNI schema and deepcopy output in `config/crd/bases/network.openperouter.io_l2vnis.yaml`, `charts/openperouter/charts/crds/templates/network.openperouter.io_l2vnis.yaml`, `operator/bundle/manifests/network.openperouter.io_l2vnis.yaml`, and `api/v1alpha1/zz_generated.deepcopy.go`.
- [X] T016 [US1] Add a focused `RD assigned number` e2e case with two node-originated Type-2 pod routes observed under their configured RDs through `frr.EVPNInfo` on `infra.KindLeaf` in `e2etests/tests/evpn_l2.go`.
- [X] T017 [US1] Keep the existing two-client route-reflector e2e case as a regression without adding a feature-specific RD assertion in `e2etests/tests/route_reflector.go`.

**Checkpoint**: US1 can be deployed and verified without an L3VNI RD override or any L3VPN change.

---

## Phase 4: User Story 2 — Set an RD for an L3VNI (Priority: P2)

**Goal**: An L3VNI advertises Type-5 routes with its node-specific configured RD while preserving VNI, VRF, route targets, and omitted-field behavior.

**Independent test**: Configure an L3VNI with an RD assigned number and find its Type-5 prefix under each originating router's RD on LeafA and LeafB.

### Tests

- [X] T018 [P] [US2] Add conversion tests for configured/omitted L3VNI numbers in both no-HostSession and per-family HostSession paths, two router IDs, and unchanged RTs in `internal/conversion/frr_conversion_test.go`.
- [X] T019 [P] [US2] Add FRR rendering tests for configured and omitted L3VNI RD commands alongside existing route-target commands in `internal/frr/frr_test.go` with expected output in `internal/frr/testdata/TestL3VNIWithRDAssignedNumber.golden`.

### Implementation

- [X] T020 [US2] Derive the L3VNI RD from its optional number and the selected node's router ID in every L3VNI conversion path in `internal/conversion/frr_conversion.go`.
- [X] T021 [US2] Carry the optional L3VNI RD in `internal/frr/config.go` and render it only in the VRF `address-family l2vpn evpn` stanza in `internal/frr/templates/vni.tmpl`; verify T018–T019 pass.
- [X] T022 [US2] Add admission tests for L3VNI present zero, negative, and above-65535, valid 1 and 65535, and omitted fallback in `internal/webhooks/l3vni_webhook_test.go`.
- [X] T023 [US2] Run `make generate-all-ci` and verify the L3VNI schema and deepcopy output in `config/crd/bases/network.openperouter.io_l3vnis.yaml`, `charts/openperouter/charts/crds/templates/network.openperouter.io_l3vnis.yaml`, `operator/bundle/manifests/network.openperouter.io_l3vnis.yaml`, and `api/v1alpha1/zz_generated.deepcopy.go`.
- [X] T024 [US2] Add a focused Type-5 e2e case that checks the configured L3VNI RD for each originating node on LeafA and LeafB in `e2etests/tests/route_target.go`.

**Checkpoint**: US2 is independently verifiable by conversion/FRR tests and leaf-observed Type-5 routes.

---

## Phase 5: User Story 3 — Preserve L3VPN RD Behavior (Priority: P3)

**Goal**: Confirm the existing required L3VPN assigned number still forms the VPN RD with each originating node's router ID and still supplies the existing default export RT.

**Independent test**: Convert an L3VPN for a selected router ID and verify its `rd vpn export` and unchanged export route target.

### Tests and verification

- [X] T025 [P] [US3] Add an L3VPN conversion regression covering node-specific `rd vpn export`, default export RT, and unchanged explicit export RT in `internal/conversion/frr_conversion_test.go`.
- [X] T026 [P] [US3] Add an admission regression proving the required L3VPN field and its 1–65535 range remain enforced in `internal/webhooks/l3vpn_webhook_test.go`.
- [X] T027 [US3] Keep the existing SRv6 fabric test unchanged and verify L3VPN RD compatibility with conversion and admission tests.

**Checkpoint**: L3VPN behavior is covered by conversion and admission tests without a new L3VPN API field.

---

## Phase 6: Polish and Cross-Cutting Verification

- [X] T028 [P] Document the optional L2VNI/L3VNI field, router-ID formula, automatic fallback, unchanged VNI RTs, and same-node uniqueness in `website/content/docs/configuration/evpn.md`; confirm the existing L3VPN explanation in `website/content/docs/configuration/srv6.md` remains accurate.
- [X] T029 Add static-configuration schema tests for omitted, valid, and out-of-range L2VNI/L3VNI `rdAssignedNumber` values in `internal/controller/routerconfiguration/static_configuration_reader_test.go`.
- [X] T030 Run `make generate-all-ci` and check generated API docs and manifests, including `website/content/docs/api-reference.md`, `config/crd/bases/network.openperouter.io_l2vnis.yaml`, `config/crd/bases/network.openperouter.io_l3vnis.yaml`, and their Helm/bundle copies, for the same optional bounds and no unintended L3VPN contract change.
- [X] T031 Run focused tests, then `make test` and `make lint`; resolve failures in `internal/conversion/frr_conversion_test.go`, `internal/conversion/validate_vni_test.go`, `internal/frr/frr_test.go`, `internal/webhooks/l2vni_webhook_test.go`, `internal/webhooks/l3vni_webhook_test.go`, and `internal/controller/routerconfiguration/reconcile_test.go`.
- [X] T032 Deploy the feature in Kubernetes mode and run the focused L2VNI and L3VNI RD Ginkgo cases from `specs/001-custom-route-distinguisher/quickstart.md`; inspect the leaf route assertions in `e2etests/tests/evpn_l2.go` and `e2etests/tests/route_target.go`.

---

## Dependencies and Execution Order

```text
T001 → T002 → {T003, T004, T005} → T006 → T007
T007 → US1 (T008–T017)
T007 → US2 (T018–T024)
T007 → US3 (T025–T027)
US1 + US2 + US3 → T028–T032
```

- Within US1, T008–T010 precede T011–T013; T011 precedes T012, and T013 precedes T016–T017. T014 and T015 follow the public API field; T016–T017 follow deployable schema generation.
- Within US2, T018–T019 precede T020–T021; T021 precedes T024, and T023 precedes cluster e2e execution.
- Within US3, T025–T026 are regression tests; T027 reuses the existing SRv6 topology and does not depend on US1 or US2.
- T029 follows both story-specific generation tasks. T030 follows T029; T032 follows all focused cases and cluster deployment. Use [quickstart.md](quickstart.md) for the validation commands.

### Parallel execution examples

| After prerequisite | Independent tasks | Why |
|--------------------|-------------------|-----|
| T002 | T003, T004, T005 | Separate validation, webhook, and reconciliation test files. |
| T007 | T008, T009, T010 | Separate conversion, FRR, and e2e parser test files. |
| T007 | T018 and T019 | Separate conversion and FRR test files within US2. |
| T007 | T025 and T026 | Separate L3VPN conversion and webhook test files. |
| All stories | T028 alongside verification preparation | Documentation is independent of code test runs. |

The story phases can be developed separately after T007. Tasks in different stories that touch `internal/conversion/frr_conversion.go`, `internal/frr/config.go`, `internal/frr/frr_test.go`, or generated files must be serialized or coordinated to avoid overlapping edits.

## Implementation Strategy

1. Complete T001–T007 so admission and reconciliation enforce the configured-number rule.
2. Deliver US1 as the MVP: L2VNI field, FRR output, two-node Type-2 evidence on a leaf, and reflected RD preservation.
3. Add US2's L3VNI RD and leaf-observed Type-5 proof.
4. Confirm US3's existing SRv6 L3VPN behavior on LeafSRV6.
5. Complete documentation, generation, unit/lint gates, and focused e2e validation in T028–T032.
