# Implementation Plan: Reimplement Existing VNIs with VLAN-Aware Bridges

**Branch**: `main` (actual Git branch) | **Feature ID**: `001-reimplement-vlan-aware-bridge` | **Date**: 2026-10-27 | **Spec**: [spec.md](spec.md)

**Input**: `specs/001-reimplement-vlan-aware-bridge/spec.md`

## Summary

Replace the Linux L2VNI/L3VNI internals with dedicated VLAN-filtering bridges and metadata-mode VXLAN devices using a fixed private VLAN ID of 1. Preserve all public configuration, routing behavior, host attachments, and existing e2e expectations. There is no trunk API, shared bridge, VLAN allocator, or new feature.

Preserve names that existing tests observe: `vni<VNI>` remains the VXLAN device; `br-pe-<VNI>` remains the neighbor-owning gateway interface. A new private `br-vl-<VNI>` bridge performs switching. VLAN interfaces bind domains to existing VRFs; routed L2 gateways use a macvlan over an addressless VLAN interface. See [data-model.md](data-model.md) for exact roles.

## Technical Context

**Language/Version**: Go 1.26.4, as pinned in `go.mod`.

**Primary Dependencies**: Existing netlink v1.3.1, netns v0.0.5, x/sys v0.48.0, controller-runtime v0.25.2, and FRR 10.6.0 image. No dependency upgrade planned.

**Storage**: Existing Kubernetes resources and static YAML remain authoritative. Kernel interfaces hold derived state; no persisted VLAN assignments or new status fields.

**Testing**: Go tests, Ginkgo/Gomega privileged namespace tests via `make test`, `make lint`, and unchanged e2e suites through `make e2etests` and the existing hostmode-boot target.

**Target Platform**: Linux kernel with external VXLAN and VNI receive filtering (upstream support present in 5.18), VLAN filtering, VRF, and macvlan. Pod and systemd/host kernel dataplanes are changed. Grout retains its separate implementation and regression coverage.

**Project Type**: Kubernetes networking controller/operator with Linux dataplane and optional static configuration.

**Performance Goals**: Preserve current MTU, test deadlines, recovery, and forwarding behavior. No new throughput target or benchmark project.

**Constraints**: Zero public API/schema/default/validation/status changes; no new e2e exclusions or relaxed assertions. Preserve interface names observed by the current suite. Required external+vnifilter creation must be atomic because vnifilter is immutable.

**Scale/Scope**: Existing VNI range 1–16777215 and current node-selection/uniqueness rules. VLAN 1 is reused across independent dedicated bridges; the VLAN range does not cap node-wide VNI counts.

## Constitution Check

*Pre-research gate: PASS. Post-design gate: PASS.*

`.specify/memory/constitution.md` is an unfilled template, so no ratified principles can be inferred from its examples. Apply the user's scope, `AGENTS.md`, and `CODING_STYLE.md` directly:

| Gate | Design evidence | Result |
| --- | --- | --- |
| Preserve public behavior | Existing API, static types, validation, routing policy, and status stay unchanged | Pass |
| Bound scope | Dedicated devices, fixed private VLAN, no trunk or allocator | Pass |
| Reuse repository mechanisms | Existing conversion, setup ordering, errors, refresher, and cleanup paths retained | Pass |
| Verify relevant behavior | Privileged tests plus unchanged pod/host/boot suites and Grout regression | Pass |
| Keep complexity justified | Small rtnetlink helper fills verified gaps in pinned netlink; gateway/SVI split preserves tested names | Pass |

No unjustified gate violations. Runtime compatibility is an implementation acceptance gate, not a claim established by planning.

## Project Structure

### Documentation (this feature)

```text
specs/001-reimplement-vlan-aware-bridge/
├── spec.md
├── plan.md
├── research.md
├── data-model.md
├── quickstart.md
├── contracts/
│   └── compatibility.md
└── checklists/
    └── requirements.md
```

`tasks.md` is generated later by `$speckit-tasks`.

### Source Code (repository root)

```text
internal/hostnetwork/
├── bridge.go                 # Private switching bridge and existing name/MAC helpers
├── vxlan.go                  # External-mode creation, comparison, and mapping reconciliation
├── vxlan_filter_linux.go     # New narrow rtnetlink support; filename proposed
├── vlan.go                   # New internal VLAN/SVI/gateway helpers; filename proposed
├── vni.go                    # Setup ordering, legacy preflight, owned cleanup
├── vni_test.go               # Privileged topology/lifecycle regression proof
├── bridge_test.go
└── bridgerefresh/            # Preserve neighbor-owner name and behavior; regression tests
internal/controller/routerconfiguration/
├── host_config.go           # Kernel-only preflight and existing resource failure handling
└── grout_config.go          # Separate existing dataplane; regression boundary
api/v1alpha1/                 # Public contract: unchanged
api/static/                  # Public contract: unchanged
internal/conversion/         # Existing params sufficient; avoid new configuration fields
internal/frrconfig/           # Preserve policy generation; verify discovery end-to-end
config/, charts/, operator/  # Public schemas/manifests: unchanged
e2etests/                    # Existing scenarios/assertions/filters: unchanged
```

