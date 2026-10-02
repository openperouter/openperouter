# Neighbor Update Source

## Summary

This enhancement adds an optional `updateSource` field to a BGP `Neighbor`.
When set to the special keyword `loopback`, OpenPERouter derives the BGP
`update-source` for that neighbor from the node's loopback tunnel endpoint IP
address, selecting the IPv4 or IPv6 address that matches the neighbor's own
address family.

FRR's `update-source` is incompatible with interface (unnumbered) peering
(`neighbor PEER interface`): FRR does not error on an `update-source` configured
for a neighbor identified by interface, it silently discards it. Rather than let
that silent no-op reach the dataplane, the `updateSource` field is treated as
**incompatible with interface neighbors** and is rejected whenever a neighbor
also sets `interface`.

This enhancement also changes the default behavior for SRv6 setups so that BGP sessions no
longer peer from the loopback interface automatically. This aligns SRv6 with
the EVPN behavior, where tunnels are sourced from the loopback IPs but BGP
peering happens from the directly connected interface.

## Motivation

OpenPERouter previously tied the BGP `update-source` to the underlay dataplane
model. For SRv6 underlays, any neighbor carrying a VPN address family
(`ipv4vpn` or `ipv6vpn`) automatically had its `update-source` set to the SRv6
segment-routing source address, i.e. the node's loopback IP. EVPN neighbors, on
the other hand, kept the directly connected interface as the session source
while sourcing the VXLAN tunnels from the loopback.

This difference was a deliberate but inconsistent choice. It coupled the BGP
session source to the encapsulation technology and left users with no way to
influence the behavior. In particular:

- SRv6 and EVPN behaved differently for no reason that is visible to the user.
- There was no API surface to request loopback-sourced peering for a neighbor
  that needs it, nor to opt out of it when it was applied automatically.
- The automatic coupling made multihop and route-reflector topologies, where
  peering from the loopback is a common requirement, awkward to express.

Making the session source explicit and consistent across dataplanes gives users
a single, predictable knob and removes the hidden dependency on the underlay
type.

### Goals

- Allow a neighbor to explicitly request that its BGP `update-source` be
  derived from the node's loopback.
- Derive the correct update source automatically from the neighbor's address
  family: the matching-family tunnel endpoint IP for address and listen-range
  neighbors.
- Make the default behavior consistent across SRv6 and EVPN: no neighbor peers
  from the loopback unless explicitly configured.
- Keep the SRv6 segment-routing encapsulation source address unchanged
  (still derived from the loopback).
- Preserve the existing behavior for neighbors that do not set the field,
  apart from the intentional SRv6 default change described above.

### Non-Goals

- Supporting arbitrary update-source values such as a named interface or an
  explicit IP address. Only the `loopback` keyword is introduced.
- Supporting `loopback` for interface neighbors. The combination is rejected
  (see below).
- Changing how the SRv6 locator or segment-routing `source-address` is
  computed.
- Changing VXLAN tunnel sourcing for EVPN.
- Changing route targets, VNIs, VRFs, or routing-domain configuration.

## Proposal

Add the following optional field to the `Neighbor` type, together with a new
`UpdateSource` string type that is validated to only accept the `loopback`
keyword:

```go
type Neighbor struct {
	// ... existing fields

	// updateSource explicitly specifies the BGP update-source for this neighbor. It currently only
	// supports the special keyword `loopback`, which instructs the OpenPERouter to derive the update
	// source from the IPv4 or IPv6 tunnel endpoint IP address matching the neighbor's address family.
	// It is only valid for neighbors identified by `address` or `listenRange` and must not be set
	// together with `interface`.
	// +optional
	UpdateSource *UpdateSource `json:"updateSource,omitempty"`
}

// updateSource explicitly specifies the BGP update-source for this neighbor. It currently only
// supports the special keyword `loopback`, which instructs the OpenPERouter to derive the update
// source from the IPv4 or IPv6 tunnel endpoint IP address matching the neighbor's address family.
// +kubebuilder:validation:MaxLength:=8
// +kubebuilder:validation:MinLength:=8
// +kubebuilder:validation:Enum=loopback
type UpdateSource string

const (
	// Loopback instructs the OpenPERouter to derive the update source from the IPv4 or IPv6 tunnel
	// endpoint IP address matching the neighbor's address family.
	Loopback UpdateSource = "loopback"
)
```

The incompatibility with interface neighbors is enforced with a CEL rule on the
`Neighbor` type, alongside the existing address/interface/listenRange rules:

```go
// +kubebuilder:validation:XValidation:rule="!has(self.updateSource) || !has(self.interface)",message="updateSource cannot be set together with Interface for Neighbor"
```

