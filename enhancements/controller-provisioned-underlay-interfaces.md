# Controller-Provisioned Underlay Interfaces

## Summary

Replace the current underlay connectivity model — where the controller either
moves a physical NIC into the router network namespace or delegates to Multus —
with a single, explicit API field that lets the operator choose **how** the
router connects to the physical underlay. The new `underlayInterface` field in
the Underlay CRD uses a discriminated union (type enum + per-type sub-struct)
to support three modes: `physical` (move the entire NIC), `macvlan` (derive a
macvlan child), and `ipvlan` (derive an ipvlan child). The controller provisions
the interface directly — Multus is no longer involved in underlay connectivity
for the router.

## Motivation

### Goals

- **Deprecate Multus for router underlay connectivity.** With the persistent
  named netns model (see [router-resiliency.md](router-resiliency.md)), the
  router runs as a `hostNetwork` pod or a Podman quadlet. Neither deployment
  model integrates with Multus CNI, which operates on pod network namespaces
  managed by the container runtime. The controller must provision underlay
  interfaces itself.

- **Explicit interface type selection.** Today, the operator must coordinate
  two separate knobs — the `nics` field in the Underlay CRD and the
  `--underlay-from-multus` controller flag — to express a single intent.
  The new API collapses this into one declarative field with explicit type
  semantics.

- **Enable NIC sharing.** Moving a physical NIC into the router netns
  (`physical` mode) makes the NIC exclusively available to the router. The
  `macvlan` and `ipvlan` modes let the parent device remain on the host,
  enabling scenarios such as:
  - The host using the same NIC for management traffic.
  - Multiple router instances (future redundant-instances enhancement) deriving
    separate child interfaces from the same parent.

- **Consistent API pattern.** Follow the existing discriminated-union pattern
  established by `HostMaster` in the L2VNI CRD (`type` enum + per-type
  sub-struct), so operators encounter a familiar structure across the API
  surface.

### Non-Goals

- **SR-IOV support.** While the discriminated-union pattern makes it trivial to
  add an `sriov` variant in the future, designing or implementing SR-IOV
  support is out of scope for this enhancement.
- **Changes to workload pod networking.** Multus-attached secondary interfaces
  on application pods and KubeVirt VMs are completely unaffected.
- **Redundant router instances.** macvlan/ipvlan enables NIC sharing, which is
  a prerequisite for running multiple router instances per node, but the
  multi-instance design itself is a separate enhancement.
- **Per-node interface overrides.** All nodes matching the Underlay's
  `nodeSelector` use the same interface configuration. Per-node customization
  (e.g. different parent device names on different hardware) is handled by
  creating multiple Underlay objects with non-overlapping node selectors.

## Proposal

### Overview

Replace `UnderlaySpec.Nics []string` and the `--underlay-from-multus`
controller flag with a single `underlayInterface` field. This field is a
discriminated union: a `type` enum selects one of three modes, and a
corresponding per-type sub-struct carries the parameters relevant to that mode.

The three modes are:

| Mode | Behavior | Host NIC availability |
|------|----------|----------------------|
| `physical` | Moves the entire NIC into the router netns | NIC is exclusively owned by the router |
| `macvlan` | Creates a macvlan child from the parent device and moves the child into the router netns | Parent stays on the host |
| `ipvlan` | Creates an ipvlan child from the parent device and moves the child into the router netns | Parent stays on the host |

### API

```yaml
apiVersion: network.openperouter.io/v1alpha1
kind: Underlay
metadata:
  name: underlay
spec:
  asn: 64514
  routeridcidr: "10.0.0.0/24"
  underlayInterface:
    type: macvlan
    macvlan:
      parentDevice: eth1
      mode: bridge
  evpn:
    vtepcidr: "100.65.0.0/24"
  neighbors:
    - address: 192.168.1.1
      asn: 65000
```

#### Go Types

