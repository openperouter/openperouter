# Implementation Plan: Configurable RD Assigned Numbers

**Branch**: `distinguisher` | **Date**: 2026-10-01 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/001-custom-route-distinguisher/spec.md`

## Summary

Add an optional `rdAssignedNumber` to L2VNI and L3VNI. On each selected node, combine the configured number with that node's router ID and render it as the VNI's EVPN RD. Leave FRR's automatic VNI RD in place when the field is omitted. L3VPN's existing required `rdAssignedNumber` remains unchanged. Preserve all route-target behavior and the existing VNI/L3VPN numeric restrictions; add same-node checks for duplicate configured RD numbers. See [research.md](research.md) for decisions and the limit of predicting FRR automatic RDs.

## Technical Context

**Language/Version**: Go 1.26.4 (`go.mod`)

**Primary Dependencies**: Kubernetes API machinery and controller-runtime, FRR 10.6.0, controller-gen, project FRR templates

**Storage**: Kubernetes L2VNI/L3VNI/L3VPN custom resources; no new persistent store

**Testing**: Go unit tests (`make test`), lint (`make lint`), generated-manifest checks (`make generate-all-ci`), and leaf-observed L2VNI and L3VNI RD end-to-end tests in a kind development cluster

**Target Platform**: Linux nodes in Kubernetes; FRR-based EVPN and existing SRv6 L3VPN deployments

**Project Type**: Kubernetes network operator and node router

**Performance Goals**: No additional reconciliation or BGP sessions; same-node uniqueness validation scales linearly with the number of selected overlay resources

**Constraints**: Preserve omitted-field behavior, route targets, L3VPN API semantics, legacy numeric reservations, and node selection; configured RD suffix range is 1–65535

**Scale/Scope**: One optional field on each of two VNI resource kinds, applied to every selected node; no new controllers or external service

## Constitution Check

*GATE: Evaluate before research and re-check after Phase 1 design.*

The repository's `.specify/memory/constitution.md` is an unratified template and contains no enforceable project principles. The active project instructions are `AGENTS.md`, `CODING_STYLE.md`, and the contributing guide.

| Gate | Before research | After design | Evidence |
|------|-----------------|--------------|----------|
| Preserve existing behavior when the optional field is absent | Pass | Pass | No RD command is emitted for omitted VNI fields; contract documents fallback. |
| Validate public configuration and maintain generated schemas | Pass | Pass | Presence-aware field, numeric bounds, admission and static schema checks, generation target. |
| Keep network identity and policy separate | Pass | Pass | RD assigned number changes only RD; route-target fields and defaults remain untouched. |
| Cover routing behavior and edge cases | Pass | Pass | Unit, schema, and leaf-observed route RD checks in [quickstart.md](quickstart.md). |
| Avoid unnecessary new abstractions | Pass | Pass | Extend existing conversion, validation, and FRR rendering paths. |

No gate violation or unresolved clarification remains. FRR automatic RD collisions are outside pre-admission checks by the user's decision in the spec.

## Project Structure

### Documentation (this feature)

```text
specs/001-custom-route-distinguisher/
├── spec.md
├── plan.md
├── research.md
├── data-model.md
├── quickstart.md
├── checklists/requirements.md
└── contracts/overlay-rd-api.md
```

`tasks.md` belongs to the later `$speckit-tasks` phase.

### Source Code (repository root)

```text
api/v1alpha1/
├── l2vni_types.go
├── l3vni_types.go
└── l3vpn_types.go              # existing reference behavior
internal/
├── conversion/
│   ├── frr_conversion.go
│   ├── validate_vni.go
│   └── validate_l3vpn.go
├── controller/routerconfiguration/reconcile.go
├── frr/
│   ├── config.go
│   └── templates/{underlay_evpn,vni,vpn}.tmpl
└── webhooks/{l2vni,l3vni,l3vpn}_webhook.go
config/crd/bases/
charts/openperouter/charts/crds/templates/
operator/bundle/
website/content/docs/configuration/{evpn,srv6}.md
website/content/docs/api-reference.md
e2etests/tests/
e2etests/pkg/frr/
```

**Structure Decision**: Extend the existing API, conversion, validation, and FRR template layers. The public contract is the Kubernetes custom-resource schema; generated manifests and docs follow the typed API.

## Implementation Design

1. Add optional, presence-aware VNI fields with the L3VPN numeric bounds. Regenerate schemas and deepcopy output so Kubernetes and static configuration accept the same contract.
2. Propagate configured values through L2VNI and L3VNI conversion and FRR configuration models. Pass the current node router ID into L2VNI conversion. Keep RD-only L2VNIs in the generated config even when no explicit route targets exist.
3. Render `rd <router-ID>:<assigned-number>` only for configured VNIs in their respective EVPN stanzas. Preserve L3VPN's `rd vpn export` rendering and all route-target rendering.
4. Keep current VNI and L3VPN numeric filters. Add a separate per-node check for duplicate configured RD numbers and use it in both admission and reconciliation, with resource-specific errors.
5. Add focused e2e tests for L2VNI Type-2 and L3VNI Type-5 routes under `<originating-router-ID>:<assigned-number>` on receiving leaves. Verify L3VPN behavior through conversion and FRR golden tests.
6. Reuse the existing leaf executor and FRR JSON parser. Add a narrow EVPN helper that retains the RD-to-route association, since the existing route helpers flatten all RD entries. Use `Eventually` for route convergence.
7. Cover bounds, omission, collisions, node selectors, FRR output, and unchanged RTs in focused unit and schema tests. Update user docs and generated artifacts.

## Validation Strategy

- Confirm generated CRD schemas and API docs expose optional VNI `rdAssignedNumber` with bounds and retain required L3VPN behavior.
- Run focused conversion, FRR rendering, and webhook/schema tests, then `make test` and `make lint`.
- Run `make generate-all-ci` and inspect generated output for drift.
- Run the focused L2VNI and L3VNI e2e cases described in [quickstart.md](quickstart.md). Query the receiving fabric leaves' BGP tables and require Type-2 and Type-5 routes under their originating nodes' configured RDs.
