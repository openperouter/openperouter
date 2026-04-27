# API Improvements

## Summary

This enhancement covers API improvements to the OpenPERouter CRDs. It
consolidates breaking redesigns (acceptable in `v1alpha1`), important fixes,
and quality improvements into a single document. Items already handled by the
kube-api-linter ([PR #313](https://github.com/openperouter/openperouter/pull/313))
are out of scope. Status subresources are tracked in a
[separate enhancement](status-crd.md).

## Motivation

### Goals

- Redesign API fields whose current shape would lock in bad semantics.
- Fix security, usability, and validation gaps.
- Establish a stable API group name before it becomes immutable.

### Non-Goals

- Status reporting and configuration resilience (see [status-crd.md](status-crd.md)).
- Linter-enforceable fixes: SSA markers, JSON tag casing, required/optional
  consistency, integer type widths, duration types, godoc field-name prefixes
  (all covered by [PR #313](https://github.com/openperouter/openperouter/pull/313)).

## Proposal

### API Redesign (Breaking Changes)

These are breaking changes to `v1alpha1` fields whose current design would
create permanent API debt if left unchanged.

#### Routing Domain / VRF Semantics on L2VNI

**Problem:** `L2VNISpec.VRF` defaults to `metadata.name` when unset, conflating
L2VNI-attached-to-VRF with pure-L2-overlay intent. The VRF association also
relies on fragile string matching between L2VNI and L3VNI resources.

**Proposal:** Replace `VRF *string` + `L2GatewayIPs` with a `routingDomain`
struct that references the L3VNI CR by `metadata.name`. Omitting
`routingDomain` explicitly means "disconnected overlay". `GatewayIPs` moves
inside `RoutingDomain` since gateway IPs without a routing domain are invalid.
If the referenced L3VNI does not exist, the controller rejects the L2VNI
(`Ready=False, Reason=L3VNINotFound`).

```go
type L2VNISpec struct {
    // RoutingDomain optionally attaches this L2VNI to a routing domain
    // provided by an L3VNI. When omitted, the L2VNI is a disconnected
    // overlay (east-west L2 only, no VRF, no gateway).
    // +optional
    RoutingDomain *RoutingDomain `json:"routingDomain,omitempty"`

    // ... other fields unchanged
}

type RoutingDomain struct {
    // L3VNI is the name of the L3VNI resource (metadata.name) in the same
    // namespace that provides the routing domain for this L2VNI.
    // The VRF configuration (name, VNI, route targets) is owned entirely
    // by the referenced L3VNI -- the L2VNI does not duplicate it.
    // +kubebuilder:validation:Required
    L3VNI string `json:"l3vni"`

    // GatewayIPs is a list of IP addresses in CIDR notation for the
    // distributed anycast gateway on this L2 segment's bridge (IRB
    // interface). Maximum 2 (one IPv4, one IPv6).
    // +optional
    // +kubebuilder:validation:MaxItems=2
    // +kubebuilder:validation:XValidation:rule="self == oldSelf",message="gatewayIPs cannot be changed"
    GatewayIPs []string `json:"gatewayIPs,omitempty"`
}
```

**Examples:**

```yaml
# L2VNI with routing domain -- managed bridge (auto-named br-hs-<VNI>)
spec:
  vni: 100
  routingDomain:
    l3vni: tenant-a       # references L3VNI by metadata.name
    gatewayIPs:
      - 10.100.0.1/24
      - fd10:100::1/64
  nodeUplink:
    type: LinuxBridge
    linuxBridge:
      managementPolicy: Managed
---
# Managed bridge with explicit name
spec:
  vni: 150
  routingDomain:
    l3vni: tenant-a
  nodeUplink:
    type: LinuxBridge
    linuxBridge:
      managementPolicy: Managed
      name: br-web
---
# Disconnected overlay -- user-provided bridge
spec:
  vni: 200
  nodeUplink:
    type: LinuxBridge
    linuxBridge:
      managementPolicy: Unmanaged
      name: br-isolated
```

#### API Group Name

**Problem:** The current API group `openpe.openperouter.github.io` uses a
`github.io` domain, which looks provisional. Changing it later is a massive
breaking change.

**Proposal:** Rename to `network.openperouter.io` via find-and-replace
(`groupversion_info.go`, webhooks, RBAC, Helm, docs). No conversion webhook
needed in `v1alpha1`; release notes must document the rename.

#### Replace `nics []string` with `interfaces`

**Problem:** Underlay connectivity is split between `UnderlaySpec.Nics []string`
and the `--underlay-from-multus` controller flag, making the API incomplete and
the two mechanisms not mutually exclusive in the schema.

**Proposal:** Replace both with `interfaces`, a discriminated-union slice on
`UnderlaySpec`. Currently one mode (`netdev`) is defined; macvlan/ipvlan modes
will be added as part of [router resiliency](router-resiliency.md).

```go
type UnderlaySpec struct {
    // ...

    // interfaces is the list of interfaces the router uses for underlay
    // connectivity. Each entry is a discriminated union describing how the
    // interface is obtained.
    // +kubebuilder:validation:MinItems=1
    // +required
    Interfaces []UnderlayInterface `json:"interfaces"`
}

// UnderlayInterface defines how the router obtains a single underlay link.
// Exactly one of the sub-structs must match the type field.
// The union is designed to be extended with future modes (e.g. macvlan,
// ipvlan) for controller-provisioned interfaces.
//
// +kubebuilder:validation:XValidation:rule="self.type == 'Netdev' == has(self.netdev)",message="netdev field mismatch"
// +union
type UnderlayInterface struct {
    // type selects how the router obtains this underlay link.
    // +kubebuilder:validation:Enum=Netdev
    // +kubebuilder:validation:Required
    // +unionDiscriminator
    Type string `json:"type"`

    // netdev moves an existing host network device into the router netns.
    // The device can be of any kind (physical NIC, bridge, macvlan, etc.).
    // +optional
    Netdev *NetdevInterface `json:"netdev,omitempty"`
}

type NetdevInterface struct {
    // interfaceName is the name of the host network device to move into
    // the router netns.
    // +kubebuilder:validation:Pattern=`^[a-zA-Z][a-zA-Z0-9._-]*$`
    // +kubebuilder:validation:MaxLength=15
    // +kubebuilder:validation:Required
    InterfaceName string `json:"interfaceName"`
}
```

**Example:**

```yaml
interfaces:
  - type: Netdev
    netdev:
      interfaceName: eth1
```

#### Remove Non-Inclusive Terminology

**Problem:** Several API types and comments use non-inclusive language:

- `HostMaster` / `hostmaster` uses "master" naming and is not
  implementation-agnostic.
- `RunOnMaster` / `runOnMaster` on the operator CRD
  (`OpenPERouterSpec`) uses "master" instead of "control-plane".
- Godoc comments on `L2VNISpec.HostMaster` and `L2GatewayIPs` use
  "enslaved to" instead of "attached to".

**Proposal:**

Rename types and fields:

```go
// Before
HostMaster *HostMaster `json:"hostmaster"`
// After
HostUplink *HostUplink `json:"nodeUplink,omitempty"`

// Operator CRD -- before
RunOnMaster bool `json:"runOnMaster,omitempty"`
// After
RunOnControlPlane bool `json:"runOnControlPlane,omitempty"`
```

Replace "enslaved" with "attached" in all API godoc comments (e.g.
"the veth should be attached to" instead of "the veth should be
enslaved to").

#### Align Enum Values

**Problem:** Several enum values use lowercase or kebab-case, align them to 
PascalCase.

**Proposal:** Rename all enum values to PascalCase:

| Field | Current | Proposed |
|-------|---------|----------|
| `Neighbor.Type` | `external`, `internal` | `External`, `Internal` |
| `HostSession.HostType` | `external`, `internal` | `External`, `Internal` |
| `HostUplink.Type` | `linux-bridge`, `ovs-bridge` | `LinuxBridge`, `OVSBridge` |
| `UnderlayInterface.Type` | _(new)_ | `Netdev` |

#### Replace Boolean Fields

**Problem:** Several boolean fields violate the
[Kubernetes convention against booleans](https://github.com/kubernetes/community/blob/main/contributors/devel/sig-architecture/api-conventions.md#primitive-types).
Booleans cannot be extended to a third state and their `true`/`false` values
do not convey intent.

**Proposal:**

Replace `EBGPMultiHop bool` on `Neighbor` with a `features` set. This is
extensible for future neighbor-level toggles:

```go
// +kubebuilder:validation:Enum=EBGPMultiHop
type NeighborFeature string

type Neighbor struct {
    // ...

    // features is the set of optional features to enable on this neighbor.
    // EBGPMultiHop: the peer is multiple hops away (RFC 4271).
    // +optional
    // +listType=set
    Features []NeighborFeature `json:"features,omitempty"`
}
```

Replace `EchoMode *bool` and `PassiveMode *bool` on `BFDSettings` with a
`modes` set. Echo and passive are independent and can be combined (RFC 5880).
The set is extensible for future BFD modes (e.g. `Demand`):

```go
// +kubebuilder:validation:Enum=Echo;Passive
type BFDMode string

type BFDSettings struct {
    // ...

    // modes is the set of optional BFD session modes to enable.
    // Echo: use echo packets for liveness detection (single-hop only,
    //       RFC 5880 Section 6.4).
    // Passive: wait for the peer to initiate the session before
    //          transmitting (RFC 5880 Section 6.1).
    // When omitted the session uses active non-echo mode.
    // +optional
    // +listType=set
    Modes []BFDMode `json:"modes,omitempty"`
}
```

Echo mode is prohibited on multi-hop sessions (RFC 5883 Section 3). Add a
CEL rule on `Neighbor` to enforce this:

```go
// +kubebuilder:validation:XValidation:rule="!has(self.features) || !self.features.exists(f, f == 'EBGPMultiHop') || !has(self.bfd) || !has(self.bfd.modes) || !self.bfd.modes.exists(m, m == 'Echo')",message="echo mode cannot be used with multi-hop sessions (RFC 5883)"
```

Replace `AutoCreate bool` with a `managementPolicy` enum (`Managed`/`Unmanaged`),
following the OpenShift `dnsManagementPolicy` precedent on IngressController:

```go
// BridgeManagementPolicy determines how the bridge is provisioned.
// +kubebuilder:validation:Enum=Managed;Unmanaged
type BridgeManagementPolicy string

const (
    // BridgeManagementPolicyManaged means the controller creates and owns
    // the bridge, and deletes it when the L2VNI is removed. If Name is
    // omitted, the bridge is auto-named br-hs-<VNI>. If Name is
    // provided, the controller creates the bridge with that name.
    BridgeManagementPolicyManaged BridgeManagementPolicy = "Managed"

    // BridgeManagementPolicyUnmanaged means the user provides a
    // pre-existing bridge via the Name field. The controller does not
    // create or delete it; only veth ports are attached/detached.
    BridgeManagementPolicyUnmanaged BridgeManagementPolicy = "Unmanaged"
)

// LinuxBridgeConfig contains configuration for Linux bridge type.
// +kubebuilder:validation:XValidation:rule="self.managementPolicy == 'Managed' || (has(self.name) && self.name != '')",message="name is required when managementPolicy is Unmanaged"
type LinuxBridgeConfig struct {
    // managementPolicy determines if the bridge is managed by the
    // controller or provided by the user.
    // +kubebuilder:validation:Required
    ManagementPolicy BridgeManagementPolicy `json:"managementPolicy"`

    // name of the Linux bridge interface.
    // Optional when Managed (defaults to br-hs-<VNI>).
    // Required when Unmanaged.
    // +kubebuilder:validation:Pattern=`^[a-zA-Z][a-zA-Z0-9_-]*$`
    // +kubebuilder:validation:MaxLength=15
    // +optional
    Name string `json:"name,omitempty"`
}

// OVSBridgeConfig contains configuration for OVS bridge type.
// +kubebuilder:validation:XValidation:rule="self.managementPolicy == 'Managed' || (has(self.name) && self.name != '')",message="name is required when managementPolicy is Unmanaged"
type OVSBridgeConfig struct {
    // managementPolicy determines if the OVS bridge is managed by the
    // controller or provided by the user.
    // +kubebuilder:validation:Required
    ManagementPolicy BridgeManagementPolicy `json:"managementPolicy"`

    // name of the OVS bridge interface.
    // Optional when Managed (defaults to br-hs-<VNI>).
    // Required when Unmanaged.
    // +kubebuilder:validation:Pattern=`^[a-zA-Z][a-zA-Z0-9_-]*$`
    // +kubebuilder:validation:MaxLength=15
    // +optional
    Name string `json:"name,omitempty"`
}
```

#### Dual-Stack Friendly API Shape

**Problem:** Split `IPv4`/`IPv6` fields lock out future dual-stack support as a
breaking change. Both Kubernetes (e.g. `ClusterIPs`, `PodCIDRs`) and OpenShift
use ordered `[]string` slices with `MaxItems=2` and CEL family-differs
validation for dual-stack fields.

**Affected fields:**

**`LocalCIDRConfig` -- breaking change.** Replace the separate `IPv4`/`IPv6`
fields with an ordered `localCIDRs` slice on `HostSession`:

```go
// After
type HostSession struct {
    // ...

    // localCIDRs is the list of CIDRs for the veth pair connecting to the
    // default namespace. The router side uses the first IP of each CIDR.
    // At least one CIDR is required. At most two are allowed (one IPv4,
    // one IPv6). The first element is the primary address family.
    // +kubebuilder:validation:MinItems=1
    // +kubebuilder:validation:MaxItems=2
    // +kubebuilder:validation:XValidation:rule="self.all(x, isCIDR(x)) && (size(self) == 2 ? cidr(self[0]).ip().family() != cidr(self[1]).ip().family() : true)",message="localCIDRs must contain valid CIDRs and at most one of each family"
    // +kubebuilder:validation:XValidation:rule="self == oldSelf",message="localCIDRs cannot be changed"
    // +listType=atomic
    // +required
    LocalCIDRs []string `json:"localCIDRs"`
}
```

**`VTEPCIDR` -- no change.** Single-family in practice; can add `VTEPCIDRs`
alongside later if needed.

**`RouterIDCIDR` -- no change.** BGP router IDs are IPv4-only by definition
(RFC 4271).

**`RoutingDomain.GatewayIPs` -- add CEL family validation.** Already a
`[]string` with `MaxItems=2` but lacks family-differs enforcement:

```go
// +kubebuilder:validation:XValidation:rule="self.all(x, isCIDR(x)) && (size(self) == 2 ? cidr(self[0]).ip().family() != cidr(self[1]).ip().family() : true)",message="gatewayIPs must contain valid CIDRs and at most one of each family"
// +listType=atomic
```

#### Plaintext `Password` on Neighbor

**Problem:** `Neighbor.Password` stores the BGP password as plaintext in etcd.

**Proposal:** Remove both `Password` and `PasswordSecret`. Replace with a
`password` field using a custom `SecretKeyRef` struct:

```go
type Neighbor struct {
    // ...

    // password references a key in a Kubernetes Secret containing the
    // BGP session password.
    // +optional
    Password *SecretKeyRef `json:"password,omitempty"`
}

type SecretKeyRef struct {
    // name is the name of the Secret in the same namespace.
    // +kubebuilder:validation:Required
    // +kubebuilder:validation:MinLength=1
    Name string `json:"name"`

    // key is the key within the Secret's data to select.
    // +kubebuilder:default="password"
    // +kubebuilder:validation:MinLength=1
    // +optional
    Key string `json:"key,omitempty"`
}
```


### Important Fixes

These are non-breaking changes that should be addressed.

#### Printer Columns

**Problem:** `kubectl get` only shows NAME and AGE for all CRDs.

**Proposal:** Add `+kubebuilder:printcolumn` markers:

| CRD | Columns |
|-----|---------|
| Underlay | ASN, RouterID CIDR, VTEP CIDR, Age |
| L2VNI | VNI, L3VNI, Gateway IPs, Age |
| L3VNI | VNI, VRF, Age |
| L3Passthrough | ASN, HostASN, Age |

### Quality and Correctness

#### Move Format Validation from Webhooks to Schema

**Problem:** IP, CIDR, and Route Target fields lack schema-level validation;
checks exist only in webhooks and are not discoverable via `kubectl explain`
or OpenAPI.

**Proposal:** Add CEL / `+kubebuilder:validation` rules and remove the
corresponding webhook checks:

| Field | Type | Current webhook | Proposed schema validation |
|-------|------|-----------------|---------------------------|
| `UnderlaySpec.RouterIDCIDR` | IPv4 CIDR | `validate_underlay.go` | CEL: valid CIDR format |
| `EVPNConfig.VTEPCIDR` | IPv4 CIDR | `validate_underlay.go` | CEL: valid CIDR format |
| `Neighbor.Address` | IP address | Not validated | CEL: valid IPv4 or IPv6 |
| `RoutingDomain.GatewayIPs` | IP/CIDR list | `validate_vni.go` | CEL: valid CIDR, max 1 IPv4 + 1 IPv6 |
| `HostSession.LocalCIDRs` | IP/CIDR list | `validate_hostsession.go` | CEL: valid CIDR, max 1 IPv4 + 1 IPv6 |
| `L3VNISpec.ExportRTs` | Route Target | `validate_vni.go` | Pattern: `^\d+:\d+$` |
| `L3VNISpec.ImportRTs` | Route Target | `validate_vni.go` | Pattern: `^\d+:\d+$` |

Cross-resource validations (duplicate VNI, subnet overlaps, singleton-per-node)
must remain in webhooks, even if it's not perfect is better than not having it
also controllers will check it and report it to future node statuses.


#### Immutability Rules

**Problem:** Several fields that are dangerous to change at runtime lack
`self == oldSelf` CEL enforcement. `L2GatewayIPs` and `LocalCIDR` already
have it, but the following do not.

**Proposal:** Add CEL immutability rules:

```go
// On L2VNISpec.VNI and L3VNISpec.VNI:
// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="VNI cannot be changed"

// On UnderlaySpec.ASN:
// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="ASN cannot be changed"

// On UnderlaySpec.RouterIDCIDR (changing router ID tears down all BGP sessions):
// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="routerIDCIDR cannot be changed"

// On EVPNConfig.VTEPCIDR and EVPNConfig.VTEPInterface (changing VTEP breaks all VXLAN tunnels):
// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="EVPN configuration cannot be changed"

// On L3VNISpec.VRF (renaming tears down the VRF and orphans L2VNI references):
// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="VRF name cannot be changed"

// On L2VNISpec.VXLanPort and L3VNISpec.VXLanPort (changing port breaks existing tunnels):
// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="vxlanPort cannot be changed"

// On HostUplink.Type (switching bridge type is destructive):
// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="host uplink type cannot be changed"

// On LinuxBridgeConfig.Name and OVSBridgeConfig.Name (changing name orphans the old bridge):
// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="bridge name cannot be changed"
```

#### VNI Range Validation

**Problem:** `L2VNISpec.VNI` and `L3VNISpec.VNI` have `Minimum=0,
Maximum=4294967295`. VNI 0 is reserved and VXLAN VNIs are 24-bit (RFC 7348).

**Proposal:** Change to `Minimum=1, Maximum=16777215`.

#### VXLanPort Range Validation

**Problem:** `VXLanPort` on both `L2VNISpec` and `L3VNISpec` has no range
validation. Valid UDP ports are 1-65535.

**Proposal:** Add `Minimum=1, Maximum=65535`.

#### Neighbor.Port Maximum

**Problem:** `Neighbor.Port` has `Maximum=16384` and `Minimum=0`. The TCP
port ceiling is 65535, and port 0 is invalid.

**Proposal:** Change to `Minimum=1, Maximum=65535`.

#### BGP Timer Validation

**Problem:** `HoldTime` and `KeepaliveTime` on `Neighbor` have no range or
whole-seconds validation (unlike `ConnectTime` which does). Per RFC 4271,
hold time must be 0 or >= 3s, and keepalive must be less than hold time.

**Proposal:** Add CEL rules:

```go
// On Neighbor.HoldTime:
// +kubebuilder:validation:XValidation:rule="duration(self).getSeconds() == 0 || duration(self).getSeconds() >= 3",message="holdTime must be 0 or at least 3s (RFC 4271)"

// On Neighbor (cross-field):
// +kubebuilder:validation:XValidation:rule="!has(self.holdTime) || !has(self.keepaliveTime) || duration(self.holdTime).getSeconds() == 0 || duration(self.keepaliveTime).getSeconds() < duration(self.holdTime).getSeconds()",message="keepaliveTime must be less than holdTime"
```

#### HostUplink Union Markers

**Problem:** `HostUplink` (currently `HostMaster`) is a discriminated union
but lacks `+union` and `+unionDiscriminator` markers. The CEL rule enforces
correctness, but the union markers are the canonical way to express this
pattern and enable tooling (`kubectl explain`, OpenAPI discriminator).

**Proposal:** Add `+union` to the struct and `+unionDiscriminator` to `Type`.

#### Short Names and Categories

**Problem:** No short names or categories; CLI usage is verbose.

**Proposal:** Add `+kubebuilder:resource` with short names and a shared
`openperouter` category (`kubectl get openperouter`):

| CRD | Short Name | Categories |
|-----|-----------|------------|
| Underlay | `ul` | `openperouter` |
| L2VNI | `l2` | `openperouter` |
| L3VNI | `l3` | `openperouter` |
| L3Passthrough | `l3pt` | `openperouter` |

#### Remove Duplicate Webhook/Schema Enforcement

**Problem:** Several validations are duplicated in both CEL/schema and
webhooks:

| Validation | Webhook | Schema/CEL |
|------------|---------|------------|
| `L2GatewayIPs` immutability | `l2vni_webhook.go:93-95` | CEL `self == oldSelf` on `l2vni_types.go:65` |
| `LocalCIDR` immutability (L3VNI) | `l3vni_webhook.go:92-94` | CEL `self == oldSelf` on `hostsession.go:27` |
| `HostSession` ASN != HostASN | `validate_hostsession.go:51-52` | CEL on `l3vni_types.go:24` |
| VTEPCIDR XOR VTEPInterface | `validate_underlay.go:49-55` | CEL on `underlay_types.go:56` |

**Proposal:** Remove the webhook-level checks above. CEL runs at admission
before the webhook, so they are redundant. Keep webhooks only for
cross-resource validations.

## References

- [CNV-83550](https://issues.redhat.com/browse/CNV-83550) -- Jira tracking ticket
- [PR #313](https://github.com/openperouter/openperouter/pull/313) -- kube-api-linter fixes (out of scope)
- [PR #329](https://github.com/openperouter/openperouter/pull/329) -- Controller-provisioned underlay interfaces enhancement
- [router-resiliency.md](router-resiliency.md) -- Persistent named netns (motivates underlay interface redesign)
- [status-crd.md](status-crd.md) -- Status reporting (separate enhancement)