```go
// UnderlayInterface defines how the router connects to the physical underlay.
// The type field selects the provisioning mode; exactly one of the corresponding
// sub-structs (physical, macvlan, ipvlan) must be set to match the type.
//
// This follows the same discriminated-union pattern as HostMaster in L2VNISpec.
//
// +kubebuilder:validation:Required
// +kubebuilder:validation:XValidation:rule="(self.type == 'physical' && has(self.physical) && !has(self.macvlan) && !has(self.ipvlan)) || (self.type == 'macvlan' && has(self.macvlan) && !has(self.physical) && !has(self.ipvlan)) || (self.type == 'ipvlan' && has(self.ipvlan) && !has(self.physical) && !has(self.macvlan))",message="type/config mismatch: the sub-struct must match the type field"
type UnderlayInterface struct {
	// Type selects how the router obtains underlay connectivity.
	// +kubebuilder:validation:Enum=physical;macvlan;ipvlan
	// +kubebuilder:validation:Required
	Type string `json:"type"`

	// Physical moves a host NIC exclusively into the router netns.
	// Must be set when type is "physical".
	// +optional
	Physical *PhysicalInterface `json:"physical,omitempty"`

	// Macvlan creates a macvlan child interface from a host NIC.
	// The parent device stays on the host; the child is moved into the
	// router netns. Must be set when type is "macvlan".
	// +optional
	Macvlan *MacvlanInterface `json:"macvlan,omitempty"`

	// Ipvlan creates an ipvlan child interface from a host NIC.
	// The parent device stays on the host; the child is moved into the
	// router netns. Must be set when type is "ipvlan".
	// +optional
	Ipvlan *IpvlanInterface `json:"ipvlan,omitempty"`
}

// PhysicalInterface specifies a host NIC to move into the router netns.
// The host loses access to this device while the router owns it.
type PhysicalInterface struct {
	// InterfaceName is the name of the host NIC to move.
	// +kubebuilder:validation:Pattern=`^[a-zA-Z][a-zA-Z0-9._-]*$`
	// +kubebuilder:validation:MaxLength=15
	// +kubebuilder:validation:Required
	InterfaceName string `json:"interfaceName"`
}

// MacvlanInterface specifies a macvlan child interface to create from a
// host NIC. The parent device remains on the host.
type MacvlanInterface struct {
	// ParentDevice is the host NIC from which the macvlan is derived.
	// +kubebuilder:validation:Pattern=`^[a-zA-Z][a-zA-Z0-9._-]*$`
	// +kubebuilder:validation:MaxLength=15
	// +kubebuilder:validation:Required
	ParentDevice string `json:"parentDevice"`

	// Mode is the macvlan operating mode.
	// - bridge: all child interfaces can communicate directly (most common).
	// - vepa: all traffic goes through the parent's external bridge/switch,
	//   even between children on the same host.
	// - private: children are completely isolated from each other.
	// - passthru: a single child takes over the parent; only one child allowed.
	// +kubebuilder:validation:Enum=bridge;vepa;private;passthru
	// +kubebuilder:default=bridge
	// +optional
	Mode string `json:"mode,omitempty"`
}

// IpvlanInterface specifies an ipvlan child interface to create from a
// host NIC. The parent device remains on the host. Unlike macvlan, ipvlan
// shares the parent's MAC address — only one MAC appears on the wire.
type IpvlanInterface struct {
	// ParentDevice is the host NIC from which the ipvlan is derived.
	// +kubebuilder:validation:Pattern=`^[a-zA-Z][a-zA-Z0-9._-]*$`
	// +kubebuilder:validation:MaxLength=15
	// +kubebuilder:validation:Required
	ParentDevice string `json:"parentDevice"`

	// Mode is the ipvlan operating mode.
	// - l2: operates at L2, child participates in ARP (most common).
	// - l3: operates at L3, no ARP/NDP — the parent acts as a router.
	// - l3s: like l3, but with netfilter/conntrack integration.
	// +kubebuilder:validation:Enum=l2;l3;l3s
	// +kubebuilder:default=l2
	// +optional
	Mode string `json:"mode,omitempty"`
}
```

#### Examples

**Physical (current behavior, replaces `nics: [eth1]`):**
```yaml
underlayInterface:
  type: physical
  physical:
    interfaceName: eth1
```

