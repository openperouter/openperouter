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

**Problem:** [#332](https://github.com/openperouter/openperouter/pull/332) and
[#346](https://github.com/openperouter/openperouter/pull/346) already stopped the
runtime from forcing a VRF on disconnected L2VNIs, so a VNI without `spec.vrf`
now behaves as a pure east-west L2 overlay. The remaining problem is the API
shape: intent (attached-to-VRF vs pure-L2-overlay) is encoded by the *absence*
of the free-form `VRF *string`, `VRFName()` still falls back to `metadata.name`,
and the L2VNI-to-L3VNI association relies on fragile string matching rather than
an explicit, validated reference.

**Proposal:** Replace `VRF *string` + `L2GatewayIPs` with a `routingDomain`
struct that references the L3VNI CR by `metadata.name`. Omitting
`routingDomain` explicitly means "disconnected overlay". `GatewayIPs` moves
inside `RoutingDomain` since gateway IPs without a routing domain are invalid.
If the referenced L3VNI does not exist, the controller rejects the L2VNI
(`Ready=False, Reason=L3VNINotFound`).

```go
// +kubebuilder:validation:XValidation:rule="!has(oldSelf.routingDomain) || has(self.routingDomain)",message="routingDomain cannot be removed once set"
type L2VNISpec struct {
    // RoutingDomain optionally attaches this L2VNI to a routing domain
    // provided by an L3VNI. When omitted, the L2VNI is a disconnected
    // overlay (east-west L2 only, no VRF, no gateway). Once set it cannot
    // be removed, otherwise the immutable GatewayIPs below could be dropped
    // by deleting the parent struct.
    // +optional
    RoutingDomain *RoutingDomain `json:"routingDomain,omitempty"`

    // ... other fields unchanged
}

type RoutingDomain struct {
    // L3VNI is the name of the L3VNI resource (metadata.name) in the same
    // namespace that provides the routing domain for this L2VNI.
    // The VRF configuration (name, VNI, route targets) is owned entirely
    // by the referenced L3VNI -- the L2VNI does not duplicate it.
    // +required
    L3VNI string `json:"l3vni,omitempty"`

    // GatewayIPs is a list of IP addresses in CIDR notation for the
    // distributed anycast gateway on this L2 segment's bridge (IRB
    // interface). Maximum 2 (one IPv4, one IPv6).
    // +optional
    // +kubebuilder:validation:MaxItems=2
    // +kubebuilder:validation:XValidation:rule="self.all(x, isCIDR(x)) && (size(self) == 2 ? cidr(self[0]).ip().family() != cidr(self[1]).ip().family() : true)",message="gatewayIPs must contain valid CIDRs and at most one of each family"
    // +kubebuilder:validation:XValidation:rule="self == oldSelf",message="gatewayIPs cannot be changed"
    // +listType=atomic
    GatewayIPs []string `json:"gatewayIPs,omitempty"`
}
```

The `gatewayIPs` immutability rule lives on the field, but a field-level
transition rule (`self == oldSelf`) only runs when the field is present in both
the old and new objects -- it never fires when the parent is removed. So a user
could drop the immutable `gatewayIPs` simply by deleting the whole
`routingDomain` struct. To close that gap, the "cannot remove once set" rule is
placed on `L2VNISpec` (the parent), where `has(oldSelf.routingDomain)` and
`has(self.routingDomain)` can detect the removal. The rule still allows
attaching a routing domain to a previously disconnected overlay (unset -> set)
and mutating `l3vni`; it only forbids the destructive set -> unset transition.

**Examples:**

```yaml
# L2VNI with routing domain
spec:
  vni: 100
  routingDomain:
    l3vni: tenant-a       # references L3VNI by metadata.name
    gatewayIPs:
      - 10.100.0.1/24
      - fd10:100::1/64
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
`UnderlaySpec`. Currently one mode (`networkDevice`) is defined; macvlan/ipvlan modes
will be added in a follow-up API enhancement.

```go
type UnderlaySpec struct {
    // ...

    // interfaces is the list of interfaces the router uses for underlay
    // connectivity. Each entry is a discriminated union describing how the
    // interface is obtained.
    // +kubebuilder:validation:MinItems=1
    // +required
    Interfaces []UnderlayInterface `json:"interfaces,omitempty"`
}

// UnderlayInterface defines how the router obtains a single underlay link.
// Exactly one of the sub-structs must match the type field.
// The union is designed to be extended with future modes (e.g. macvlan,
// ipvlan) for controller-provisioned interfaces.
//
// +kubebuilder:validation:XValidation:rule="self.type == 'NetworkDevice' == has(self.networkDevice)",message="networkDevice field mismatch"
// +union
type UnderlayInterface struct {
    // type selects how the router obtains this underlay link.
    // +kubebuilder:validation:Enum=NetworkDevice
    // +required
    // +unionDiscriminator
    Type string `json:"type,omitempty"`

    // networkDevice moves an existing host network device into the router netns.
    // The device can be of any kind (physical NIC, bridge, macvlan, etc.).
    // +optional
    NetworkDevice *NetworkDevice `json:"networkDevice,omitempty"`
}

type NetworkDevice struct {
    // interfaceName is the name of the host network device to move into
    // the router netns.
    // +kubebuilder:validation:Pattern=`^[a-zA-Z][a-zA-Z0-9._-]*$`
    // +kubebuilder:validation:MaxLength=15
    // +required
    InterfaceName string `json:"interfaceName,omitempty"`
}
```

**Example:**

```yaml
interfaces:
  - type: NetworkDevice
    networkDevice:
      interfaceName: eth1
```

#### Rename `EVPN` to `TunnelEndpoint`

**Problem:** `UnderlaySpec.EVPN` (type `EVPNConfig`) holds the tunnel endpoint
source address configuration. This information is not EVPN-specific — it
provides the source addresses for any overlay tunnel technology (EVPN VXLAN
today, SRv6 in the future). The
[SRv6 enhancement](https://github.com/openperouter/openperouter/pull/316)
introduces SRv6 L3VPN support and needs the same tunnel endpoint fields for
IPv6 source addresses. Keeping the field named `EVPN` would force SRv6 to
reference an `EVPN` struct, which is misleading.

Additionally, `VTEPCIDR` has no schema-level format or family validation.
The only CIDR check is in the Go webhook (`validate_underlay.go`). VTEP is
also an EVPN/VXLAN-specific term.

The `VTEPInterface` field has been removed by
[PR #461](https://github.com/openperouter/openperouter/pull/461) since
Multus support was removed as part of the resiliency work, leaving
`VTEPCIDR` as the only field in `EVPNConfig`. This makes `EVPNConfig` a
single-field wrapper struct — the rename to `TunnelEndpointConfig` is the
right time to also replace `VTEPCIDR *string` with `CIDRs []string` for
schema-level validation.

**Proposal:** Rename `EVPNConfig` to `TunnelEndpointConfig` and the field
from `evpn` to `tunnelEndpoint`. Replace `VTEPCIDR *string` with
`CIDRs []string`. Currently only a single IPv4 CIDR is supported.

```go
// TunnelEndpointConfig contains tunnel endpoint configuration for the
// underlay. The tunnel endpoint provides source addresses used by overlay
// tunnels. Replaces the previous EVPNConfig struct.
type TunnelEndpointConfig struct {
    // cidrs is a list of CIDRs to assign IPs to the local tunnel endpoint
    // on each node. A loopback interface will be created with IPs derived
    // from these CIDRs. Currently only a single IPv4 CIDR is supported.
    // +kubebuilder:validation:MaxItems=1
    // +kubebuilder:validation:XValidation:rule="self.all(c, isCIDR(c) && cidr(c).ip().family() == 4)",message="all entries must be valid IPv4 CIDRs"
    // +optional
    CIDRs []string `json:"cidrs,omitempty"`
}
```

**Example:**

```yaml
# Before (EVPNConfig)
spec:
  evpn:
    vtepCIDR: "10.100.0.0/24"

# After (TunnelEndpointConfig)
spec:
  tunnelEndpoint:
    cidrs:
      - "10.100.0.0/24"
```

#### Rename `RouterIDCIDR` to `RouterIDPool`

**Problem:** `UnderlaySpec.RouterIDCIDR` is an IPv4 CIDR from which the
controller allocates a distinct BGP router-id per node — one host address per
node index, via `ipam.RouterID()`. The `CIDR` suffix names the storage format,
not the field's purpose, and once `VTEPCIDR` becomes
`TunnelEndpointConfig.CIDRs` it would be the only `*CIDR`-suffixed field left on
`UnderlaySpec`. The name also hides the allocation semantics: the operator
supplies a range and the controller hands out one address from it to each node.

**Proposal:** Rename the field from `RouterIDCIDR` to `RouterIDPool`
(Go `RouterIDPool`, JSON `routerIDPool`). "Pool" matches the
allocate-one-per-consumer model operators already know from MetalLB address
pools and keeps the field self-describing without leaking the CIDR storage
type. The value stays an IPv4 CIDR — router-ids are IPv4-only by definition
(BGP per RFC 4271, OSPF, IS-IS) — so the type and the CIDR-format validation
are unchanged; only the name changes.

```go
// Before
type UnderlaySpec struct {
    // routeridcidr is the CIDR used to derive the BGP router-id of each node.
    RouterIDCIDR *string `json:"routeridcidr,omitempty"`
}

// After
type UnderlaySpec struct {
    // routerIDPool is the IPv4 CIDR from which a BGP router-id is allocated
    // for each node (one host address per node).
    // +kubebuilder:validation:XValidation:rule="isCIDR(self) && cidr(self).ip().family() == 4",message="routerIDPool must be a valid IPv4 CIDR"
    // +optional
    RouterIDPool *string `json:"routerIDPool,omitempty"`
}
```

This is a breaking change on both the Go field name and the JSON tag, so it is
listed here rather than in the JSON tag casing fix below, which is limited to
serialization-only changes.

#### Remove Non-Inclusive Terminology

**Problem:** Several API types and comments use non-inclusive language:

- `RunOnMaster` / `runOnMaster` on the operator CRD
  (`OpenPERouterSpec`) uses "master" instead of "control-plane" and is
  a boolean, which violates K8s API conventions. Replace with native
  K8s scheduling primitives (nodeSelector, tolerations, affinity)
  following the [MetalLB operator](https://github.com/metallb/metallb-operator)
  pattern.
- Godoc comments on `L2VNISpec.HostMaster` and `L2GatewayIPs` use
  "enslaved to" instead of "attached to".

**Proposal:**

Rename types and fields:

```go
// Operator CRD -- before
RunOnMaster *bool `json:"runOnMaster,omitempty"`

// After — replace boolean with native K8s scheduling primitives,
// following the MetalLB operator pattern. A single set of fields
// applies to all components (router, controller, nodemarker,
// hostbridge). Per-component scheduling can be added later without
// breaking the API.

// nodeSelector constrains all openperouter pods to nodes with
// matching labels.
// +optional
NodeSelector map[string]string `json:"nodeSelector,omitempty"`

// tolerations for all openperouter pods.
// +optional
Tolerations []corev1.Toleration `json:"tolerations,omitempty"`

// affinity scheduling rules for all openperouter pods.
// +optional
Affinity *corev1.Affinity `json:"affinity,omitempty"`
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
| `HostMaster.Type` | `linux-bridge`, `ovs-bridge` | `LinuxBridge`, `OVSBridge` |
| `UnderlayInterface.Type` | _(new)_ | `NetworkDevice` |

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

Replace `AutoCreate bool` with a `lifecycle` enum (`Managed`/`Unmanaged`):


```go
// BridgeLifecycle determines how the bridge is provisioned.
// +kubebuilder:validation:Enum=Managed;Unmanaged
type BridgeLifecycle string

const (
    // BridgeLifecycleManaged means the controller creates and owns
    // the bridge, and deletes it when the L2VNI is removed. If Name is
    // omitted, the bridge is auto-named br-hs-<VNI>. If Name is
    // provided, the controller creates the bridge with that name.
    BridgeLifecycleManaged BridgeLifecycle = "Managed"

    // BridgeLifecycleUnmanaged means the user provides a
    // pre-existing bridge via the Name field. The controller does not
    // create or delete it; only veth ports are attached/detached.
    BridgeLifecycleUnmanaged BridgeLifecycle = "Unmanaged"
)

// LinuxBridgeConfig contains configuration for Linux bridge type.
// +kubebuilder:validation:XValidation:rule="self.lifecycle == 'Managed' || (has(self.name) && self.name != '')",message="name is required when lifecycle is Unmanaged"
type LinuxBridgeConfig struct {
    // lifecycle determines if the bridge is managed by the
    // controller or provided by the user.
    // +required
    Lifecycle BridgeLifecycle `json:"lifecycle,omitempty"`

    // name of the Linux bridge interface.
    // Optional when Managed (defaults to br-hs-<VNI>).
    // Required when Unmanaged.
    // +kubebuilder:validation:Pattern=`^[a-zA-Z][a-zA-Z0-9_-]*$`
    // +kubebuilder:validation:MaxLength=15
    // +optional
    Name *string `json:"name,omitempty"`
}

// OVSBridgeConfig contains configuration for OVS bridge type.
// +kubebuilder:validation:XValidation:rule="self.lifecycle == 'Managed' || (has(self.name) && self.name != '')",message="name is required when lifecycle is Unmanaged"
type OVSBridgeConfig struct {
    // lifecycle determines if the OVS bridge is managed by the
    // controller or provided by the user.
    // +required
    Lifecycle BridgeLifecycle `json:"lifecycle,omitempty"`

    // name of the OVS bridge interface.
    // Optional when Managed (defaults to br-hs-<VNI>).
    // Required when Unmanaged.
    // +kubebuilder:validation:Pattern=`^[a-zA-Z][a-zA-Z0-9_-]*$`
    // +kubebuilder:validation:MaxLength=15
    // +optional
    Name *string `json:"name,omitempty"`
}
```

#### Consolidate Split IP Family Fields

**Problem:** `LocalCIDRConfig` uses separate `IPv4 *string` / `IPv6 *string`
fields. The current implementation already supports dual-stack, but using two
distinct fields is unnecessarily rigid — it requires a struct with a CEL rule
to enforce "at least one set", and cannot be extended to multiple CIDRs of
the same family without adding more fields. Both Kubernetes
(e.g. `ClusterIPs`, `PodCIDRs`) and OpenShift use ordered `[]string` slices
for this pattern.

**Affected fields:**

**`LocalCIDRConfig` -- breaking change.** Replace the `LocalCIDRConfig`
struct with an ordered `localCIDRs` slice directly on `HostSession`.
This collapses two fields into one and follows the K8s convention:

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
    LocalCIDRs []string `json:"localCIDRs,omitempty"`
}
```

**`VTEPCIDR` -- covered by `TunnelEndpoint` redesign above.** After
[PR #461](https://github.com/openperouter/openperouter/pull/461) removed
`VTEPInterface`, `VTEPCIDR` is the only field in `EVPNConfig` (optional
`*string`). The field becomes `TunnelEndpointConfig.CIDRs []string`
with `MaxItems=1` and IPv4-only CEL enforcement.

**`RouterIDCIDR` -- renamed, not split.** Router IDs are IPv4-only by
definition across routing protocols (BGP per RFC 4271, OSPF, IS-IS), so there
is no IP-family split to make here. The field is instead renamed to
`RouterIDPool` by the `RouterIDCIDR` → `RouterIDPool` rename above.

**`RoutingDomain.GatewayIPs` -- covered by the Routing Domain / VRF Semantics
on L2VNI redesign above.** The field is a `[]string` with `MaxItems=2`, and its
definition already carries the family-differs CEL rule and `+listType=atomic`:

```go
// +kubebuilder:validation:XValidation:rule="self.all(x, isCIDR(x)) && (size(self) == 2 ? cidr(self[0]).ip().family() != cidr(self[1]).ip().family() : true)",message="gatewayIPs must contain valid CIDRs and at most one of each family"
// +listType=atomic
```

#### BGP Password Handling on Neighbor

**Problem:** `Neighbor` has two fields for BGP session authentication:

- `Password *string` — stores the password as **plaintext in the CR**,
  which means it is stored unencrypted in etcd and visible to anyone
  with read access to the resource. This is the only field actually
  wired into the FRR configuration.
- `PasswordSecret *string` — intended to reference a
  `kubernetes.io/basic-auth` Secret by name, but **never implemented**.
  No controller code reads it, no Secret lookup exists, no RBAC grants
  access to Secrets, and no tests cover it. Setting this field has no
  effect.

**Proposal:** Remove both `Password` and the unimplemented
`PasswordSecret`. Replace with a `passwordSecretRef` field using a
`SecretKeyRef` struct that references a Secret by name and key. The
controller must be updated to fetch the Secret and inject the password
into the FRR configuration:

```go
type Neighbor struct {
    // ...

    // passwordSecretRef references a key in a Kubernetes Secret
    // containing the BGP session password.
    // +optional
    PasswordSecretRef *SecretKeyRef `json:"passwordSecretRef,omitempty"`
}

type SecretKeyRef struct {
    // name is the name of the Secret in the same namespace.
    // +required
    // +kubebuilder:validation:MinLength=1
    Name string `json:"name,omitempty"`

    // key is the key within the Secret's data to select.
    // The controller defaults this to "password" when unset.
    // +kubebuilder:validation:MinLength=1
    // +optional
    Key *string `json:"key,omitempty"`
}
```


### Important Fixes

These are non-breaking changes that should be addressed.

#### Normalize JSON Tag Casing

**Problem:** Several JSON tags on existing API types are flat-lowercase
instead of `lowerCamelCase`, which violates the
[Kubernetes API conventions](https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#naming-conventions)
and is inconsistent with neighbouring fields on the same types
(e.g. `nodeSelector`, `linuxBridge`, `holdTimeSeconds`). The
kube-api-linter on `main` does not flag these because some are silenced
with `//nolint:kubeapilinter` and others slip past its acronym rules.

**Proposal:** Rename the offending JSON tags. Go field names stay the
same; only the serialized name changes. Items already covered by other
redesigns in this enhancement (e.g.
`L2GatewayIPs` → `GatewayIPs`, `LocalCIDR` → `LocalCIDRs`
) inherit the corrected tag and are not
listed again here.

| Type / Field | Current tag | Proposed tag |
|--------------|-------------|--------------|
| `L2VNISpec.HostMaster` | `hostmaster` | `hostMaster` |
| `HostSession.HostASN` | `hostasn` | `hostASN` |
| `HostSession.HostType` | `hosttype` | `hostType` |
| `L2VNISpec.VXLanPort` | `vxlanport` | `vxlanPort` |
| `L3VNISpec.VXLanPort` | `vxlanport` | `vxlanPort` |
| `L3PassthroughSpec.HostSession` | `hostsession` | `hostSession` |

This is a breaking serialization change but is grouped with Important
Fixes because the Go API surface (field names, types) is unchanged.

#### Print Columns

**Problem:** `kubectl get` only shows NAME and AGE for all CRDs.

**Proposal:** Add `+kubebuilder:printcolumn` markers:

| CRD | Columns |
|-----|---------|
| Underlay | ASN, Router ID Pool, VTEP CIDR, Age |
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
| `UnderlaySpec.RouterIDPool` | IPv4 CIDR | `validate_underlay.go` | CEL: valid IPv4 CIDR format |
| `TunnelEndpointConfig.CIDRs` | IPv4 CIDR | `validate_underlay.go` | CEL: valid IPv4 CIDR (covered by `TunnelEndpoint` redesign) |
| `Neighbor.Address` | IP address | Not validated | CEL: valid IPv4 or IPv6 |
| `RoutingDomain.GatewayIPs` | IP/CIDR list | `validate_vni.go` | CEL: valid CIDR, max 1 IPv4 + 1 IPv6 |
| `HostSession.LocalCIDRs` | IP/CIDR list | `validate_hostsession.go` | CEL: valid CIDR, max 1 IPv4 + 1 IPv6 |
| `L3VNISpec.ExportRTs` / `ImportRTs` | Route Target | `validate_vni.go` | `RouteTarget` named type (see below) |

Introduce a `RouteTarget` named type to replace the plain `[]string` on
`L3VNISpec.ExportRTs` and `ImportRTs`. The current webhook validation
(`validateRouteTarget()`) already accepts both `ASN:NN` and `IP:NN` formats
with range checks; the named type moves this to schema-level CEL. The SRv6
enhancement ([PR #316](https://github.com/openperouter/openperouter/pull/316))
reuses the same type for `L3VPN`.

```go
// RouteTarget defines a BGP Extended Community for route filtering.
// Supports two formats per RFC 4360:
//   - ASN:NN   (e.g. "64514:100")        where ASN is a 2- or 4-byte ASN (<= 4294967295)
//   - IP:NN    (e.g. "192.168.1.1:100")  where IP is a valid IPv4 address
// +kubebuilder:validation:MaxLength=26
// +kubebuilder:validation:XValidation:rule=`self.split(':').size() == 2 && ((isIP(self.split(':')[0]) && ip(self.split(':')[0]).family() == 4) || (self.split(':')[0].matches('^[0-9]{1,10}$') && uint(self.split(':')[0]) <= 4294967295u)) && self.split(':')[1].matches('^[0-9]{1,10}$') && uint(self.split(':')[1]) <= 4294967295u`,message="routeTarget must be in ASN:NN or IPv4:NN format, where the prefix is a valid IPv4 address or an ASN <= 4294967295"
type RouteTarget string
```

The prefix (the part before the `:`) is validated to be either a valid IPv4
address or a valid ASN, mirroring `validateRouteTarget()` in `validate_vni.go`:
the IPv4 form is checked with the CEL IP library (`isIP` / `ip().family() == 4`,
available in CRD validation since Kubernetes 1.31) so octet ranges are enforced
instead of relying on a loose `[0-9]{1,3}` regex, and the ASN form is bounded to a
4-byte ASN (`<= 4294967295`). The per-format member-number ranges still enforced
by the webhook (IPv4 / 4-byte ASN → MN `<= 65535`, 2-byte ASN → MN `<= 4294967295`)
are not yet encoded in CEL and can be folded into the same rule if full parity is
required.

Cross-resource validations (duplicate VNI, subnet overlaps, singleton-per-node)
must remain in webhooks, even if it's not perfect is better than not having it
also controllers will check it and report it to future node statuses.


#### Immutability Rules

**Problem:** Several fields that are dangerous to change at runtime lack
`self == oldSelf` CEL enforcement. `L2GatewayIPs` and `LocalCIDR` already
have it, but the following do not.

Immutability should only be enforced where there is a **hard technical
constraint** — changes that cause data plane breakage the controller
cannot gracefully handle (e.g. orphaned tunnels, broken bridge
attachments). Fields where the controller can handle reconfiguration
(BGP sessions drop and re-converge, neighbors change) should remain
mutable to avoid forcing users to delete and recreate resources.

**Proposal:** Add CEL immutability rules:

```go
// On L2VNISpec.VNI and L3VNISpec.VNI:
// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="VNI cannot be changed"

// On TunnelEndpointConfig.CIDRs and TunnelEndpointConfig.Interface (changing tunnel endpoint breaks all tunnels):
// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="tunnel endpoint configuration cannot be changed"

// On L3VNISpec.VRF (renaming tears down the VRF and orphans L2VNI references):
// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="VRF name cannot be changed"

// On L2VNISpec.VXLanPort and L3VNISpec.VXLanPort (changing port breaks existing tunnels):
// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="vxlanPort cannot be changed"

// On HostMaster.Type (switching bridge type is destructive):
// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="host master type cannot be changed"

// On LinuxBridgeConfig.Name and OVSBridgeConfig.Name (changing name orphans the old bridge):
// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="bridge name cannot be changed"
```

The following fields are **intentionally mutable** — the controller
handles reconfiguration gracefully (BGP sessions drop and re-converge):

- `UnderlaySpec.ASN` — changing local ASN causes BGP sessions to reset,
  but FRR re-establishes them with the new ASN.
- `UnderlaySpec.RouterIDPool` — same as ASN; sessions drop and
  re-converge.
- `Neighbors` (address, ASN, timers, etc.) — the controller reconciles
  neighbor changes without requiring resource deletion.

`NodeSelector` on every CRD remains mutable. Operators need to be able
to expand or shrink the set of nodes the configuration applies to
(e.g. rolling out to additional nodes, draining a node) without having
to delete and recreate the resource.

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

#### Neighbors MaxItems

**Problem:** `UnderlaySpec.Neighbors` has no `MaxItems` bound. Unbounded
lists are a Kubernetes API anti-pattern — they cause etcd bloat and slow
admission validation.

**Proposal:** Add `+kubebuilder:validation:MaxItems=100`. This aligns with
the SRv6 enhancement ([PR #316](https://github.com/openperouter/openperouter/pull/316))
which adds the same bound.

#### BGP Timer Validation

**Problem:** `HoldTimeSeconds` and `KeepaliveTimeSeconds` on `Neighbor`
have no range or relational validation (unlike `ConnectTimeSeconds` which
does). Per RFC 4271, hold time must be 0 or >= 3s, and keepalive must be
less than hold time. Additionally, setting one without the other creates
undefined FRR behavior — they must be both set or both unset.

**Proposal:** Add CEL rules:

```go
// On Neighbor (cross-field):
// +kubebuilder:validation:XValidation:rule="has(self.holdTimeSeconds) == has(self.keepaliveTimeSeconds)",message="holdTimeSeconds and keepaliveTimeSeconds must be both set or both unset"
// +kubebuilder:validation:XValidation:rule="!has(self.holdTimeSeconds) || self.holdTimeSeconds == 0 || self.holdTimeSeconds >= 3",message="holdTimeSeconds must be 0 or at least 3 (RFC 4271)"
// +kubebuilder:validation:XValidation:rule="!has(self.holdTimeSeconds) || !has(self.keepaliveTimeSeconds) || self.holdTimeSeconds == 0 || self.keepaliveTimeSeconds < self.holdTimeSeconds",message="keepaliveTimeSeconds must be less than holdTimeSeconds"
```

#### HostMaster Union Markers

**Problem:** `HostMaster` is a discriminated union
but lacks `+union` and `+unionDiscriminator` markers. The CEL rule enforces
correctness, but the union markers are the canonical way to express this
pattern and enable tooling (`kubectl explain`, OpenAPI discriminator).

**Proposal:** Add `+union` to the struct and `+unionDiscriminator` to `Type`.


#### Remove Duplicate Webhook/Schema Enforcement

**Problem:** Several validations are duplicated in both CEL/schema and
webhooks:

| Validation | Webhook | Schema/CEL |
|------------|---------|------------|
| `L2GatewayIPs` immutability | `l2vni_webhook.go:93-95` | CEL `self == oldSelf` on `l2vni_types.go:65` |
| `LocalCIDR` immutability (L3VNI) | `l3vni_webhook.go:92-94` | CEL `self == oldSelf` on `hostsession.go:27` |
| `HostSession` ASN != HostASN | `validate_hostsession.go:51-52` | CEL on `l3vni_types.go:24` |
| CIDRs XOR Interface | `validate_underlay.go:49-55` | CEL on `TunnelEndpointConfig` |

**Proposal:** Remove the webhook-level checks above. CEL runs at admission
before the webhook, so they are redundant. Keep webhooks only for
cross-resource validations.

## References

- [CNV-83550](https://issues.redhat.com/browse/CNV-83550) -- Jira tracking ticket
- [PR #313](https://github.com/openperouter/openperouter/pull/313) -- kube-api-linter fixes (out of scope)
- [PR #316](https://github.com/openperouter/openperouter/pull/316) -- SRv6 enhancement (introduces `TunnelEndpoint` concept)
- [PR #329](https://github.com/openperouter/openperouter/pull/329) -- Controller-provisioned underlay interfaces enhancement
- [PR #461](https://github.com/openperouter/openperouter/pull/461) -- Remove VTEPInterface from EVPNConfig ([#458](https://github.com/openperouter/openperouter/issues/458))
- [router-resiliency.md](router-resiliency.md) -- Persistent named netns (motivates underlay interface redesign)
- [status-crd.md](status-crd.md) -- Status reporting (separate enhancement)
