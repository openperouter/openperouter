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

The configuration is rejected via CEL/API validation, if:
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

The previous assumption that the BGP session update-source and the segment-routing locator's
source-address needed to be the same was erroneous and too strict.

The BGP session's update-source is used for the BGP peering only and is independent of the VXLAN
and SRv6 tunnel source addresses.

The segment-routing locator's source-address is the equivalent of the VXLAN's tunnel source, with
one important difference: in the case of VXLAN, the tunnel source must be advertised, as it is the
tunnel destination for other peers (the tunnels are symmetric). In SRv6, the tunnel source
technically does not need to be advertised, as the tunnel destination is the SRv6 function
(the tunnels are asymmetric). However, for better traceability and to avoid issues with
reverse-path filtering, the SRv6 tunnel source shall always be announced.

The segment-routing locator's source-address is set to the IPv6 endpoint which is derived from
`tunnelEndpoint.cidrs`. If no such endpoint exists, SRv6 already refuses the configuration.
Currently, the IPv4 and IPv6 endpoints derived from `tunnelEndpoint.cidrs` are consistently
advertised to the network:
- In the case of EVPN, they are both advertised via the BGP network statement for the endpoint.
- In the case of SRv6, the IPv6 address is announced via IS-IS which is enabled and configured to
  advertise the IPv6 addresses of the loopback.

Therefore, the aforementioned requirement is already fulfilled.

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

### IS-IS advertisement of the update-source

For the BGP session to be established, the peer must be able to reach the
update-source address, so it has to be advertised into the IS-IS underlay.
The enhancement guarantees this implicitly:
- `updateSource: loopback` is safe because the passive loopback advertisement
  is independent of `advertisePassiveOnly` (unless admins configure an IS-IS
  interface override).
- The physical interface is safe because it is advertised while
  `advertisePassiveOnly` is `false`, which is the default.

The question is whether the combination of `advertisePassiveOnly` without
`updateSource: loopback` should be rejected explicitly. This enhancement
establishes that this is not necessary, as admins can advertise
the source address of the physical interface by other means. See
[Compatibility/IS-IS Advertisement](#is-is-advertisement) for further
details.

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

### IS-IS Advertisement

The BGP session update-source is independent of the SRv6 encapsulation source
address and of the locator prefix, and it is worth keeping the three concepts
separate when reasoning about what IS-IS actually advertises:

- **Locator prefix:** SRv6 functions are allocated from the locator prefix.
  The SRv6 peer sends its IPv6 SRv6 packets _to_ an IPv6 address within this
  prefix.
- **Encapsulation source-address:** The SRv6 encapsulation source is the IPv6
  source address of the IPv6 SRv6 packets. It is hardcoded to use the loopback
  IPv6 address (the explicit `encapsulation source-address` is temporarily
  disabled until [FRRouting/frr#20716](https://github.com/FRRouting/frr/pull/20716)
  lands in our image; meanwhile, the source address defaults to the loopback
  even without it).
- **BGP update-source:** The BGP update-source is what this enhancement
  controls. It is the source address of the TCP session between the 2 BGP
  speakers and is independent of the two above and can be either the
  address of the loopback or the address of the physical interface.

What IS-IS advertises, and when:

- **Locator prefix:** always advertised via the segment-routing locator
  configuration (`router isis ... segment-routing srv6 locator ...`),
  independent of interface advertisement and of the `advertisePassiveOnly`
  feature.
- **Loopback IPv6 address:** the loopback is configured as a passive IS-IS
  interface, and passive interfaces are always advertised. The loopback address
  is therefore advertised even when `advertisePassiveOnly` is set, unless the
  admin explicitly overrides the passive setting on the loopback via
  an interface override.
- **Physical interface IPv6 address:** advertised by default, because
  `advertisePassiveOnly` is `false` by default and active interfaces are
  advertised too. When `advertisePassiveOnly` is set, only passive interfaces
  are advertised and the announcement of the physical interface's address is
  suppressed.

Therefore, even with the change of the BGP session update-source from the
loopback to the physical interface, we can be sure that the update-source
address will be advertised by default via IS-IS.

On the other hand, should admins choose to set `advertisePassiveOnly`, or should
they override the IS-IS default interface configuration, they will be responsible
for either setting `updateSource: loopback` or advertising the physical interface'speakers
subnet by other means, e.g. from the ToR switch.