**Macvlan (replaces Multus-based underlay):**
```yaml
underlayInterface:
  type: macvlan
  macvlan:
    parentDevice: eth1
    mode: bridge
```

**Ipvlan (shared MAC, useful when MAC learning is constrained):**
```yaml
underlayInterface:
  type: ipvlan
  ipvlan:
    parentDevice: eth1
    mode: l2
```

### User Stories

#### Story 1: Operator Migrating from Multus-Based Underlay

As a cluster operator currently using Multus to plumb the underlay NIC into the
router pod, I want to switch to the named netns deployment model without losing
underlay connectivity, so that I benefit from persistent data plane resiliency.

**Before (Multus):**
```yaml
# Underlay CRD — nics omitted, Multus handles it
spec:
  asn: 64514
  evpn:
    vtepcidr: "100.65.0.0/24"
  neighbors:
    - address: 192.168.1.1
      asn: 65000
---
# Separate NetworkAttachmentDefinition + controller flag
# --underlay-from-multus=true
```

**After (controller-provisioned macvlan):**
```yaml
spec:
  asn: 64514
  underlayInterface:
    type: macvlan
    macvlan:
      parentDevice: eth1
      mode: bridge
  evpn:
    vtepcidr: "100.65.0.0/24"
  neighbors:
    - address: 192.168.1.1
      asn: 65000
```

The intent is fully captured in one CRD. No separate NAD, no controller flag.

#### Story 2: Operator Using Dedicated Physical NIC

As a cluster operator with a dedicated physical NIC for underlay traffic, I want
to move it exclusively into the router netns for maximum performance and
isolation.

```yaml
underlayInterface:
  type: physical
  physical:
    interfaceName: eth1
```

This is equivalent to the current `nics: [eth1]` behavior.

#### Story 3: Operator Needing Shared NIC

As a cluster operator on hardware with limited NICs, I want the host and the
router to share the same physical NIC, so that I don't need a dedicated NIC for
underlay traffic.

```yaml
underlayInterface:
  type: macvlan
  macvlan:
    parentDevice: eth1
    mode: bridge
```

The host retains `eth1` for management; the router gets a macvlan child with
its own MAC and IP addressing.

#### Story 4: Operator on a Network with MAC Restrictions

As a cluster operator on a network that limits the number of MAC addresses per
port (e.g. certain cloud or campus environments), I want the router's underlay
interface to share the physical NIC's MAC address.

```yaml
underlayInterface:
  type: ipvlan
  ipvlan:
    parentDevice: eth1
    mode: l2
```

Only one MAC appears on the wire regardless of how many ipvlan children exist.

### Risks and Mitigations

| Risk | Mitigation |
|------|------------|
| Breaking change to `nics` field | This is a `v1alpha1` API with no stability guarantees. Provide clear migration documentation. The `physical` mode is a direct equivalent. |
| macvlan/ipvlan performance overhead vs physical NIC | Minimal for macvlan in bridge mode (~1-2% throughput impact in benchmarks). Document that `physical` mode is preferred when a dedicated NIC is available. |
| macvlan child cannot communicate with parent on the same host | This is a well-known Linux kernel constraint. Document it: if the host needs to reach the router via the underlay NIC, use `ipvlan` or a separate veth pair. |
| ipvlan requires parent and children to share the same L3 subnet | Document the L3 implications of ipvlan mode selection. |
| Operator forgets to set the sub-struct matching the type | CEL validation rejects the resource at admission time with a clear error message. |

## Design Details

### API Pattern: Discriminated Union

The `UnderlayInterface` struct uses the same discriminated-union pattern as
`HostMaster` in the L2VNI CRD:

```go
// L2VNI's HostMaster (existing pattern)
type HostMaster struct {
    Type        string             `json:"type"`         // "linux-bridge" | "ovs-bridge"
    LinuxBridge *LinuxBridgeConfig `json:"linuxBridge,omitempty"`
    OVSBridge   *OVSBridgeConfig   `json:"ovsBridge,omitempty"`
}

// UnderlayInterface (proposed — same pattern)
type UnderlayInterface struct {
    Type     string             `json:"type"`     // "physical" | "macvlan" | "ipvlan"
    Physical *PhysicalInterface `json:"physical,omitempty"`
    Macvlan  *MacvlanInterface  `json:"macvlan,omitempty"`
    Ipvlan   *IpvlanInterface   `json:"ipvlan,omitempty"`
}
```