**Structure Decision**: Extend existing Linux setup helpers; keep the missing UAPI support private to hostnetwork. Reuse `BridgeName` for `br-pe-<VNI>` so the shared refresher and Grout retain their current lookup. Add a distinct helper for the private switching bridge. Do not introduce a generic interface manager or speculative trunk abstraction.

## Phase 0: Research Results

[research.md](research.md) resolves the internal VLAN value, interface naming and gateway ownership, VRF placement, missing netlink operations, lifecycle behavior, MTU implications, platform prerequisites, and validation matrix. No unresolved clarification remains.

## Phase 1: Design

### Setup and reconciliation

1. Keep API/static conversion and underlay setup. Before provisioning the first new VXLAN, inspect owned legacy namespace devices. Stop affected refreshers and remove all owned legacy fixed-ID VXLANs before creating external devices on their ports. Remove old owned `br-pe-*` bridges to free endpoint names, retaining host attachments where possible. Do not remove unrelated interfaces or external host bridges. This is a bounded rebuild, not a hitless migration framework.
2. Retain existing L3-before-L2 ordering and VRF creation. Build `br-vl-<VNI>` with VLAN filtering and default PVID 0, without a VRF master. Add explicit VLAN 1 self membership.
3. Create `vni<VNI>` with external metadata mode and VNI filtering in the same request, preserving VTEP, underlay device, effective UDP port, and learning settings. Configure VLAN tunnel mode, VLAN 1 membership, VLAN-to-VNI mapping, and exactly the configured VNI receive filter. Preserve neighbor suppression and disabled VXLAN/bridge-port learning.
4. Build the routed interface chain defined in the data model. Only routed endpoint devices join VRFs; switching bridges do not. Gateway IPs and the existing per-VNI anycast MAC belong to `br-pe-<VNI>`. Install the gateway's VLAN-scoped local FDB entry. Addressless L3 interfaces have IPv6 address generation disabled.
5. Keep existing L2 host/router veth names and host-master setup. Set router-side VLAN 1 PVID/untagged membership so host traffic stays untagged. Keep tunnel payload MTUs unchanged; internal VLAN classification must not add an on-wire inner tag.
6. Before declaring success, independently verify link attributes, VLAN memberships, mapping, receive filter, VRF binding, addresses, and local gateway FDB state. Repair missing mutable state without recreating a matching link or sending unnecessary link/address changes. Immutable VXLAN mismatches require replacing only that owned VXLAN and restoring its mapping/filter.
7. Start the existing refresher after successful L2 setup. It continues resolving `br-pe-<VNI>`, now the real gateway neighbor owner. Retain current failure reasons and per-resource continuation behavior.

### MAC identity and address handling

Pin a locally generated unicast router MAC on each private bridge at creation and reuse it across reconciles, its VXLAN device, and addressless SVI. Do not derive router MACs solely from VNI, which would make them identical on different nodes. Preserve `BridgeFixedMAC(VNI)` only for the distributed L2 gateway endpoint. Disable automatic IPv6 addresses on backing/addressless devices; preserve configured gateway address families and the existing host-session veth behavior. No new public MAC setting is added.

### Cleanup and recovery

Stop affected refreshers before deleting their gateway devices. Remove owned macvlan endpoints, VLAN interfaces, VXLANs, and private bridges in dependency order; tolerate links already removed through parent deletion. Extend name/type recognition to both old and new owned topologies. Preserve managed/external host bridge ownership and existing VRF sharing. Underlay replacement and complete cleanup must remove every new owned device. No silent fallback to the old dataplane is allowed if required kernel operations fail.

### Verification and acceptance mapping

| Requirements | Planned proof |
| --- | --- |
| FR-001, FR-011 | API/static/schema diff review; existing validation/conversion tests; no new configuration kinds or fields |
| FR-002, FR-003, FR-005 | Privileged topology tests for L2/L3, VLAN reuse, VNI >4094, metadata/filter/mapping state |
| FR-004, FR-006 | Existing Linux/OVS/omitted host-master tests; host and overlay traffic capture |
| FR-007, FR-008 | Existing disconnected EVPN, shared-L3VNI IRB, L3VPN, RT/RD, IPv6 underlay and same-port scenarios |
| FR-009 | Fault repair, deletion, repeated setup, restart, bridge refresh and migration tests |
| FR-010 | Full unchanged e2e suite in existing CI modes; unchanged packaging lanes and filters |

Unit/privileged tests may change assertions about private topology and add meaningful regression cases. Existing e2e manifests, scenarios, assertions, timeouts, and filters remain the baseline. [quickstart.md](quickstart.md) provides commands and expected evidence.

## Complexity Tracking

No constitution violations. Two necessary costs are explicit: a private VLAN/SVI/gateway chain preserves neighbor ownership and routing, and a narrow raw rtnetlink helper supplies creation/query/filter operations absent from netlink v1.3.1. No new dependency, persisted allocation, shared-device ownership, or generic networking framework is planned.