Example:

```yaml
apiVersion: network.openperouter.io/v1alpha1
kind: Underlay
metadata:
  name: underlay
spec:
  # ...
  nics:
    - toswitch
  neighbors:
    - asn: 64512
      address: 192.168.1.2
      updateSource: loopback
```

### Update source resolution

When `updateSource` is omitted, OpenPERouter configures no explicit update
source and the session uses the directly connected address (for EVPN, this is the current
behavior; for SRv6, this is a behavioral change).

When `updateSource` is set to `loopback`, OpenPERouter determines the neighbor's
IP address family from its `address` or `listenRange` and selects the matching
IPv4 or IPv6 CIDR from the node's tunnel endpoint (`tunnelEndpoint.cidrs`). The
IP of that CIDR becomes the update source.

The configuration is rejected via CEL validation, if:
- The value is set to anything other than `loopback`.
- The neighbor also sets `interface` (FRR silently discards `update-source` on
  interface/unnumbered peers).

Additionally, the configuration is rejected via webhook, if:
- No tunnel endpoint is present.
- The neighbor's address family cannot be determined from the `address` or
  `listenRange`.
- The tunnel endpoint has no CIDR for the neighbor's address family.

Because static resources are not applied through the API server, the aforementioned rules will also
be enforced during the resolution mechanism in the FRR conversion layer.

### SRv6 default behavior change

Previously, SRv6 underlays automatically set the BGP `update-source` to the
segment-routing source address for every neighbor carrying a VPN address
family. SRv6 neighbors now default to the directly connected source, exactly like EVPN neighbors,
and must opt in to loopback peering via `updateSource: loopback`.

The SRv6 segment-routing encapsulation `source-address` is unchanged and
continues to be derived from the loopback. Only the BGP session source is
affected by this change.

## Items for Discussion

### Keyword versus explicit value

The field accepts only the `loopback` keyword rather than an arbitrary address
or interface name. This keeps the API small and lets OpenPERouter own the
derivation of the correct per-family source. If future use cases require
pinning the session source to a specific interface or IP address, the type can be
extended or the syntax change on the string can be changed.

### Interface neighbors are out of scope

`updateSource` is rejected for interface neighbors because FRR silently discards
`update-source` on a neighbor configured by interface (`neighbor PEER
interface`, i.e. unnumbered peering). Since the setting would be a silent no-op
in FRR, OpenPERouter rejects it up front rather than accepting configuration that
has no effect on the dataplane.

As a welcome side effect, scoping the field to address and listen-range
neighbors keeps update-source resolution unambiguous: the update source is
always a concrete tunnel endpoint IP of the neighbor's address family, with no
need to introspect host-level interface details in the FRR conversion layer.

### Interaction with SRv6 VPN sessions

Because the SRv6 automatic loopback peering is removed, existing SRv6
deployments that relied on it will change behavior after upgrade: their VPN
sessions will source from the directly connected interface unless
`updateSource: loopback` is added. The migration impact and whether any
defaulting or warning is needed should be confirmed during implementation, but
this impact should be negligible given that we are in an early alpha version.

### CEL vs webhook validation

In order to keep CEL complexity low, the following 2 items are ideally enforce via webhook:
- The neighbor's address family cannot be determined from the `address` or
  `listenRange`.
- The tunnel endpoint has no CIDR for the neighbor's address family.

Whereas the following rule can be easily enforced via CEL:
- No tunnel endpoint is present.

For consistency, however, all 3 rules will be enforced together via webhook.

## Validation and Testing

Implementation should cover:

- API CEL validation that only the `loopback` keyword is accepted.
- API CEL validation that `updateSource` cannot be combined with `interface`.
- API webhook validation for the more complex cases (missing tunnel endpoint,
  undeterminable address family, missing matching-family CIDR).
- Unit tests for update-source resolution across address and listen-range
  neighbors, including the IPv4 and IPv6 tunnel endpoint cases and the failure
  modes above.
- Unit tests for API webhook validation.
- Unit tests and golden FRR configuration for a neighbor with
  `updateSource: loopback`.
- End-to-end tests that verify peering from the loopback for both route
  reflector and SRv6 L3VPN scenarios, alongside the existing directly connected
  peering.
- End-to-end tests for CEL / API webhook validation.

## Compatibility

The change is additive at the API level: existing neighbors do not set
`updateSource` and keep configuring no explicit update source.

The one behavioral change is for SRv6 underlays, which no longer peer VPN
neighbors from the loopback automatically. Deployments that require that
behavior must set `updateSource: loopback` on the affected neighbors. Static
configuration uses the same field and semantics as the Kubernetes API.