A CEL validation on the struct enforces that exactly the sub-struct matching the
`type` enum is set. This gives operators:
- An explicit discriminator (`type`) that makes the intent unambiguous in YAML
  and in code (switch on `Type`, not on which pointer is non-nil).
- Per-type fields that are only present when relevant — no unused `macvlanMode`
  field when the type is `physical`.
- Clear `kubectl explain` output — each sub-struct is its own section with
  contextual documentation.

### Controller Provisioning Flow

The controller's `SetupUnderlay` function is extended to handle the three
interface types. The provisioning logic runs inside the controller's
reconciliation loop, in the same phase where it currently moves the physical
NIC.

#### Physical Mode

No change from current behavior:

1. `moveUnderlayInterface(interfaceName, targetNS)` — moves the NIC from the
   host netns into the router netns.
2. Assigns the marker address (`172.16.1.1/32`) for detection.
3. Sets the interface UP.

#### Macvlan Mode

1. Look up the parent device on the host by name.
2. Create a macvlan link: `netlink.LinkAdd(&netlink.Macvlan{...})` with the
   specified mode and parent.
3. Move the macvlan child into the router netns.
4. Assign the marker address for detection.
5. Set the interface UP.
6. The parent device stays on the host, untouched.

#### Ipvlan Mode

1. Look up the parent device on the host by name.
2. Create an ipvlan link: `netlink.LinkAdd(&netlink.IPVlan{...})` with the
   specified mode and parent.
3. Move the ipvlan child into the router netns.
4. Assign the marker address for detection.
5. Set the interface UP.
6. The parent device stays on the host, untouched.

### Naming Convention

The macvlan/ipvlan child interface is created with a deterministic name derived
from the parent device. The interface is renamed inside the router netns to
match the `interfaceName` / `parentDevice` so that FRR configuration and
existing references work consistently:

- **Physical**: interface keeps its original name (e.g. `eth1`).
- **Macvlan/Ipvlan**: child is renamed to `ul-<parentDevice>` in the router
  netns (e.g. `ul-eth1`). The `ul-` prefix signals "underlay" and avoids
  collisions with the parent name (which lives in a different netns but could
  cause confusion in logs and debugging).

### Idempotency and Reconciliation

The marker address (`172.16.1.1/32`) detection mechanism already used for
physical NICs applies identically to macvlan/ipvlan children:

- If the marker is found on the expected interface in the router netns, the
  underlay is already configured — no-op.
- If the marker is found on a **different** interface (e.g. the operator changed
  `parentDevice`), return `UnderlayExistsError` to signal that the netns must be
  rebuilt.
- If no marker is found, provision the interface.

### Underlay Change Detection

Changing the underlay interface type or parent device is a destructive operation
that requires a netns rebuild. The controller detects this via the
`UnderlayExistsError` returned by `SetupUnderlay` when the configured underlay
doesn't match the desired one. This triggers `HandleNonRecoverableError`, which
deletes the netns and lets the next reconciliation rebuild it from scratch.

### Removal of `--underlay-from-multus` Flag

The `--underlay-from-multus` controller flag becomes unnecessary. Its current
purpose is to tell the controller "don't validate `nics` — Multus will provide
the underlay interface." With the new API, the `type` field is the single source
of truth for how the underlay is provisioned. The flag is removed.

### Changes Required

