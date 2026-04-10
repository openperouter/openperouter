# Implementation Plan: Support Multiple Underlay Interfaces and Neighbors

**Branch**: `006-multi-underlay-neighbors` | **Date**: 2026-04-01 | **Spec**: [spec.md](./spec.md)
**Input**: Feature specification from `/specs/006-multi-underlay-neighbors/spec.md`

**Note**: This template is filled in by the `/speckit.plan` command. See `.specify/templates/plan-template.md` for the execution workflow.

## Summary

Remove the single-interface and single-neighbor constraints from the Underlay CRD API to support realistic network topologies with multiple underlay interfaces and multiple BGP neighbor relationships. The implementation will update validation logic to handle arrays of interfaces/neighbors, modify FRR configuration generation to support multiple entities, implement hot-reload capability for runtime additions, and create comprehensive E2E tests using containerlab for multi-interface/multi-neighbor scenarios.

## Technical Context

**Language/Version**: Go 1.25.7  
**Primary Dependencies**: 
- Kubernetes: controller-runtime, client-go, apimachinery (v0.35.0)
- FRR (Free Range Routing): BGP/EVPN routing daemon
- Networking: vishvananda/netlink, netns for namespace manipulation
- Containerlab: 0.74.1+ for E2E testing with network topology simulation

**Storage**: Kubernetes etcd (via CRDs), in-memory state in controller  
**Testing**: Ginkgo v2.27.2 / Gomega v1.38.2 for E2E tests, standard Go testing for unit tests  
**Target Platform**: Linux nodes in Kubernetes clusters (network namespaces, container networking)  
**Project Type**: Kubernetes Operator (CRD-based controller with webhook validation)  
**Performance Goals**: 
- API validation: <2 seconds response time
- Reconciliation: handle configuration changes within standard K8s reconciliation loop timing
- No performance degradation with multi-entity configurations vs single-entity

**Constraints**: 
- Current single-interface/single-neighbor limitation in `internal/conversion/validate_underlay.go:32-33`
- Current array index access `Nics[0]` and `Neighbors[0]` in conversion logic
- Must maintain backward compatibility with existing single-entity configurations
- Kubernetes reconciliation loop handles concurrent updates via queueing
- Router namespace restart currently triggered on any underlay change

**Scale/Scope**: 
- No hard-coded limits on interfaces/neighbors (resource-constrained only)
- Typical deployments: 3-10 interfaces, 5-20 neighbors per node
- E2E test topologies: 3-4 interfaces, 4-5 neighbors for comprehensive validation

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

**Status**: No established project constitution - no gates to enforce

**Notes**: 
- This is a backward-compatible API enhancement to existing CRD
- Follows existing project patterns (Kubernetes operator, webhook validation, FRR config generation)
- No new architectural components or dependencies required
- Maintains existing testing approach (unit + E2E with Ginkgo/Gomega)

## Project Structure

### Documentation (this feature)

```text
specs/[###-feature]/
├── plan.md              # This file (/speckit.plan command output)
├── research.md          # Phase 0 output (/speckit.plan command)
├── data-model.md        # Phase 1 output (/speckit.plan command)
├── quickstart.md        # Phase 1 output (/speckit.plan command)
├── contracts/           # Phase 1 output (/speckit.plan command)
└── tasks.md             # Phase 2 output (/speckit.tasks command - NOT created by /speckit.plan)
```

### Source Code (repository root)

```text
api/v1alpha1/
├── underlay_types.go         # Underlay CRD definition (update Nics/Neighbors to support multiples)
├── neighbor.go               # Neighbor type definition
└── l3vni_types.go            # L3VNI CRD (may reference multiple interfaces)

internal/
├── conversion/
│   ├── validate_underlay.go  # Validation logic (remove single-entity check line 32-33)
│   ├── frr_conversion.go     # FRR config generation (handle multiple Nics/Neighbors)
│   └── host_conversion.go    # Host network config (remove Nics[0] assumption)
├── webhooks/
│   └── underlay_webhook.go   # Webhook validation (calls conversion validation)
├── controller/
│   └── routerconfiguration/  # Reconciliation logic
├── frr/
│   └── frr.go                # FRR template/config generation
└── hostnetwork/
    └── setup.go              # Network namespace setup

e2etests/
├── tests/
│   ├── webhooks.go           # E2E webhook validation tests (TRANSFORM to multi-session)
│   ├── sessions.go           # BGP session tests (TRANSFORM to multi-session)
│   ├── singlesession.go      # NEW: Single-session baseline test (1 neighbor to TOR)
│   ├── hostconfiguration.go  # Multi-interface E2E tests (TRANSFORM to multi-session)
│   └── ...                   # Other existing tests (TRANSFORM to multi-session)
└── pkg/
    └── frrk8s/               # FRR-K8s integration helpers

clab/
└── *.clab.yml                # UPDATE existing topology: add 2nd leaf node, all kind nodes connect to both leafs
```

**Structure Decision**: Kubernetes Operator project with standard controller-runtime layout. Changes span CRD definitions (api/), validation/conversion logic (internal/conversion, internal/webhooks), reconciliation (internal/controller), and E2E tests (e2etests/ with containerlab topologies in clab/).

## Complexity Tracking

> **Fill ONLY if Constitution Check has violations that must be justified**

**Status**: No violations - no complexity tracking needed

This feature follows existing project patterns without introducing new complexity:
- Uses standard Kubernetes operator patterns already in use
- Extends existing CRD rather than creating new ones
- Follows established validation and reconciliation approaches
- No new architectural components or frameworks required
