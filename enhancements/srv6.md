## Summary

This enhancement proposes an extension of the OpenPERouter API to add support
for SRv6 L3VPNs. It extends the `Underlay` CRD with ISIS and SRv6 configuration
fields, adds an Address Family selector to the BGP Neighbor, and adds a new
`L3VPN` Custom Resource Definition.

## Motivation

Segment Routing instantiated on the IPv6 data plane (SRv6) is an implementation
of source routing over an IPv6 data plane
([RFC8402](https://datatracker.ietf.org/doc/html/rfc8402)) and can be used to
implement [both EVPNs and L3VPNs](https://www.segment-routing.net/tutorials/SRv6-uSID/).
SRv6 has significant industry support and its adoption is quickly growing.

Contrary to classic datacenter EVPN, L3VPNs over SRv6 allow operators to tunnel
VRF traffic over IPv6 infrastructure only, without the need for VXLAN tunnels.
Instead, the VPN is entirely encapsulated in the outer IPv6 header. SRv6 L3VPN
is also simpler than EVPN and easier to understand. Support for the technology
in FRR is constantly improving, and it is a feature that operators are looking for
in OpenPERouter.

L3VPNs are implemented using `End.DT46` endpoints which provide decapsulation
and specific IP table lookup ([RFC8986](https://datatracker.ietf.org/doc/rfc8986/)).
In SRv6 L3VPN, each node is assigned an SRv6 Locator prefix. When creating a
tunnel endpoint on an edge node, the node assigns a function to an individual IP
address from this prefix and uses BGP to advertise the reachability of its tunnel
endpoint, implemented as an SRv6 function, to other edge nodes
([RFC9252](https://datatracker.ietf.org/doc/rfc9252/)).

ISIS as the Internal Gateway Protocol (IGP) advertises SRv6-related reachability
information, such as the SRv6 Locator prefix and tunnel source addresses,
between all routers participating in the data plane for end-to-end reachability
between edge nodes ([RFC8667](https://datatracker.ietf.org/doc/rfc8667/)).
BGP is another option for distributing such SRv6 reachability information.
See [draft-hss-srv6ops-srv6-routing-planes](https://datatracker.ietf.org/doc/draft-hss-srv6ops-srv6-routing-planes/).

### Goals

This RFE extends the API in a way so that the OpenPERouter offers two distinct
protocol stacks of choice for VPN overlays that can partially coexist.

The first stack is the currently supported stack of EVPN VXLAN tunnels in
combination with BGP for underlay routing (and existing L3VNI and L2VNI CRDs).
We propose a new stack consisting of SRv6 tunnels in combination with ISIS
for underlay routing (with a new L3VPN CRD).
FRR's implementation of SRv6 tunnels currently does not support layer 2
capabilities. Therefore, L2VNI for East/West traffic between pods must
be able to coexist with the new L3VPN for North/South overlay routing to edge
nodes in the operator's network.
L3VNI and L3VPN will not currently coexist under any circumstances.

The RFE intends to keep breaking changes to the existing `v1alpha1` API minimal.
It prefers simple defaults and use knobs for power users wherever possible.

FRR will be used alongside the Linux kernel to handle the data plane.

The suggested implementation of L3VPN will add support for IPv4 and
IPv6 overlays. It will use ISIS as the IGP to exchange underlay reachability
information for SRv6 between Kubernetes nodes and the network's edge nodes.
Both eBGP or iBGP will be supported to exchange SRv6 overlay routes.

To keep validation simple and cover the most common use cases, we will add
support for Type 1 Route Distinguishers only. For NET addresses, OpenPERouter
will at first only support those with a 6-byte system ID in canonical dot
notation (e.g. `XX.XXXX.XXXX.XXXX.XXXX.XX`).

Standalone ISIS will not be supported by a first implementation: even though
the improvements introduced by this RFE will likely already add a working
standalone ISIS implementation, further testing and tweaks are needed to
productize this feature. We will keep ISIS knobs to a minimum.

### Future goals not immediately covered by this enhancement

In order to limit the scope of this enhancement, some desired, logical and
strongly related changes are deferred to follow-up implementations.

All of the below points shall be addressed in immediate follow-ups to this
first implementation.

Beyond adding support for standalone ISIS, we will also want to extend ISIS
interface configuration, such as per-interface timers, as well as global ISIS
configuration.

It will also be needed to add support for other types of Route Distinguishers
and NET addresses.

As stated earlier, in the first version of this feature, L3VNI and L3VPN
cannot coexist under any circumstances. A follow-up will keep this limitation
inside the same VRF, but it will add support for L3VNI and L3VPN on the same
cluster for as long as these resources are inside different VRFs.

Status resources for newly added CRDs shall be added after
https://github.com/openperouter/openperouter/pull/225 and
https://github.com/openperouter/openperouter/pull/423 merged.

### Non-Goals of this enhancement

This RFE does not provide an expansion of the offered protocol stacks beyond
the 2 aforementioned tightly integrated stacks. Instead, a future enhancement
request (such as the API redesign in [PR341](https://github.com/openperouter/openperouter/pull/341))
shall modify the API in such a way that flexible combinations of
protocols are possible. For example, any of OSPF, ISIS or BGP may serve as the
underlay routing protocol. Either EVPN VXLAN, EVPN SRv6, or IP VPN SRv6 can be
used on top of any of these. Whereas BGP always exchanges overlay routes.

L3VNI and L3VPN inside the same VRF are strictly incompatible with each other.
We will not add support for L2VNI with the operator's edge nodes in a mixed
L3VPN + L2VNI setup.

Furthermore, neither support for another routing daemon nor a data plane other
than the Linux kernel are within the scope of this or any immediate follow-up
enhancement.

## Proposal

This Request For Enhancement proposes the expansion of the API with, and the
implementation of, a new protocol stack to support IPv4/IPv6 L3VPN over SRv6
tunnels with ISIS as the underlay routing protocol, in addition to the currently
supported protocol stack (EVPN + underlay BGP).

### User Stories

- **As a network administrator**, I want to be able to tightly integrate a
Kubernetes cluster into my SRv6-enabled L3VPN without having to terminate tunnels
on the ToR switches.
- **As a cluster administrator**, I want to connect my Kubernetes services and
pods directly to my company's SRv6-enabled L3VPN overlay, and at the same time 
I want to be able to set up L2VNIs between the Kubernetes nodes, spanning an L2
domain between pods on different nodes.
- **As a cluster user**, I want to advertise a Kubernetes service IP so that it
can be reached from other nodes inside the same VPN, and I want L2 reachability
between pods across different nodes inside the same VRF.

#### Story 1

An operator runs an SRv6-enabled network with IPv6 and ISIS as the IGP. The
operator's edge nodes implement IPv4 and IPv6 VPN over SRv6 (L3VPN), exchange
underlay reachability via ISIS, and exchange L3VPN routes via BGP. They want to
span ISIS all the way to the Kubernetes cluster and want to peer iBGP between
their tunnel edge nodes (and potentially Route Reflectors) and the Kubernetes
nodes. The operator also runs MetalLB on their Kubernetes nodes to advertise
Kubernetes service VIPs to the network.

The operator does not want to use EVPN (neither L2VNI nor L3VNI) but L3VPN over
SRv6.

The operator configures the OpenPERouter underlay with the required ISIS
configuration, such as the ISIS base NET address (which will be incremented
by the router index for each node), as well as the SRv6 information such as the
source CIDR (again offset by the router index for each Kubernetes node) and the
locator information such as the prefix (offset by the router index).

The operator then leverages OpenPERouter to configure required information for
the L3VPN, such as the VRF name, the Route Targets, information about the Route
Distinguisher to create unique routes, as well as the host session to peer with
MetalLB on the Kubernetes node itself.

The OpenPERouter pods start exchanging prefixes via ISIS and BGP and establish
an SRv6 L3VPN overlay with the rest of the network. On the other side, they
peer with MetalLB across the configured VRF.

Operator nodes inside the L3VPN can reach the Kubernetes service via the MetalLB
configured and advertised IPv4 and IPv6 addresses.

#### Story 2

The same as story 1, but with eBGP.

#### Story 3

An operator runs an SRv6-enabled network and wants to use SRv6 as the layer 3
domain. The operator wants to add East/West L2 connectivity between their pods
via EVPN for L2VNIs (available today in OpenPERouter). The operator does not
need North/South L2 connectivity between their pods and hosts behind their
edge nodes.

The operator configures OpenPERouter the same way as in Story 1 or 2 and the
OpenPERouter establishes end-to-end connectivity for the SRv6 L3 overlay.

The operator then uses OpenPERouter to set the required information for an L2VNI,
such as the VRF name, the VNI, as well as the host session to peer with MetalLB
on the Kubernetes node itself. The operator configures a CIDR for the VXLAN
tunnel source IPs. The operator also attaches their pods to the L2VNI enabled
network via a network attachment definition.

The API allows the operator to configure both L2VNIs and SRv6 L3VPNs on the same
cluster and for the same VRF. The SRv6 L3VPN provides the layer 3 overlay, while
EVPN continues to handle layer 2 connectivity. The API rejects the configuration
of L3VNIs and L3VPN at the same time.

Operator nodes inside the L3VPN can reach the Kubernetes service via the MetalLB
configured and advertised IPv4 and IPv6 addresses. Pods running on the
Kubernetes cluster can reach each other via the L2VNI.

### Notes/Constraints/Caveats

The following was already mentioned in the Goals / Non-Goals, but is reiterated
here as a reminder and for clarity:

FRR currently only supports SRv6 L3VPN, not L2VPN. Therefore, the
OpenPERouter will support a mixed L2 EVPN + L3VPN mode: L2VNIs
continue to use EVPN over VXLAN while L3VPNs use SRv6.
Configuration of L3VNI and L3VPN is mutually exclusive. Across different VRFs,
support for L3VNI and L3VPN on the same cluster will be added in a future iteration.

ISIS configuration is independent of SRv6. The OpenPERouter will
support standalone ISIS without any SRv6 overlay in a future version. For the
implementation of this RFE, ISIS standalone may work, but will not be tested 
nor supported.

SRv6 requires either ISIS or BGP neighbors for underlay reachability.
The OpenPERouter supports both ISIS underlay + BGP overlay in this RFE.
BGP underlay + BGP overlay configurations will be added with a future RFE. See
https://datatracker.ietf.org/doc/draft-hss-srv6ops-srv6-routing-planes/.

### Risks and Mitigations

**Risk:** FRR or Linux kernel support for specific features may be missing.  
**Mitigations:** Work with FRR upstream community / Kernel upstream community
in case of roadblocks.

**Risk:** FRR support for SRv6 is a cutting-edge feature and specific fixes
are only available in master. During upstream and downstream implementation and
testing, we might find further issues.  
**Mitigations:** Thoroughly test and specifically make sure that flakes do not
occur. Thoroughly test downstream and identify missing bits.  
**Examples:** At the time of this writing for fixes only available in the FRR master branch:  
- [no encapsulation not working](https://github.com/FRRouting/frr/pull/20716)  
- [broken no interface command](https://github.com/FRRouting/frr/pull/20378)  

**Risk:** Missing or bad host configuration, such as sysctl settings or MTU
mismatches. For example, FRR cannot install BGP routes if the VRF is not set to
strict mode at the right moment in time, leading to rejected (`B>r`) routes.
SRv6 also requires `rp_filter=0` on the VRF interfaces. ISIS neighbor
adjacencies are particularly sensitive to MTU mismatches.  
**Mitigations:** Thoroughly test both upstream and downstream.

**Risk:** Teardowns and restarts of OpenPERouter pods might cause issues.  
**Mitigations:** Test that the setup is stable and tolerates OpenPERouter
teardown and restarts.

**Risk:** Ignoring cleanup operations.  
**Mitigations:** When deleting / unconfiguring API resources / configuration
items, make sure that host configuration and FRR configuration are unconfigured.

**Risk:** Ignoring incompatible settings.  
**Mitigations:** Make sure that L3VPN and L3 EVPN are mutually exclusive
settings and can never be configured at the same time.

**Risk:** Incompatibility between EVPN L2VNI and SRv6 L3VPN.  
**Mitigations:** Determine if a re-architecture of Linux network components is
feasible. Otherwise, drop support for a mix of EVPN L2VNI and SRv6 L3VPN.

**Risk:** Introducing bad API choices.
**Mitigations:** The OpenPERouter is currently in an alpha API. We may thus
redesign the API as needed in future iterations.

## Design Details

### API version

This proposal targets the `v1alpha1` API version, extending it with new fields
for SRv6 while keeping changes to existing fields minimal. This ensures a
narrow scope focused solely on adding SRv6 support.

The current `v1alpha1` API structure is not an ideal fit for SRv6. For example,
the `EVPN` field holds IPv4 tunnel source information, while the new SRv6
stack requires IPv6 tunnel source information. Therefore, `EVPN.VtepCIDR` will
be moved into a unified `TunnelEndpoint` struct and renamed `CIDRS`. The
`TunnelEndpoint` struct is used by the EVPN and SRv6 stacks alike.

A broader API redesign is underway in
[PR341](https://github.com/openperouter/openperouter/pull/341), which will
incorporate lessons learned from this SRv6 enhancement.

### API types

At time of this writing, neither the suggested changes to the `Underlay` CRD nor
the new `L3VPN` CRD require status updates. Therefore, the below sections only
present the respective `Spec` definitions. Status resources shall be added after
https://github.com/openperouter/openperouter/pull/225 and
https://github.com/openperouter/openperouter/pull/423 merged.

#### UnderlaySpec and related

**UnderlaySpec**

```
// UnderlaySpec defines the desired state of Underlay.
// +kubebuilder:validation:XValidation:rule="!has(self.srv6) || has(self.isis)",message="SRv6 can only be configured if isis is set"
// +kubebuilder:validation:XValidation:rule="!has(self.srv6) || (has(self.tunnelEndpoint) && has(self.tunnelEndpoint.cidrs) && self.tunnelEndpoint.cidrs.exists(c, cidr(c).ip().family() == 6))",message="SRv6 requires at least one IPv6 CIDR in tunnelEndpoint.cidrs"
type UnderlaySpec struct {
	// nodeSelector specifies which nodes this Underlay applies to.
	// If empty or not specified, applies to all nodes (backward compatible).
	// Multiple Underlays with overlapping node selectors will be rejected.
	// +optional
	NodeSelector *metav1.LabelSelector `json:"nodeSelector,omitempty"`

	// asn is the local AS number to use for the session with the TOR switch.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=4294967295
	// +required
	ASN int64 `json:"asn,omitempty"`

	// routeridcidr is the ipv4 cidr to be used to assign a different routerID on each node.
	// +default="10.0.0.0/24"
	// +optional
	RouterIDCIDR *string `json:"routeridcidr,omitempty"`

	// Note: MaxItems:=128 is arbitrarily chosen to keep total CEL cost low
	// Note: kubeapilinter complained about 'the struct has no required fields', but CEL enforces either/or choices
	// for Address and Interface.

	// neighbors is the list of external BGP neighbors to peer with.
	// Multiple neighbors are supported for connecting to multiple TOR switches
	// or establishing redundant BGP sessions. Each neighbor address must be unique.
	// At least one neighbor is required.
	// +kubebuilder:validation:MinItems:=1
	// +kubebuilder:validation:MaxItems:=128
	// +required
	// +listType=atomic
	Neighbors []Neighbor `json:"neighbors,omitempty"` //nolint:kubeapilinter

	// nics is the list of physical nics to move under the PERouter namespace to connect
	// to external routers. At least one NIC is required.
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:items:Pattern=`^[a-zA-Z][a-zA-Z0-9._-]*$`
	// +kubebuilder:validation:items:MaxLength=15
	// +required
	// +listType=atomic
	Nics []string `json:"nics,omitempty"`

	// tunnelEndpoint contains tunnel endpoint configuration for the underlay.
	// +optional
	TunnelEndpoint *TunnelEndpointConfig `json:"tunnelEndpoint,omitempty"`

	// gracefulRestart configures BGP Graceful Restart behaviour.
	// When set, FRR advertises GR capability and preserves forwarding
	// state across restarts so that peers keep stale routes active.
	// Omit to disable graceful restart.
	// +optional
	GracefulRestart *GracefulRestartConfig `json:"gracefulRestart,omitempty"`

	// isis holds the ISIS configuration for the underlay.
	// +optional
	ISIS *ISISConfig `json:"isis,omitempty"`

	// srv6 holds the SRv6 configuration. Requires ISIS or Neighbors configuration.
	// +optional
	SRV6 *SRV6Config `json:"srv6,omitempty"`
}
```

General observations:
- Rename `EVPN` to `TunnelEndpoint` as the information in this field is used
  for both EVPN and SRv6.
- `L2VNI` and `L3VPN` shall be able to coexist.
- However, `L3VNI` and `L3VPN` resources are mutually exclusive. This must be
  enforced by a webhook.
- `ISIS` is independent from `SRV6`. However, `SRV6` can only be configured
  if `ISIS` is set.
- When `ISIS` is present, configure the host configuration and FRR configuration
  for `ISIS`.
- When `SRV6` is present, configure the host configuration and FRR configuration
  for SRv6.
- We can add support for OSPF and BGP for the underlay routing protocol later on.
  In that case, `SRV6` can only be configured if either `ISIS`, `Neighbors`, or
  `OSPF` are set. 
- A webhook must ensure that L3VNI and L2VNI resources cannot be created if
  tunnelEndpoint is not set. For SRv6, part of this validation is done by CEL 
  (SRv6 can only be set when an IPv6 CIDR is present in tunnelEndpoint).
- A webhook will have to enforce that L3VPN can only be created when SRv6
  configuration is present. However, we opt against adding a webhook on underlay
  deletion.

Fields:
- **Name:** `TunnelEndpoint`  
  **Description:** Object containing a single `TunnelEndpointConfig`.  
  **Comments:** Replaces current `EVPN` field.
- **Name:** `ISIS`  
  **Description:** Object containing a single `ISISConfig`.  
  **Comments:** OpenPERouter supports only a single ISIS process.
- **Name:** `SRV6`   
  **Description:** Holds a pointer to an SRV6Config object which holds SRv6-related
                   configuration (see below).  
  **Comments:** FRR seems to support only a single block for segment-routing,
                therefore a single object should be sufficient.  
  ```
  segment-routing
   srv6
    encapsulation
     source-address 2001:db8:1234::1
    exit
    locators
     ...
     !
    exit
    !
   exit
   !
  exit
  ```

**Neighbor**

```
// Neighbor represents a BGP Neighbor we want FRR to connect to.
// +kubebuilder:validation:XValidation:rule="has(self.asn) || has(self.type)",message="either ASN or Type must be set"
// +kubebuilder:validation:XValidation:rule="!has(self.asn) || !has(self.type)",message="ASN and Type cannot be set together"
// +kubebuilder:validation:XValidation:rule="has(self.holdTimeSeconds) == has(self.keepaliveTimeSeconds)",message="holdTimeSeconds and keepaliveTimeSeconds must be both set or both unset"
// +kubebuilder:validation:XValidation:rule="!has(self.holdTimeSeconds) || self.holdTimeSeconds == 0 || self.holdTimeSeconds >= 3",message="holdTimeSeconds must be 0 or >=3"
// +kubebuilder:validation:XValidation:rule="!has(self.holdTimeSeconds) || !has(self.keepaliveTimeSeconds) || self.keepaliveTimeSeconds <= self.holdTimeSeconds",message="keepaliveTimeSeconds must be lower than or equal to holdTimeSeconds"
// +kubebuilder:validation:XValidation:rule="has(self.address) || has(self.interface)",message="Either a valid Address or Interface name must be provided for Neighbor"
// +kubebuilder:validation:XValidation:rule="!has(self.address) || !has(self.interface)",message="Address and Interface cannot be set together for Neighbor"
// +kubebuilder:validation:XValidation:rule="!has(self.interface) || !has(self.addressFamilies) || !self.addressFamilies.exists(f, f.type == 'ipv4vpn' || f.type == 'ipv6vpn')",message="ipv4vpn and ipv6vpn address families are not supported for unnumbered (interface) neighbors"
// +kubebuilder:validation:XValidation:rule="!has(self.address) || !has(self.addressFamilies) || ip(self.address).family() != 4 || !self.addressFamilies.exists(f, f.type == 'ipv4vpn' || f.type == 'ipv6vpn')",message="ipv4vpn and ipv6vpn address families require an IPv6 neighbor address"
type Neighbor struct {
(...)
	// addressFamilies specifies the BGP address families that shall be enabled
	// for this BGP neighbor. evpn and ipv4vpn/ipv6vpn are mutually exclusive.
	// If ipv4vpn or ipv6vpn are set, the update source of this neighbor will
	// be set to the loopback's IPv6 address.
	// If addressFamilies is not provided or empty, the following defaults are
	// chosen:
	// For unnamed neighbors:
	// - ipv4unicast
	// - ipv6unicast if passthrough is configured with IPv6 local CIDR
	// - evpn if L2VNIs or L3VNIs are present.
	// For IPv4 neighbors:
	// - ipv4unicast
	// - ipv6unicast if passthrough is configured with IPv6 local CIDR
	// - evpn if L2VNIs or L3VNIs are present.
	// For IPv6 neighbors:
	// - ipv4unicast if L2VNIs or L3VNIs are present, or if passthrough is configured with IPv4 local CIDR
	// - ipv6unicast
	// - evpn if L2VNIs or L3VNIs are present
	// - ipv4vpn if L3VPNs and SRv6 configuration are present.
	// - ipv6vpn if L3VPNs and SRv6 configuration are present.
	// +kubebuilder:validation:MaxItems:=4
	// +kubebuilder:validation:XValidation:rule="!(self.exists(f, f.type == 'evpn') && self.exists(f, f.type == 'ipv4vpn' || f.type == 'ipv6vpn'))",message="EVPN and IPv4VPN/IPv6VPN address families are mutually exclusive"
	// +kubebuilder:validation:XValidation:rule="!self.exists(x, self.filter(y, x.type == y.type).size() > 1)",message="addressFamilies must not contain duplicate names"
	// +optional
	// +listType=atomic
	AddressFamilies []NeighborAddressFamily `json:"addressFamilies,omitempty"`
}
```

General observations:
- The `Neighbor` struct must be extended with field `AddressFamilies`.

- **Name:** `AddressFamilies`  
  **Description:** Determines for which address families this neighbor will
                   be enabled.  
  **Comments:** Accepts a maximum of 4 distinct address families. evpn and 
                ipv4vpn/ipv6vpn are mutually exclusive. This option also determines  
                if the neighbor's `update-source` is set to the loopback IP 
                address: if ipv4vpn or ipv6vpn are set, the update-source will 
                be the loopback's IPv6 address.
                Finding good defaults is quite complex. In the first implementation,
                defaults will be chosen as follows:  
                For unnamed neighbors:
                - ipv4unicast
                - ipv6unicast if passthrough is configured with IPv6 local CIDR
                - evpn if L2VNIs or L3VNIs are present.
                For IPv4 neighbors:
                - ipv4unicast
                - ipv6unicast if passthrough is configured with IPv6 local CIDR
                - evpn if L2VNIs or L3VNIs are present.
                For IPv6 neighbors:
                - ipv4unicast if L2VNIs or L3VNIs are present, or if passthrough is configured with IPv4 local CIDR
                - ipv6unicast
                - evpn if L2VNIs or L3VNIs are present
                - ipv4vpn if L3VPNs and SRv6 configuration are present.
                - ipv6vpn if L3VPNs and SRv6 configuration are present.

**NeighborAddressFamily**

```
// NeighborAddressFamily represents a single BGP address family configuration
// for a neighbor.
type NeighborAddressFamily struct {
	// type is the address family type.
	// +kubebuilder:validation:MinLength:=1
	// +kubebuilder:validation:MaxLength:=11
	// +kubebuilder:validation:Enum:=ipv4unicast;ipv6unicast;evpn;ipv4vpn;ipv6vpn
	// +required
	Type string `json:"type,omitempty"`
}
```

- **Name:** `Type`  
  **Description:** Holds the identifier of the address family for the neighbor.  
  **Comments:** The following address families will be enabled for the neighbor
  depending on the value of `type`. The type field is a combination of the AFI
  and SAFI:  
  - `ipv4unicast`: `address-family ipv4 unicast`
  - `ipv6unicast`: `address-family ipv6 unicast`
  - `evpn`: `address-family l2vpn evpn`
  - `ipv4vpn`: `address-family ipv4 vpn`
  - `ipv6vpn`: `address-family ipv6 vpn`

**TunnelEndpointConfig**

```
// TunnelEndpointConfig contains tunnel endpoint configuration for the underlay.
type TunnelEndpointConfig struct {
	// cidrs is a list of CIDRs to be used to assign IPs to the local tunnel endpoint on
	// each node. IPs derived from these CIDRs will be assigned to the local loopback.
	// At least one CIDR is required, and at most one CIDR per address family is allowed.
	// +kubebuilder:validation:MinItems:=1
	// +kubebuilder:validation:MaxItems:=2
	// +kubebuilder:validation:XValidation:rule="self.all(c, isCIDR(c))",message="all entries must be valid CIDRs"
	// +kubebuilder:validation:XValidation:rule="self.filter(c, isCIDR(c) && cidr(c).ip().family() == 4).size() <= 1",message="at most one IPv4 CIDR is allowed"
	// +kubebuilder:validation:XValidation:rule="self.filter(c, isCIDR(c) && cidr(c).ip().family() == 6).size() <= 1",message="at most one IPv6 CIDR is allowed"
	// +listType=atomic
	// +required
	CIDRs []string `json:"cidrs,omitempty"`
}
```

General observations:
- Rename `EVPN` to `TunnelEndpointConfig`.
- The IPv4 address of `TunnelEndpoint` will be used as the EVPN VXLAN source
  address.
- The IPv6 address of `TunnelEndpoint` will be used as the `encapsulation`
  `update-source` for SRv6 as well as the neighbor `update-source` of the BGP
  neighbors.

- **Name:** `CIDRs`  
  **Description:** Sets the IPv4 and IPv6 CIDRs on the `lo` loopback interface.  
  **Comments:** Expand `CIDR` to a slice of strings, `CIDRs`, that can hold one 
                IPv4 and one IPv6 address. The IPv4 address will be used by EVPN 
                and the IPv6 address will be used by SRv6.  

**ISISConfig**

```
// ISISConfig contains ISIS configuration for the underlay.
type ISISConfig struct {
	// baseNet holds the ISIS NET address.
	// The configured Net address is a base address which is offset by the node index of each node.
	// Only accepts the simplified NSAP format with a fixed AreaID length of 3 bytes and a 6 byte SystemID in compliance
	// with the U.S. GOSIP version 2.0 for a total of 10 bytes.
	// +required
	BaseNet ISISNet `json:"baseNet,omitempty"`
	// features enables ISIS boolean features.
	// Supported features are:
	// advertisePassiveOnly: configures ISIS to advertise only prefixes that belong to passive interfaces.
	// +kubebuilder:validation:MaxItems:=32
	// +listType=atomic
	// +optional
	Features []ISISFeature `json:"features,omitempty"`
	// interfaces holds additional ISIS interface level configuration and / or per
	// interface overrides. By default, OpenPERouter enables IPv6 on all required
	// interfaces with default settings.
	// +listType=atomic
	// +kubebuilder:validation:MaxItems:=128
	// +optional
	Interfaces []ISISInterface `json:"interfaces,omitempty"`
	// level configures the ISIS type, system wide. It defaults to level-1-2 unless specified otherwise.
	// +kubebuilder:validation:Enum:=1;2
	// +optional
	Level *int32 `json:"level,omitempty"`
}
```

General observations:
- Holds the configuration of a single ISIS process.
- These settings are currently fairly minimal. However, `ISISConfig` can easily  
  be extended to allow for further tweaking of the ISIS process.
  For a quick overview of ISIS, see the [ISIS](https://docs.frrouting.org/en/latest/isisd.html)
  documentation.

Fields:
- **Name:** `BaseNet`  
  **Description:** The base values for the ISIS process's single `net` addresses.
                   Offset by router's node index.  
  **Comments:** Each FRR ISIS process can in theory hold up to 3 `net` addresses. 
                This is useful when splitting, joining or renumbering areas. 
                Given that the OpenPERouter sits at the edge, it will not need 
                any of these. If an area is renumbered, it suffices to reconfigure 
                the OpenPERouter which will in turn restart the ISIS process.  
- **Name:** `Level`  
  **Description:** Either not set (meaning level-1-2), 1 (level-1) or 2 (level-2)  
  **Comments:** According to unofficial online sources
                ([0](https://www.reddit.com/r/networking/comments/w99vei/does_service_providers_still_use_a_single_level_2/),
                [1](https://www.reddit.com/r/networking/comments/5z5omd/isis_one_big_level_1_area/))
                providers tend to run a single, large L2 area. However, it would 
                probably be wise to account for L1 only areas as well as for
                multi-area configurations.
- **Name:** `Features`  
  **Description:** Configures ISIS to enable specific features.  
  **Comments:** Currently supports only `advertisePassiveOnly`. FRR's default is
                to advertise all interfaces.  
                Passive interfaces are included in
                link state advertisements, but ISIS routers do not establish
                adjacencies via them. In ISIS, it is a common practice to make
                the loopback interface passive, and to set advertise-passive-only.
                That way, adjacencies are formed over the physical interfaces,
                but only routes for the loopback IPs will be exchanged with
                neighbors, reducing the size of the routing table.
                See [advertise-passive-only](https://docs.frrouting.org/en/latest/isisd.html#clicmd-advertise-passive-only).  
- **Name:** `Interfaces`  
  **Description:** Slice of `ISISInterface`, containing interface configuration 
                   overrides.  
  **Comments:** Interface `lo` will by default be configured as a passive ISIS interface
                for IPv6. OpenPERouter auto-derives all other relevant interfaces 
                from `UnderlaySpec.Nics` and enables ISIS for IPv6 on them. 
                `Interfaces` allows for further tuning of interface related 
                configuration and/or for overrides, including the `Nics` and `lo`.


**ISISFeature**

```
// ISISFeature represents a single ISIS feature.
// +kubebuilder:validation:MinLength:=1
// +kubebuilder:validation:MaxLength:=128
// +kubebuilder:validation:Enum:=advertisePassiveOnly
type ISISFeature string
```

General observations:
- Used for validation purposes.

**ISISNet**

```
// ISISNet represents a single ISIS NET address.
// Only accepts the simplified NSAP format with a fixed AreaID length of 3 bytes and a 6 byte SystemID in compliance
// with the U.S. GOSIP version 2.0 for a total of 10 bytes.
// +kubebuilder:validation:MinLength:=25
// +kubebuilder:validation:MaxLength:=25
// +kubebuilder:validation:XValidation:rule=`self.matches('^[0-9a-f]{2}\\.([0-9a-f]{4}\\.){4}[0-9a-f]{2}$')`,message="Provided net address must match canonical format"
type ISISNet string
```

General observations:
- ISISNet stores the ISIS NET (Network Entity Title) address, a special form of
  NSAP (Network Service Access Point).
- Used for validation purposes. Currently, the rules are relatively strict
  and enforce a net value with the following format: `49.0001.0002.0003.0004.00`
- We use the simplified NSAP format with a fixed AreaID length of 3 bytes and a 6 byte SystemID in compliance with the
  U.S. GOSIP version 2.0 for a total of 10 bytes. These addresses are sufficient for IP routing purposes. Future
  implementations should consider expanding this to allow for a more flexible, 20 byte long representation.
  Source: Cisco Press IS-IS Network Design Solutions, chapter: NSAP Format
- The fixed length of 25 characters only accommodates the most common NET format
  with a 6-byte system ID in canonical dot notation (e.g. `XX.XXXX.XXXX.XXXX.XXXX.XX`).
  While ISIS NET addresses can technically vary in length (1-8 byte area ID,
  6-byte system ID, 1-byte selector), this restriction is intentional to keep
  validation simple and covers the standard use case.

**IPFamily**

```
// IPFamily specifies which address families are enabled.
// +kubebuilder:validation:Enum:=ipv4;ipv6;dualstack
type IPFamily string

const (
	IPFamilyIPv4      IPFamily = "ipv4"
	IPFamilyIPv6      IPFamily = "ipv6"
	IPFamilyDualStack IPFamily = "dualstack"
)
```

General observations:
- Enum type used to select which address families are enabled on an interface.
- Replaces separate IPv4/IPv6 boolean fields with a single, self-documenting field.
- `IPv4` enables only IPv4, `IPv6` enables only IPv6, `DualStack` enables both.

**ISISInterface**

```
// ISISInterface holds ISIS interface level configuration.
type ISISInterface struct {
	// name of the interface that these settings shall apply to.
	// +kubebuilder:validation:XValidation:rule=`self.matches('^[^\\/:\\s]+$')`,message="Interface must not contain /, :, or whitespace"
	// +kubebuilder:validation:XValidation:rule=`self != '.' && self != '..'`,message="Interface cannot be . or .."
	// +kubebuilder:validation:MaxLength:=15
	// +kubebuilder:validation:MinLength:=1
	// +required
	Name string `json:"name,omitempty"`
	// ipFamily configures which address families ISIS is enabled for on this interface.
	// +optional
	IPFamily *IPFamily `json:"ipFamily,omitempty"`
	// features enables ISIS interface boolean features.
	// Supported features are:
	// passive: configures ISIS passive mode on this interface.
	// +kubebuilder:validation:MaxItems:=32
	// +listType=atomic
	// +optional
	Features []ISISInterfaceFeature `json:"features,omitempty"`
}
```

General observations:
- In FRR, it is possible to set the interface level directly on the interface. 
  However, we currently enforce the level ISIS process wide. Interface 
  configuration is fairly minimal and ignores further tuning such as timers:  
  https://docs.frrouting.org/en/latest/isisd.html#isis-interface  
  Further parameters can easily be added to the interface in the future.
- Defaults to IPv6 passive enabled on `lo`, and IPv6 active enabled on the 
  interfaces specified in `nics`. 
- Used for overrides that deviate from the defaults, such as enabling/disabling 
  ISIS on specific interfaces. 
- Can later be extended for other settings, such as timers.

Fields:
- **Name:** `Name`  
  **Description:** Name of the interface that these changes shall be applied to.  
  **Comments:** N/A  
- **Name:** `IPFamily`
  **Description:** Configures which address families ISIS is enabled for on this interface (`IPv4`, `IPv6`, or `DualStack`).
  **Comments:** N/A
- **Name:** `Features`  
  **Description:** Configures ISIS to enable interface specific features.  
  **Comments:** Currently supports only `passive`.

**ISISInterfaceFeature**

```
// ISISInterfaceFeature represents a single ISIS feature of an ISIS interface.
// +kubebuilder:validation:MinLength:=1
// +kubebuilder:validation:MaxLength:=128
// +kubebuilder:validation:Enum:=passive
type ISISInterfaceFeature string
```

General observations:
- Used for validation purposes.

**SRV6Config**

```
// SRV6Config contains SRV6 configuration for the underlay.
type SRV6Config struct {
	// locator defines the locator for this SRv6 VPN.
	// +required
	Locator SRV6Locator `json:"locator,omitzero"`
}
```

General observations:
- Holds the single SRv6 configuration for the FRR process.
- Configures the `srv6` section in FRR, e.g.:

  ```
  segment-routing
   srv6
    encapsulation
     source-address 2001:db8:1234::1
    exit
    locators
     locator MAIN
      prefix fd00:0:10::/48 block-len 32 node-len 16
      behavior usid
      format usid-f3216
     exit
     !
    exit
    !
   exit
   !
  exit
  ```
- With regards to the above snippet of configuration - the behavior for the SRv6
  tunnel source IP address is roughly as follows:
  - If there's only a single IPv6 address on the loopback, and no routable IPv6
    address on the interface: use IPv6 address on the loopback.
  - If there's an IPv6 address on the interface: use that address, regardless of whether
    an IPv6 address is configured on the loopback.
  - If `segment-routing.srv6-encapsulation.source-address` is set: use that
    address as a source address.

  In the interest of having deterministic behavior, we always explicitly
  configure the encapsulation source-address the same as the BGP update source;
  this information is derived from the `TunnelEndpoint.CIDRs` IPv6 address.

Fields:
- **Name:** `Locator`  
  **Description:** Locator configuration other than `source-address`. Holds a 
                   single `locator` configuration.  
  **Comments:** FRR supports more than a single locator, but OpenPERouter allows
                only a single locator, in the interest of simplicity.

**SRV6Locator**

```
// SRV6Locator holds the configuration of a locator for SRv6.
type SRV6Locator struct {
	// basePrefix is the CIDR to be used for the locator, offset by the router index.
	// +kubebuilder:validation:XValidation:rule="isCIDR(self) && cidr(self).ip().family() == 6",message="prefix must be an IPv6 CIDR"
	// +kubebuilder:validation:MaxLength:=43
	// +kubebuilder:validation:MinLength:=1
	// +required
	BasePrefix string `json:"basePrefix,omitempty"`

	// format specifies the format of the locator. Defaults to usid-f3216
	// +kubebuilder:validation:MaxLength:=40
	// +kubebuilder:validation:MinLength:=1
	// +kubebuilder:validation:Enum:=usid-f3216
	// +required
	Format string `json:"format,omitempty"`
}
```

General observations:
- Holds information required to configure the `locators` section, e.g.:

  ```
  locator MAIN
   prefix fd00:0:10::/48 block-len 32 node-len 16
   behavior usid
   format usid-f3216
  ```

Fields:
- **Name:** `BasePrefix`  
  **Description:** Base Prefix for SRv6 locator.  
  **Comments:** This specifies the base prefix in `<IPv6>/<Mask>` format, 
                which is offset by the node index to create unique prefixes 
                per node. While the `Format` field could theoretically help 
                auto-calculate the subnet mask, FRR requires explicit mask 
                configuration, so we keep the mask. The `Format` determines 
                the `block-len` and `node-len` values, which define how the 
                base address bits are used when adding the node index to 
                generate per-node prefixes.  
                The OpenPERouter must validate the `BasePrefix`'s prefix length 
                against the `Format` via webhook.
- **Name:** `Format`  
  **Description:** Locator format.  
  **Comments:** Currently, the only supported value is `usid-f3216`. With the 
                format, we can automatically determine `block-len` and `node-len` 
                and we can calculate the offset for the prefix from `BasePrefix`. 
                Valid values for `Format` are enforced by CEL.

#### L3VPN

```
// L3VPNSpec defines the desired state of L3VPN.
// +kubebuilder:validation:XValidation:rule="!has(self.hostsession) || self.hostsession.hostasn != self.hostsession.asn",message="hostASN must be different from asn"
type L3VPNSpec struct {
	// nodeSelector specifies which nodes this L3VPN applies to.
	// If empty or not specified, applies to all nodes.
	// Multiple L3VPNs can match the same node.
	// +optional
	NodeSelector *metav1.LabelSelector `json:"nodeSelector,omitempty"`

	// vrf is the name of the linux VRF to be used inside the PERouter namespace.
	// +kubebuilder:validation:Pattern=`^[a-zA-Z][a-zA-Z0-9_-]*$`
	// +kubebuilder:validation:MinLength:=1
	// +kubebuilder:validation:MaxLength:=15
	// +required
	VRF string `json:"vrf,omitempty"`

	// exportRTs are the Route Targets to be used for exporting routes.
	// If no exportRTs are provided, defaults to single export Route Target
	// <asn>:<rdAssignedNumber>.
	// +kubebuilder:validation:MaxItems:=100
	// +listType=atomic
	// +optional
	ExportRTs []RouteTarget `json:"exportRTs,omitempty"`

	// importRTs are the Route Targets to be used for importing routes.
	// importRTs must always be provided explicitly.
	// +kubebuilder:validation:MaxItems:=100
	// +listType=atomic
	// +required
	ImportRTs []RouteTarget `json:"importRTs,omitempty"`

	// rdAssignedNumber sets the Route Distinguisher's Assigned Number subfield.
	// The Administrator subfield is automatically set to the value of the router
	// ID. OpenPERouter uses Type 1 Route Distinguishers as defined in RFC4364,
	// meaning <Administrator subfield>:<Assigned Number subfield>.
	//
	// +kubebuilder:validation:Minimum:=1
	// +kubebuilder:validation:Maximum:=65535
	// +required
	RDAssignedNumber int32 `json:"rdAssignedNumber,omitempty"`

	// hostsession is the configuration for the host session.
	// +optional
	HostSession *HostSession `json:"hostsession,omitempty"`
}
```

General observations:
- Holds `L3VPN` configuration.
- `L3VPN` is mutually exclusive with `L3VNI` and can only be used when `SRV6`
  is configured on the underlay. Equally, the underlay's `SRV6` configuration 
  must not be removed when `L3VPN` resources are present. The webhooks must 
  enforce this.
- In EVPN, the RT is automatically calculated for us by FRR: The 
  imported/exported RT is `*:<VNI>`, unless ExportRTs and ImportRTs are set
  which allow for custom import / export route targets. Unfortunately, it is
  not possible to emulate the same behavior in FRR with SRv6: a wildcard RT 
  cannot be configured manually for importRTs, as FRR does not accept
  notation `*:<number>` and notation `0:<number>` is interpreted verbatim.
  For ExportRTs, we can auto-generate an RT though as `<asn>:<rdAssignedNumber>`.

Fields:
- **Name:** `NodeSelector`  
  **Description:** Node selector.  
  **Comments:** N/A  
- **Name:** `VRF`  
  **Description:** Name of the VRF to be created on the host.  
  **Comments:** N/A  
- **Name:** `ExportRTs`  
  **Description:** Configuration of Route Targets that shall be exported.  
  **Comments:** The same as https://github.com/openperouter/openperouter/pull/197.
                To keep CEL validation complexity low, capped at a maximum of
                100 items. Optional. If no exportRTs are provided, defaults to
                single export Route Target `<asn>:<rdAssignedNumber>`.  
- **Name:** `ImportRTs`  
  **Description:** Configuration of Route Targets that shall be imported.  
  **Comments:** The same as https://github.com/openperouter/openperouter/pull/197.
                To keep CEL validation complexity low, capped at a maximum of
                100 items. Required.  
- **Name:** `RDAssignedNumber`  
  **Description:** Content of the Assigned Number subfield of the Route
                   Distinguisher.  
  **Comments:** OpenPERouter supports Type 1 Route Distinguishers:
                https://datatracker.ietf.org/doc/html/rfc4364#section-4.2   
                The Administrator subfield is set to the routerID. The Assigned
                Number subfield is set to the value of `RDAssignedNumber`.
- **Name:** `HostSession`  
  **Description:** Configuration for host session with the node's BGP process
                   such as MetalLB.  
  **Comments:** N/A  

**RouteTarget**

```
// RouteTarget defines a BGP Extended Community for route filtering.
// +kubebuilder:validation:MaxLength:=21
// +kubebuilder:validation:XValidation:rule=`self.matches('^([0-9]{1,10}:[0-9]{1,10}|[0-9]{1,3}\\.[0-9]{1,3}\\.[0-9]{1,3}\\.[0-9]{1,3}:[0-9]{1,5})$')`,message="routeTarget must be in ASN:NN or IP:NN format"
// +kubebuilder:validation:XValidation:rule=`self.contains('.') || int(self.split(':')[0]) <= 4294967295`,message="ASN must not exceed 4294967295"
// +kubebuilder:validation:XValidation:rule=`self.contains('.') || int(self.split(':')[0]) <= 65535 || int(self.split(':')[1]) <= 65535`,message="ASN:NN route target requires either ASN <= 65535 or assigned number <= 65535"
// +kubebuilder:validation:XValidation:rule=`!self.contains('.') || int(self.split(':')[1]) <= 65535`,message="IP route targets require assigned number <= 65535"
type RouteTarget string
```

General observations:
- Used for CEL validation purposes.
- For Route Target format, see: https://en.wikipedia.org/wiki/Route_distinguisher
  Route Targets have a MaxLength of 21 characters and come in 3 different types with the following
  maximum values per field:  
  Type 0:  
  <Administrator subfield AS Number (2 octets)>:<Assigned number subfield (4 octets)>  
  65535:4294967295 (len 16)  
  Type 1:  
  <Administrator subfield IPv4 address (4 octets)>:<Assigned number subfield (2 octets)>  
  255.255.255.255:65535 (len 21)  
  Type 2:  
  <Administrator subfield 4-octet AS Number (4 octets)>:<Assigned number subfield (2 octets)>  
  4294967295:65535  (len 16)  


### Example resources

#### L3VPN only

**Underlay**

```
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: Underlay
metadata:
  name: underlay
  namespace: openperouter-system
spec:
  asn: 64514
  neighbors:
  - address: 2001:db8:1234::1
    asn: 64520
    ebgpMultiHop: true
  - address: 2001:db8:1234::2
    asn: 64520
    ebgpMultiHop: true
  nics:
  - toswitch1
  routeridcidr: 10.0.0.0/24
  tunnelEndpoint:
    cidrs:
    - 2001:db8:1234:5678::/64
  isis:
    baseNet: "49.0001.0002.0003.0004.00"
    level: 1
    interfaces:
    - name: "toswitch1"
      ipFamily: "ipv6"
    features:
    - "advertisePassiveOnly"
  srv6:
    locator:
      basePrefix: "fd00:0:32::/48"
      format: "usid-f3216"
```

**L3VPN**

```
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: L3VPN
metadata:
  name: red
  namespace: openperouter-system
spec:
  hostsession:
    asn: 64514
    hostasn: 64515
    localcidr:
      ipv4: 192.169.10.0/24
  vrf: red
  rdAssignedNumber: 100
  exportRTs:
  - "64514:100"
  importRTs:
  - "64520:100"
---
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: L3VPN
metadata:
  name: blue
  namespace: openperouter-system
spec:
  hostsession:
    asn: 64514
    hostasn: 64515
    localcidr:
      ipv4: 192.169.11.0/24
  rdAssignedNumber: 200
  exportRTs:
  - "64514:200"
  importRTs:
  - "64520:200"
  vrf: blue
```

#### L3VPN + L2VNI

**Underlay**

```
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: Underlay
metadata:
  name: underlay
  namespace: openperouter-system
spec:
  tunnelEndpoint:
    cidrs:
    - 100.65.0.0/24
    - 2001:db8:1234:5678::/64
  asn: 64514
  neighbors:
  - address: 2001:db8:1234::1
    asn: 64520
    ebgpMultiHop: true
  - address: 2001:db8:1234::2
    asn: 64520
    ebgpMultiHop: true
  - asn: 64512
    address: 192.168.11.2
  nics:
  - toswitch1
  routeridcidr: 10.0.0.0/24
  isis:
    baseNet: "49.0001.0002.0003.0004.00"
    level: 1
    interfaces:
    - name: "toswitch1"
      ipFamily: "ipv6"
  srv6:
    locator:
      basePrefix: "fd00:0:32::/48"
      format: "usid-f3216"
```

**L3VPN**

```
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: L3VPN
metadata:
  name: red
  namespace: openperouter-system
spec:
  hostsession:
    asn: 64514
    hostasn: 64515
    localcidr:
      ipv4: 192.169.10.0/24
  vrf: red
  rdAssignedNumber: 100
  exportRTs:
  - "64514:100"
  importRTs:
  - "64520:100"
```

**L2VNI**

```
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: L2VNI
metadata:
  name: layer2
  namespace: openperouter-system
spec:
  vni: 110
  vrf: red
  hostmaster:
    type: linux-bridge
    linuxBridge:
      autoCreate: true
  l2gatewayips: ["192.170.1.1/24"]
```

### Example outcome

#### L3VPN only

Given the above example resources, here are the pod network and FRR configuration
that will be applied to one of the nodes for the L3VPN only use case.

**Pod network configuration**

```
pe-kind-control-plane:/# ip a
1: lo: <LOOPBACK,UP,LOWER_UP> mtu 65536 qdisc noqueue state UNKNOWN group default qlen 1000
    link/loopback 00:00:00:00:00:00 brd 00:00:00:00:00:00
    inet 127.0.0.1/8 scope host lo
       valid_lft forever preferred_lft forever
    inet6 2001:db8:1234:5678::/128 scope global 
       valid_lft forever preferred_lft forever
    inet6 ::1/128 scope host proto kernel_lo 
       valid_lft forever preferred_lft forever
2: red: <NOARP,MASTER,UP,LOWER_UP> mtu 65575 qdisc noqueue state UP group default 
    link/ether 9e:c0:db:30:1f:31 brd ff:ff:ff:ff:ff:ff
3: br-pe-100: <BROADCAST,MULTICAST,UP,LOWER_UP> mtu 1500 qdisc noqueue master red state UNKNOWN group default 
    link/ether 42:13:9b:2f:fb:e5 brd ff:ff:ff:ff:ff:ff
4: blue: <NOARP,MASTER,UP,LOWER_UP> mtu 65575 qdisc noqueue state UP group default 
    link/ether 76:ab:77:87:e4:f5 brd ff:ff:ff:ff:ff:ff
5: br-pe-200: <BROADCAST,MULTICAST,UP,LOWER_UP> mtu 1500 qdisc noqueue master blue state UNKNOWN group default 
    link/ether 96:18:0e:ea:88:8d brd ff:ff:ff:ff:ff:ff
7: pe-100@if8: <BROADCAST,MULTICAST,UP,LOWER_UP> mtu 9450 qdisc noqueue master red state UP group default 
    link/ether 32:51:39:0c:ff:2b brd ff:ff:ff:ff:ff:ff link-netnsid 1
    inet 192.169.10.1/24 brd 192.169.10.255 scope global pe-100
       valid_lft forever preferred_lft forever
    inet6 fe80::3051:39ff:fe0c:ff2b/64 scope link proto kernel_ll 
       valid_lft forever preferred_lft forever
9: pe-200@if10: <BROADCAST,MULTICAST,UP,LOWER_UP> mtu 9450 qdisc noqueue master blue state UP group default 
    link/ether e6:48:e3:4e:e4:34 brd ff:ff:ff:ff:ff:ff link-netnsid 1
    inet 192.169.11.1/24 brd 192.169.11.255 scope global pe-200
       valid_lft forever preferred_lft forever
    inet6 fe80::e448:e3ff:fe4e:e434/64 scope link proto kernel_ll 
       valid_lft forever preferred_lft forever
348: toswitch1@if349: <BROADCAST,MULTICAST,UP,LOWER_UP> mtu 9500 qdisc noqueue state UP group 4242 
    link/ether aa:c1:ab:6b:86:26 brd ff:ff:ff:ff:ff:ff link-netnsid 0
    inet 192.168.11.3/24 brd 192.168.11.255 scope global toswitch1
       valid_lft forever preferred_lft forever
    inet6 2001:db8:11::3/64 scope global 
       valid_lft forever preferred_lft forever
    inet6 fe80::a8c1:abff:fe6b:8626/64 scope link proto kernel_ll 
       valid_lft forever preferred_lft forever
pe-kind-control-plane:/# ip -6 r
2001:db8:11::/64 dev toswitch1 proto kernel metric 256 pref medium
2001:db8:1234::1 nhid 32 via fe80::a8c1:abff:fe8c:4060 dev toswitch1 proto isis metric 20 pref medium
2001:db8:1234::2 nhid 32 via fe80::a8c1:abff:fe8c:4060 dev toswitch1 proto isis metric 20 pref medium
2001:db8:1234:5678:: dev lo proto kernel metric 256 pref medium
2001:db8:1234:5678::1 nhid 33 via fe80::a8c1:abff:fe17:8f7 dev toswitch1 proto isis metric 20 pref medium
fd00:0:10::/48 nhid 32 via fe80::a8c1:abff:fe8c:4060 dev toswitch1 proto isis metric 20 pref medium
fd00:0:11::/48 nhid 32 via fe80::a8c1:abff:fe8c:4060 dev toswitch1 proto isis metric 20 pref medium
fd00:0:32:e000:: nhid 14  encap seg6local action End.DT46 vrftable 1 dev red proto bgp metric 20 pref medium
fd00:0:32:e001:: nhid 15  encap seg6local action End.DT46 vrftable 2 dev blue proto bgp metric 20 pref medium
fd00:0:32:e002::/64 nhid 23  encap seg6local action End.X nh6 fe80::a8c1:abff:fe8c:4060 flavors next-csid lblen 32 nflen 16 dev toswitch1 proto isis metric 20 pref medium
fd00:0:32:e003::/64 nhid 25  encap seg6local action End.X nh6 fe80::a8c1:abff:fe17:8f7 flavors next-csid lblen 32 nflen 16 dev toswitch1 proto isis metric 20 pref medium
fd00:0:33::/48 nhid 33 via fe80::a8c1:abff:fe17:8f7 dev toswitch1 proto isis metric 20 pref medium
fe80::/64 dev toswitch1 proto kernel metric 256 pref medium
pe-kind-control-plane:/# ip vrf
Name              Table
-----------------------
red                  1
blue                 2
pe-kind-control-plane:/# ip route show table 1 | grep encap
192.168.20.0/24 nhid 39  encap seg6 mode encap segs 1 [ fd00:0:10:e001:: ] via inet6 fe80::a8c1:abff:fe8c:4060 dev toswitch1 proto bgp metric 20 
192.169.20.0/24 nhid 41  encap seg6 mode encap segs 1 [ fd00:0:11:e001:: ] via inet6 fe80::a8c1:abff:fe8c:4060 dev toswitch1 proto bgp metric 20 
pe-kind-control-plane:/# ip -6 route show table 1 | grep encap
2001:db8:20::/64 nhid 39  encap seg6 mode encap segs 1 [ fd00:0:10:e001:: ] via fe80::a8c1:abff:fe8c:4060 dev toswitch1 proto bgp metric 20 pref medium
2001:db8:169:20::/64 nhid 41  encap seg6 mode encap segs 1 [ fd00:0:11:e001:: ] via fe80::a8c1:abff:fe8c:4060 dev toswitch1 proto bgp metric 20 pref medium
```

**FRR configuration**

```
pe-kind-control-plane# show run
Building configuration...

Current configuration:
!
frr version 10.6.0_git
frr defaults traditional
hostname pe-kind-control-plane
log stdout
log timestamp precision 3
service integrated-vtysh-config
!
route-map allowall permit 1
exit
!
debug zebra events
debug zebra kernel
debug zebra rib
debug zebra nht
debug zebra nexthop
debug bgp keepalives
debug bgp neighbor-events
debug bgp nht
debug bgp updates in
debug bgp updates out
debug bgp zebra
debug bgp graceful-restart
debug bfd peer
debug bfd zebra
debug bfd network
!
vrf blue
exit-vrf
!
vrf red
exit-vrf
!
interface lo
 ipv6 router isis ISIS
 isis passive
exit
!
interface toswitch1
 ipv6 router isis ISIS
exit
!
router bgp 64514
 bgp router-id 10.0.0.1
 no bgp ebgp-requires-policy
 no bgp default ipv4-unicast
 no bgp network import-check
 neighbor 2001:db8:1234::1 remote-as 64520
 neighbor 2001:db8:1234::1 ebgp-multihop
 neighbor 2001:db8:1234::1 update-source 2001:db8:1234:5678::
 neighbor 2001:db8:1234::1 capability extended-nexthop
 neighbor 2001:db8:1234::2 remote-as 64520
 neighbor 2001:db8:1234::2 ebgp-multihop
 neighbor 2001:db8:1234::2 update-source 2001:db8:1234:5678::
 neighbor 2001:db8:1234::2 capability extended-nexthop
 !
 segment-routing srv6
  locator MAIN
 exit
 !
 address-family ipv4 vpn
  neighbor 2001:db8:1234::1 activate
  neighbor 2001:db8:1234::1 next-hop-self
  neighbor 2001:db8:1234::2 activate
  neighbor 2001:db8:1234::2 next-hop-self
 exit-address-family
 !
 address-family ipv6 unicast
  network 2001:db8:1234:5678::/128
  neighbor 2001:db8:1234::1 activate
  neighbor 2001:db8:1234::1 allowas-in
  neighbor 2001:db8:1234::2 activate
  neighbor 2001:db8:1234::2 allowas-in
 exit-address-family
 !
 address-family ipv6 vpn
  neighbor 2001:db8:1234::1 activate
  neighbor 2001:db8:1234::1 next-hop-self
  neighbor 2001:db8:1234::2 activate
  neighbor 2001:db8:1234::2 next-hop-self
 exit-address-family
 !
 address-family l2vpn evpn
  advertise-all-vni
  advertise-svi-ip
 exit-address-family
exit
!
router bgp 64514 vrf red
 bgp router-id 10.0.0.1
 no bgp ebgp-requires-policy
 no bgp default ipv4-unicast
 no bgp network import-check
 neighbor 192.169.10.2 remote-as 64515
 sid vpn per-vrf export auto
 !
 address-family ipv4 unicast
  network 192.169.10.2/32
  neighbor 192.169.10.2 activate
  neighbor 192.169.10.2 route-map allowall in
  neighbor 192.169.10.2 route-map allowall out
  rd vpn export 10.0.0.1:100
  rt vpn import 64520:100
  rt vpn export 64514:100
  export vpn
  import vpn
 exit-address-family
 !
 address-family ipv6 unicast
  neighbor 192.169.10.2 activate
  neighbor 192.169.10.2 route-map allowall in
  neighbor 192.169.10.2 route-map allowall out
  rd vpn export 10.0.0.1:100
  rt vpn import 64520:100
  rt vpn export 64514:100
  export vpn
  import vpn
 exit-address-family
exit
!
router bgp 64514 vrf blue
 bgp router-id 10.0.0.1
 no bgp ebgp-requires-policy
 no bgp default ipv4-unicast
 no bgp network import-check
 neighbor 192.169.11.2 remote-as 64515
 sid vpn per-vrf export auto
 !
 address-family ipv4 unicast
  network 192.169.11.2/32
  neighbor 192.169.11.2 activate
  neighbor 192.169.11.2 route-map allowall in
  neighbor 192.169.11.2 route-map allowall out
  rd vpn export 10.0.0.1:200
  rt vpn import 64520:200
  rt vpn export 64514:200
  export vpn
  import vpn
 exit-address-family
 !
 address-family ipv6 unicast
  neighbor 192.169.11.2 activate
  neighbor 192.169.11.2 route-map allowall in
  neighbor 192.169.11.2 route-map allowall out
  rd vpn export 10.0.0.1:200
  rt vpn import 64520:200
  rt vpn export 64514:200
  export vpn
  import vpn
 exit-address-family
exit
!
router isis ISIS
 is-type level-1
 net 49.0001.0002.0003.0004.00
 advertise-passive-only
 segment-routing srv6
  locator MAIN
 exit
exit
!
segment-routing
 srv6
  encapsulation
   source-address 2001:db8:1234:5678::
  exit
  locators
   locator MAIN
    prefix fd00:0:32::/48 block-len 32 node-len 16
    behavior usid
    format usid-f3216
   exit
   !
  exit
  !
 exit
 !
exit
!
end
```

### Important implementation details

#### MTU

We must adjust veth MTU to account for SRv6 SRH overhead. A caveat here is
that we might have to adjust veth MTU to the minimum of {VXLAN overhead; SRH overhead}
in mixed L2VNI + L3VPN scenarios.
We should monitor developments in FRR with regards to [encap.red](https://www.segment-routing.net/images/20250311-srv6-usid-linux-netdev-0x19.pdf)
which can completely get rid of the SRH. Unfortunately, this is currently only
implemented with static routes in FRR, not with BGP.

#### sysctls

We need to determine which sysctls must be set for EVPN, for SRv6 and which
ones to set in mixed scenarios. It may be OK to unconditionally set all required
sysctls for EVPN and SRv6 for as long as these do not interfere with the functioning
of the other protocol stack. However, in a future version of the implementation,
we should strive to use the most secure sysctls and we should only weaken
security (such as disabling `rp_filter`) where necessary. We also need to
verify if VRF `strict_mode` can be enabled for EVPN without causing issues.

#### Calculation of Locators

Each node's Locator is derived from the Locator prefix configured in the
Underlay CR by adding the node's index to the prefix portion of the address.
For example, given a Locator prefix of `fd00:0:11::/48` and a node index of `2`,
the resulting Locator is `fd00:0:13::/48` (`0x11 + 0x2 = 0x13`). This ensures
that each node receives a unique Locator within the same address space.

Below is an example implementation:

```
// OffsetPrefix adds nodeIndex to the network portion of an IPv4 or IPv6 prefix.
// For example, with prefix "fd00:0:11::/48", nodeIndex 2, and prefixLen 48,
// it returns "fd00:0:13::/48" (0x11 + 2 = 0x13). The host portion of the CIDR
// will be discarded.
func OffsetPrefix(prefix string, nodeIndex, prefixLen int) (string, error) {
	_, ipNet, err := net.ParseCIDR(prefix)
	if err != nil {
		return "", fmt.Errorf("failed to parse prefix %s: %w", prefix, err)
	}

	var endOfRange bool
	for ; nodeIndex > 0; nodeIndex-- {
		ipNet, endOfRange = gocidr.NextSubnet(ipNet, prefixLen)
		if endOfRange {
			return "", fmt.Errorf("failed to offset prefix %s by nodeIndex %d, end of range", prefix, nodeIndex)
		}
	}
	return ipNet.String(), nil
}
```

#### Calculation of net addresses

Each node's ISIS NET address is derived from the base NET address configured in
the Underlay CR by adding the node's index to the System ID portion of the
address. For example, given a base NET of `49.0001.0002.0003.0004.00` and a node
index of `1`, the resulting NET is `49.0001.0002.0003.0005.00` (the System ID
`0002.0003.0004` is incremented by `1` to become `0002.0003.0005`). This ensures
that each node receives a unique System ID within the same ISIS area.

Below is an example implementation:

```
type ISISSystemID [6]byte

// IncrementSystemID takes an ISIS SystemID and an offset and returns the result of the sum of both.
func IncrementSystemID(si ISISSystemID, offset int) (ISISSystemID, error) {
	carry := offset
	for i := range 6 {
		if carry == 0 {
			break
		}
		idx := len(si) - 1 - i
		res := int(si[idx]) + carry
		carry = res / 256
		si[idx] = byte(res % 256)
	}
	if carry > 0 {
		return si, fmt.Errorf("overflow while incrementing SystemID %s with offset %d", si, offset)
	}
	return si, nil
}
```

### Test Plan

All code is expected to have adequate unit tests as well as E2E tests.

##### Unit tests

In principle every added code should have complete unit test coverage.

##### Integration tests

Not needed.

##### e2e tests

**Immediate additions:**

E2E tests will be added under `e2etests/tests`:
- In a new file `l3vpn_routes.go` for L3VPN only tests. These tests replicate
all tests in `evpn_routes.go`, but for L3VPN.
- In a new file `l3vpn_l2.go` for L3VPN + L2VNI mixed cases. These tests
will replicate most of the tests in `evpn_l2.go`, plus testing that L3VPN
works simultaneously. The L2 tests will test pod to pod reachability,
whereas reachability from pods to hostA and hostB via L2 is not supported. Instead,
reachability from pods to hostA and hostB will be tested via L3.
As reaching a service via L3VPN is tested in `l3vpn_routes.go`, this will not
be needed in `l3vpn_l2.go`.

**Future additions:**

These tests will largely replicate the existing EVPN L3 tests, but for L3VPN.
- Pod restart / GR behavior with SRv6 (interaction with [Implement router resilience](https://github.com/openperouter/openperouter/pull/317))
- MTU mismatch handling for ISIS: Testing connecting to endpoints with maximum
  MTU from a pod and vice versa.

### Upgrade / Downgrade Strategy

API changes are kept minimal on purpose to be as backward-compatible
as possible with the `v1alpha1` API.
This RFE intends to limit changes to the API that break backwards compatibility
and instead defers them to the aforementioned API redesign.

Given that we are in an alpha version, some changes might however break backwards
compatibility.

### Monitoring Requirements

No monitoring planned.

### Dependencies

- Dependent on iBGP work, see [PR260](https://github.com/openperouter/openperouter/pull/260).

## Implementation History

- 2026-04: Early prototype.