| Component | Current | Proposed | Effort |
|-----------|---------|----------|--------|
| `UnderlaySpec` | `Nics []string` | `UnderlayInterface *UnderlayInterface` | API type change |
| New types | — | `UnderlayInterface`, `PhysicalInterface`, `MacvlanInterface`, `IpvlanInterface` | New Go types |
| CEL validation | NIC name pattern on items | Discriminated-union match (type ↔ sub-struct) | New validation rule |
| `--underlay-from-multus` flag | Boolean flag in `cmd/hostcontroller/main.go` | Removed | Delete flag + wiring |
| `SetupUnderlay()` | Moves physical NIC | Switch on type: move NIC / create macvlan / create ipvlan | Extend function |
| `UnderlayParams` | `UnderlayInterface string` | Carries full `UnderlayInterface` struct | Struct change |
| `APItoHostConfig()` | Reads `Nics[0]` + `underlayFromMultus` | Reads `UnderlayInterface.Type` + sub-struct fields | Conversion update |
| Webhook validation | NIC name format | Type/sub-struct consistency | Update webhook |
| Helm values schema | `nics` list | `underlayInterface` block | Values schema change |
| Helm templates | Renders `nics` | Renders `underlayInterface` with type | Template update |
| E2E test setup | `CreateMacvlanNad` for Multus underlay | Direct `underlayInterface` in CRD | Test simplification |
| Documentation | Describes `nics` + Multus flag | Describes three interface modes | Doc rewrite |

### Migration Path

Since this is a `v1alpha1` API, no automatic migration is provided. The
migration is a one-time CRD update:

| Current configuration | New configuration |
|----------------------|-------------------|
| `nics: [eth1]` | `underlayInterface: {type: physical, physical: {interfaceName: eth1}}` |
| `nics: []` + `--underlay-from-multus=true` + Multus NAD for underlay | `underlayInterface: {type: macvlan, macvlan: {parentDevice: eth1, mode: bridge}}` |

The release notes will include a migration guide with examples for both paths.

### Test Plan

- **Unit tests**: `UnderlayInterface` CEL validation — verify that:
  - `type: physical` with `physical` set is accepted.
  - `type: macvlan` with `macvlan` set is accepted.
  - `type: ipvlan` with `ipvlan` set is accepted.
  - Type/sub-struct mismatch is rejected with a clear message.
  - Missing sub-struct is rejected.
  - Multiple sub-structs set simultaneously is rejected.
- **Unit tests**: `SetupUnderlay` with macvlan and ipvlan modes — verify
  interface creation, parent device lookup, namespace move, marker assignment,
  and idempotent re-runs (using network namespace mocks or netns test fixtures).
- **Integration tests**: In a real (non-mock) netns environment, verify:
  - macvlan child is created from the parent and moved into the target netns.
  - ipvlan child is created from the parent and moved into the target netns.
  - Parent device remains in the host netns and is functional.
  - FRR discovers the interface inside the netns.
  - `HasUnderlayInterface` correctly detects the provisioned interface.
- **Reconciliation tests**: Verify that changing the underlay type (e.g.
  `physical` to `macvlan`) triggers `UnderlayExistsError` and the controller
  handles it via netns rebuild.
- **E2E tests**: Full traffic test with each interface type:
  - `physical`: existing E2E coverage applies.
  - `macvlan`: underlay traffic flows through the macvlan child; VXLAN tunnels
    establish and carry EVPN routes.
  - `ipvlan`: underlay traffic flows through the ipvlan child; verify that
    shared MAC does not interfere with EVPN Type-2 route advertisement.
- **Migration test**: Apply an Underlay with the old `nics` field, upgrade the
  controller, apply the new `underlayInterface` field, verify convergence.

### Graduation Criteria

#### Alpha

- `UnderlayInterface` type and sub-structs added to the API with CEL
  validation.
- `Nics` field removed from `UnderlaySpec`.
- `--underlay-from-multus` flag removed from the controller.
- `SetupUnderlay` extended to handle `physical`, `macvlan`, and `ipvlan` modes.
- `physical` mode is functionally equivalent to the current `nics` behavior.
- Basic unit and integration tests for all three modes.

#### Beta

- E2E tests covering all three modes with real traffic verification.
- Helm chart and operator bindata updated with new values schema.
- Migration guide published in release notes.
- macvlan and ipvlan modes validated in at least one real-hardware environment
  (not just Kind/containerlab).

#### GA

- Production validation across multiple deployment environments and NIC
  vendors.
- Performance benchmarks comparing physical, macvlan, and ipvlan modes
  published.
- Documentation covers mode selection guidance (when to use each mode).

## Drawbacks

- **Breaking API change.** Operators must update their Underlay CRD when
  upgrading. Since this is `v1alpha1` and the migration is a straightforward
  field rename, this is acceptable.
- **Slightly more verbose YAML.** The nested sub-struct adds one level of
  indentation compared to `nics: [eth1]`. This is the intentional trade-off for
  type safety — every field visible in the YAML is relevant to the selected
  mode.
- **macvlan/ipvlan have known kernel constraints.** macvlan children cannot
  communicate with the parent on the same host. ipvlan children share the
  parent's MAC address, which affects ARP behavior. These are Linux kernel
  properties, not API design issues, but they must be documented so operators
  make informed choices.

## Alternatives

### Alternative 1: Flat Discriminated Union (Single Struct, No Sub-Structs)

Use a `type` enum with mode-specific fields as optional flat fields on the same
struct:

```yaml
underlayInterface:
  parentDevice: eth1
  type: macvlan
  macvlanMode: bridge
```

```go
type UnderlayInterface struct {
    ParentDevice string       `json:"parentDevice"`
    Type         string       `json:"type"`
    MacvlanMode  *MacvlanMode `json:"macvlanMode,omitempty"`
    IpvlanMode   *IpvlanMode  `json:"ipvlanMode,omitempty"`
}
```

**Why not chosen:**

- **Unused fields leak into the YAML.** `macvlanMode` appears in the struct
  even when `type: physical`. Operators see it in `kubectl explain` and wonder
  if they should set it. Conditional field relevance is a source of confusion.
- **Conditional validation.** "macvlanMode required when type=macvlan AND
  forbidden when type=physical" is a chain of rules. The sub-struct pattern
  makes invalid combinations structurally impossible.
- **Naming ambiguity.** `parentDevice` means "device to move" for `physical`
  and "device to derive from" for `macvlan`/`ipvlan`. The sub-struct pattern
  uses `interfaceName` for physical and `parentDevice` for macvlan/ipvlan,
  matching the semantic difference.
- **Extensibility.** Adding SR-IOV would add `numVFs`, `resourceName`,
  `pfName`, etc. as flat optional fields — the struct grows unwieldy. With
  sub-structs, `Sriov *SriovInterface` is self-contained.

### Alternative 2: One-of Sub-Structs Without Type Enum

Use mutually exclusive sub-struct pointers without an explicit `type`
discriminator, following the Kubernetes `VolumeSource` pattern:

```yaml
underlayInterface:
  macvlan:
    parentDevice: eth1
    mode: bridge
```

```go
type UnderlayInterface struct {
    Physical *PhysicalInterface `json:"physical,omitempty"`
    Macvlan  *MacvlanInterface  `json:"macvlan,omitempty"`
    Ipvlan   *IpvlanInterface   `json:"ipvlan,omitempty"`
}
```

**Why not chosen:**

- **No explicit discriminator.** Code must check which pointer is non-nil to
  determine the type. A `Type` enum makes switch statements clean and
  serialization unambiguous.
- **Inconsistent with existing API.** The `HostMaster` struct in L2VNI already
  uses the type-enum + sub-struct pattern. Mixing two different union patterns
  in the same API surface creates inconsistency.
- **JSON round-trip ambiguity.** Without a discriminator, a malformed resource
  with two sub-structs set is harder to diagnose — the error is "exactly one
  must be set" rather than "the sub-struct must match the type."

### Alternative 3: Keep Multus for Underlay

Continue using Multus `NetworkAttachmentDefinition` to plumb the underlay
interface into the router pod.

**Why not chosen:**

- **Incompatible with named netns.** Multus operates on pod network namespaces
  managed by the container runtime. With `hostNetwork: true` pods or Podman
  quadlets, the router does not have a runtime-managed netns for Multus to
  target.
- **Split configuration.** The underlay intent is split across the Underlay CRD,
  a `NetworkAttachmentDefinition`, and a controller flag. This makes the system
  harder to reason about and debug.
- **External dependency.** Multus is an optional cluster component. Requiring it
  for router underlay connectivity adds a dependency that not all deployments
  want.

## Implementation History

- 2026-04-21: Initial proposal drafted.
