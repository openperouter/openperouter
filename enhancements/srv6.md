## Summary

This enhancement proposes an extension of the OpenPERouter API to add support
for SRV6 L3VPNs. It extends the `Underlay` CRD with ISIS and SRV6 configuration
fields, and adds a new `L3VPN` Custom Resource Definition.

## Motivation

Segment Routing instantiated on the IPv6 data plane (SRV6) is an implementation
of source routing over an IPv6 dataplane
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
In SRV6 L3VPN, each node is assigned an SRv6 Locator prefix. When creating a
tunnel endpoint on an edge node, the node assigns a function to an individual IP
address from this prefix and uses BGP to advertise the reachability of its tunnel
endpoint, implemented as an SRV6 function, to other edge nodes
([RFC9252](https://datatracker.ietf.org/doc/rfc9252/)).

ISIS as the Internal Gateway Protocol (IGP) advertises SRV6 related reachability
information, such as the SRv6 Locator prefix, between all routers participating
in the dataplane for end to end reachability between edge nodes.
([RFC8667](https://datatracker.ietf.org/doc/rfc8667/)).

### Goals

- Implementation of L3VPN over SRv6
  - with IPv4 overlay (definitely possible)
  - with IPv6 overlay (verify if possible with FRR / Linux kernel)
- Using ISIS as the IGP to exchange reachability information between the edge
nodes.
- Using FRR to exchange route information via BGP and ISIS and set up the dataplane.
- Using the Linux kernel for the dataplane.

### Non-Goals

- Implementation of EVPN over SRv6
- Implementation of L2VPN over SRv6
- Using OSPF as the IGP to exchange reachability information between the edge
nodes.
- Using another routing daemon.
- Using another dataplane than the Linux kernel.

## Proposal

<!--
This is where we get down to the specifics of what the proposal actually is.
This should have enough detail that reviewers can understand exactly what
you're proposing, but should not include things like API designs or
implementation. What is the desired outcome and how do we measure success?.
The "Design Details" section below is for the real
nitty-gritty.
-->

### User Stories

<!--
Detail the things that people will be able to do if this KEP is implemented.
Include as much detail as possible so that people can understand the "how" of
the system. The goal here is to make this feel real for users without getting
bogged down.
-->

- **As a network administrator**, I want to be able to tightly integrate a
kubernetes cluster into my SRv6 enabled L3VPN without having to terminate tunnels
on the ToR switches.
- **As a cluster administrator**, I want to connect my Kubernetes services and
pods directly to my company's SRv6 enabled L3VPN overlay.
- **As a cluster user**, I want to advertise a kubernetes service IP so that it
can be reached from other nodes inside the same VPN.

#### Story 1

An operator runs an SRv6 enabled network with IPv6 and ISIS as the IGP. The
operator's edge nodes implement L3VPN and exchange L3VPN information via ISIS
and BGP. They want to span ISIS all the way to the Kubernetes cluster and want
to peer iBGP between their tunnel edge nodes and the Kubernetes nodes (for
iBGP support, see https://github.com/openperouter/openperouter/pull/260).
The operator also runs Metallb on their kubernetes nodes to advertise kubernetes
service VIPs to the network.

The operator does not want to use EVPN but L3VPN over SRv6.

The operator configures the OpenPERouter underlay with the required ISIS
configuration, such as the ISIS base NET address (which will be incremented
by the router index for each node), as well as the SRv6 information such as the
source CIDR (again offset by the router index for each kubernetes node) and the
locator information such as the prefix (offset by the router index).

The operator then configures an L3VPN CR with the required information, such
as the VRF name, the RouteTarget, information about the Route Distinguisher
to create unique routes, as well as the `hostsession` to peer with Metallb
on the kubernetes node itself.

The OpenPERouter pods start exchanging prefixes via ISIS and BGP and establish
an SRv6 L3VPN overlay with the rest of the network. On the other side, they
peer with Metallb across the configured VRF.

Operator nodes inside the L3VPN can reach the kubernetes service via the Metallb
configured and advertised IPv4 and IPv6 addresses.

#### Story 2

The same as story 1, but with eBGP.

### Notes/Constraints/Caveats (Optional)

<!--
What are the caveats to the proposal?
What are some important details that didn't come across above?
Go in to as much detail as necessary here.
This might be a good place to talk about core concepts and how they relate.
-->

- FRR currently only supports SRv6 L3VPN, not L2VPN.

### Risks and Mitigations

<!--
What are the risks of this proposal, and how do we mitigate? Think broadly.
For example, consider both security and how this will impact the larger
Kubernetes ecosystem.

How will security be reviewed, and by whom?

How will UX be reviewed, and by whom?

Consider including folks who also work outside the SIG or subproject.
-->

**Risk:** FRR or Linux kernel support for specific features may be missing.  
**Mitigation:** Work with FRR upstream community / Kernel upstream community
in case of roadblocks.

**Risk:** FRR support might not be 100% stable with regards to all features. For
example, FRR cannot install BGP routes if the VRF is not set to strict mode at
the right moment in time, leading to rejected (`B>r`) routes.  
**Mitigation:** Thoroughly test and specifically make sure that flakes do not
occur.

**Risk:** Teardowns and restarts of OpenPERouter pods might cause issues.  
**Mitigations:** Test that the setup is stable and tolerates OpenPERouter
teardown and restarts. Focus specifically on ISIS neighbor adjacencies.

**Risk:** Ignoring cleanup operations.  
**Mitigations:** When deleting / unconfiguring API resources / configuration
items, make sure that host configuration and FRR configuration are unconfigured.

**Risk:** Ignoring incompatible settings.  
**Mitigations:** Make sure that L3VPN and EVPN are mutually exclusive settings
and can never be configured at the same time.

## Design Details

<!--
This section should contain enough information that the specifics of your
change are understandable. This may include API specs (though not always
required) or even code snippets. If there's any ambiguity about HOW your
proposal will be implemented, this is the place to discuss them.
-->

### API types

Neither the suggested changes to the `Underlay` nor the new `L3VPN` CRD require status updates. Therefore, the below
sections only present the respective `Spec` definition.

#### UnderlaySpec and related

**UnderlaySpec**

```
// UnderlaySpec defines the desired state of Underlay.
// +kubebuilder:validation:XValidation:rule="!has(self.evpn) || !has(self.srv6)",message="cannot have both EVPN and SRV6 VPN at the same time"
type UnderlaySpec struct {
	// NodeSelector specifies which nodes this Underlay applies to.
	// If empty or not specified, applies to all nodes (backward compatible).
	// Multiple Underlays with overlapping node selectors will be rejected.
	// +optional
	NodeSelector *metav1.LabelSelector `json:"nodeSelector,omitempty"`

	// ASN is the local AS number to use for the session with the TOR switch.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=4294967295
	// +required
	ASN uint32 `json:"asn,omitempty"`

	// RouterIDCIDR is the ipv4 cidr to be used to assign a different routerID on each node.
	// +kubebuilder:default="10.0.0.0/24"
	// +optional
	RouterIDCIDR string `json:"routeridcidr,omitempty"`

	// Neighbors is the list of external neighbors to peer with.
	// +kubebuilder:validation:MinItems=1
	Neighbors []Neighbor `json:"neighbors,omitempty"`

	// Nics is the list of physical nics to move under the PERouter namespace to connect
	// to external routers. This field is optional when using Multus networks for TOR connectivity.
	// +kubebuilder:validation:items:Pattern=`^[a-zA-Z][a-zA-Z0-9._-]*$`
	// +kubebuilder:validation:items:MaxLength=15
	Nics []string `json:"nics,omitempty"`

	// +optional
	EVPN *EVPNConfig `json:"evpn,omitempty"`

	// ISIS holds the ISIS configurations for the underlay, one per ISIS process.
	// +kubebuilder:validation:MaxItems=10
	ISIS []ISISConfig `json:"isis,omitempty"`

	// SRV6 holds the SRV6 configuration.
	// +optional
	SRV6 *SRV6Config `json:"srv6,omitempty"`
}
```

General observations:
- Extend the `UnderlaySpec` with a new validation rule that allows either
  `EVPN` or `SRV6` configuration to be present, or neither, but not both.
- When `SRV6` is present, configure the host configuration and FRR configuration
  for SRV6.

Fields:
- **Name:** `ISIS`  
  **Description:** object containing a slice of `ISISConfig`. A single `ISISConfig`  
               holds the configuration for a single `ISIS` process.  
  **Comments:** Limited to `MaxItems=10` to limit size of CEL validation. Could also  
            be limited to `MaxItems=1` or made a pointer if we want to support  
            a single ISIS process, only.
- **Name:** `SRV6`  
  **Description:** holds a pointer to an SRV6Config object which holds SRV6 related  
               configuration (see below).  
  **Comments:** FRR seems to support only a single block for segmentrouting,  
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

**ISISConfig**

```
// ISISConfig contains ISIS configuration for the underlay.
type ISISConfig struct {
	// Name holds the name of the ISIS process.
	// +kubebuilder:validation:Pattern=`^[a-zA-Z][a-zA-Z0-9._-]*$`
	// +kubebuilder:validation:MaxLength=15
	// +kubebuilder:validation:MinLength:=1
	// +required
	Name string `json:"name"`
	// Net holds the ISIS net address.
	// TODO: right now, the last part of the system ID is offset by routerID .. however, is there a more elegant solution??
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=3
	// +required
	Net []ISISNet `json:"net"`
	// Level configures the ISIS type, system wide. It defaults to level-1-2 unless specified otherwise.
	// +kubebuilder:validation:Enum=1;2
	// +optional
	Level uint32 `json:"level,omitempty"`
	// Interfaces holds ISIS interface level configuration.
	// +kubebuilder:validation:MaxItems=1000
	// +optional
	Interfaces []ISISInterface `json:"interfaces,omitempty"`
}
```

General observations:
- Holds the configuration of a single ISIS process.
- These settings are currently fairly minimal. However, `ISISConfig` can easily  
  be extended to allow for further tweaking of the ISIS process.

Fields:
- **Name:** `Name`  
  **Description:** Name of the ISIS process; required.  
  **Comments:** If using a single ISIS process for SRV6, the name could be  
                auto-derived.  
- **Name:** `Net`  
  **Description:** The base values for the ISIS `net` addresses. Offset by router index.  
  **Comments:** Each ISIS process can hold up to 3 `net` addresses. Question is,  
                do we want to support up to 3?  Should we rename this field to  
                make it clearer that this is the base value for `Net`, meaning that  
                the actual `net` value is `Net` + router index?  
- **Name:** `Level`  
  **Description:** Either not set (meaning level-1-2), 1 (level-1) or 2 (level-2)  
  **Comments:** N/A  
- **Name:** `Interfaces`  
  **Description:** Slice of `ISISInterface`, containing interface configuration.  
  **Comments:** Interface `lound` is currently hardcoded as a passive ISIS  
                interface for IPv4 and IPv6. We could also  
                automatically add all interfaces from the list of  
                `UnderlaySpec.Nics` as these are meant for upstream connectivity  
                and enable ISIS with `ipv6 router isis <ISIS process name>`.  
                On the other hand, `Interfaces` allows for further tuning of  
                interface related configuration and/or for overrides.  
                Therefore, `Interfaces` either might be superfluous and could be  
                dropped, or it could be made optional and used for additional   
                interfaces and overrides. Or we use no automation (other than   
                the loopback) and make it required for maximum flexibility.  

**ISISNet**

```
// ISISNet represents a single ISIS net address.
// +kubebuilder:validation:MinLength=25
// +kubebuilder:validation:MaxLength=25
// +kubebuilder:validation:XValidation:rule=`self.matches('^[0-9a-f]{2}\\.([0-9a-f]{4}\\.){4}[0-9a-f]{2}$')`,message="Provided net address must match canonical format"
type ISISNet string
```

General observations:
- Used for validation purposes only. Currently, the rules are relatively strict
  and enforce a net value with the following format: `49.0001.0002.0003.0004.00`
- The fixed length of 25 characters only accommodates the most common NET format
  with a 6-byte system ID in canonical dot notation (e.g. `XX.XXXX.XXXX.XXXX.XXXX.XX`).
  While ISIS NET addresses can technically vary in length (1-8 byte area ID,
  6-byte system ID, 1-byte selector), this restriction is intentional to keep
  validation simple and covers the standard use case.

**ISISInterface**

```
// ISISInterface holds ISIS interface level configuration.
type ISISInterface struct {
	// Name of the interface that these settings shall apply to.
	// +kubebuilder:validation:XValidation:rule=`self.matches('^[^\\/:\\s]+$')`,message="Interface must not contain /, :, or whitespace"
	// +kubebuilder:validation:XValidation:rule=`self != '.' && self != '..'`,message="Interface cannot be . or .."
	// +kubebuilder:validation:MaxLength=15
	// +kubebuilder:validation:MinLength:=1
	// +required
	Name string `json:"name"`
	// +optional
	// ipv4 isis <name> enabled
	IPv4 bool `json:"ipv4,omitempty"`
	// ipv6 isis <name> enabled
	// +optional
	IPv6 bool `json:"ipv6,omitempty"`
}
```

General observations:
- In FRR, it is possible to set the interface level directly on the interface.  
  However, we currently enforce the level ISIS process wide. Interface  
  configuration is fairly minimal and ignores further tuning such as timers:  
  https://docs.frrouting.org/en/latest/isisd.html#isis-interface  
  Further parameters can easily be added to the interface in the future should  
  we decide to keep this setting. On the other hand, if we remove the `Interfaces`  
  slice, we lose the possibility to configure these settings.

Fields:
- **Name:** `Name`  
  **Description:** Name of the interface that these changes shall be applied to.
  **Comments:** N/A  
- **Name:** `IPv4`  
  **Description:** Configure `ip router isis <ISIS process name>`  
  **Comments:** N/A  
- **Name:** `IPv6`  
  **Description:** Configure `ipv6 router isis <ISIS process name>`  
  **Comments:** N/A  

**SRV6Config**

```
// SRV6Config contains SRV6 configuration for the underlay.
type SRV6Config struct {
	// Source specifies the source for the SRV6 VPN.
	// +required
	Source SRV6Source `json:"source"`

	// Locator defines the locator for this SRV6 VPN.
	// +required
	Locator SRV6Locator `json:"locator"`
}
```

General observations:
- Holds the single SRV6 configuration for the FRR process.
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

Fields:
- **Name:** `Source`  
  **Description:** Used to configure the `source-address` of the locator, as well  
                   as the BGP neighbor `update-source`.  
  **Comments:** The same as `.EVPNConfig.VTEPCIDR` and `.EVPNConfig.VTEPInterface`  
                but cleaner by adding one more nested level.  
- **Name:** `Locator`  
  **Description:** Locator configuration other than `source-address`. Holds a  
                   single `locator` configuration.  
  **Comments:** FRR seems to support more than one, should we make this a slice  
                for more flexibility? But then how would we know which `Locator`  
                to use in other parts of the generated configuration?  

**SRV6Source**

```
// +kubebuilder:validation:XValidation:rule="has(self.cidr) != has(self.interface)",message="exactly one of cidr or interface must be specified"
type SRV6Source struct {
	// CIDR to assign IPs to the local VTEP on each node from.
	// The IPv6 address will be assigned to the loopback interface.
	// Mutually exclusive with interface.
	// +kubebuilder:validation:XValidation:rule="isCIDR(self) && cidr(self).ip().family() == 6",message="cidr must be an IPv6 CIDR"
	// +kubebuilder:validation:MaxLength:=43
	// +kubebuilder:validation:MinLength:=1
	// +optional
	CIDR string `json:"cidr,omitempty"`

	// Interface is the name of an existing interface to use as the VTEP source.
	// The interface must already have an IP address configured that will be used
	// as the VTEP IP. Mutually exclusive with cidr.
	// The ToR must advertise the interface IP into the fabric underlay
	// (e.g. via redistribute connected) so that the VTEP address is reachable
	// from other leaves.
	// +kubebuilder:validation:XValidation:rule=`self.matches('^[^\\/:\\s]+$')`,message="interface must not contain /, :, or whitespace"
	// +kubebuilder:validation:XValidation:rule=`self != '.' && self != '..'`,message="interface cannot be . or .."
	// +kubebuilder:validation:MaxLength=15
	// +kubebuilder:validation:MinLength:=1
	// +optional
	Interface string `json:"interface,omitempty"` // https://regex101.com/r/RlniVP/2 see kernel bool dev_valid_name(...)
}
```

General observations:
- The same as `.EVPNConfig.VTEPCIDR` and `.EVPNConfig.VTEPInterface`.

**SRV6Locator**

```
type SRV6Locator struct {
	// Name of the Locator.
	// +kubebuilder:validation:Pattern=`^[a-zA-Z][a-zA-Z0-9._-]*$`
	// +kubebuilder:validation:MaxLength=15
	// +kubebuilder:validation:MinLength:=1
	// +required
	Name string `json:"name"`

	// BasePrefix is the CIDR to be used for the locator, offset by the router index.
	// +kubebuilder:validation:XValidation:rule="isCIDR(self) && cidr(self).ip().family() == 6",message="prefix must be an IPv6 CIDR"
	// +kubebuilder:validation:MaxLength:=43
	// +kubebuilder:validation:MinLength:=1
	// +required
	BasePrefix string `json:"basePrefix"`

	// Format specifies the format of the locator. Defaults to usid-f3216
	// +kubebuilder:validation:Enum=usid-f3216
	// +optional
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
- **Name:** `Name`  
  **Description:** Locator name.  
  **Comments:** If we allow only a single locator, then the `Name` field can be  
                dropped and we can hardcode `MAIN`.  
- **Name:** `BasePrefix`  
  **Description:** Base Prefix for SRV6 locator.  
  **Comments:** This specifies the base prefix in `<IPv6>/<Mask>` format, which  
                is offset by the node index to create unique prefixes per node.  
                While the `Format` field could theoretically help auto-calculate  
                the subnet mask, FRR requires explicit mask configuration, so we  
                keep the mask. The `Format` determines the `block-len` and  
                `node-len` values, which define how the base address bits are  
                used when adding the node index to generate per-node prefixes.  
                The field name/description may need revision to better convey that  
                this is a base value that gets modified per node.  
- **Name:** `Format`  
  **Description:** Locator format.  
  **Comments:** Currently, the only supported value is `usid-f3216`. With the  
                format, we can automatically determine `block-len` and `node-len`  
                and we can calculate the offset for the `Prefix`.  

#### L3VPN

```
// L3VPNSpec defines the desired state of VPN.
// +kubebuilder:validation:XValidation:rule="!has(self.hostsession) || self.hostsession.hostasn != self.hostsession.asn",message="hostASN must be different from asn"
type L3VPNSpec struct {
	// NodeSelector specifies which nodes this L3VPN applies to.
	// If empty or not specified, applies to all nodes.
	// Multiple L3VPNs can match the same node.
	// +optional
	NodeSelector *metav1.LabelSelector `json:"nodeSelector,omitempty"`

	// VRF is the name of the linux VRF to be used inside the PERouter namespace.
	// +kubebuilder:validation:Pattern=`^[a-zA-Z][a-zA-Z0-9_-]*$`
	// +kubebuilder:validation:MaxLength=15
	// +required
	VRF string `json:"vrf"`

	// RouteTarget is the RouteTarget to be used, in ASN:NN or IP:NN format.
	// +kubebuilder:validation:MaxLength=26
	// +kubebuilder:validation:XValidation:rule=`self.matches('^([0-9]{1,10}:[0-9]{1,10}|[0-9]{1,3}\\.[0-9]{1,3}\\.[0-9]{1,3}\\.[0-9]{1,3}:[0-9]{1,10})$')`,message="routeTarget must be in ASN:NN or IP:NN format"
	// +optional
	RouteTarget string `json:"routeTarget,omitempty"`

	// RouteDistinguisherSuffix is the part of the RouteDistinguisher after the ':' to be used
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=4294967295
	// +optional
	RouteDistinguisherSuffix uint32 `json:"routeDistinguisherSuffix,omitempty"`

	// HostSession is the configuration for the host session.
	// +optional
	HostSession *HostSession `json:"hostsession,omitempty"`
}
```

General observations:
- Holds `L3VPN` configuration.
- `L3VPN` is mutually exclusive with `L3VNI` and can only be used when `SRV6`
  is configured on the underlay. The webhooks should enforce this.

Fields:
- **Name:** `NodeSelector`  
  **Description:** Node selector.  
  **Comments:** N/A  
- **Name:** `VRF`  
  **Description:** Name of the VRF to be created on the host.  
  **Comments:** N/A  
- **Name:** `RouteTarget`  
  **Description:** Configuration of RouteTarget that shall be imported and exported.  
  **Comments:** In EVPN, the RT is automatically calculated for us by FRR. The  
                imported/exported RT is `*:<VNI>`. I have to do further research  
                if we can use this wildcard when manually importing / exporting  
                RTs. In any case, the syntax is the one supported by FRR. We should  
                probably leave this field configurable for maximum compatibility  
                with 3rd party providers. I am wondering if the current default  
                behavior in EVPN might be too strict as well for cross-vendor  
                compatibility (but this doesn't matter for L3VPN).
- **Name:** `RouteDistinguisherSuffix`  
  **Description:** Right part of the RouteDistinguisher, after the `:`, with the  
                   left part being the router ID.  
  **Comments:** `RouteDistinguisherSuffix` is a very clumsy name. We might instead  
                want to add a field `VNI` (the same as for EVPN) and the right part  
                of the RD (and maybe even the RT?) could be calculated from  
                routerID and `VNI`. Right now, the `RouteDistinguisherSuffix`  
                is used exactly as the `VNI` when setting up interfaces on the host  
                (meaning that interfaces have the `RouteDistinguisherSuffix`)  
                as a suffix to their name.
- **Name:** `HostSession`  
  **Description:** Configuration for host session with the node's BGP process such as Metallb.  
  **Comments:** N/A  

### Example resources

#### Underlay

```
---
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
  - toswitch
  routeridcidr: 10.0.0.0/24
  isis:
  - name: "ISIS"
    net:
    - "49.0001.0002.0003.0004.00"
    level: 1
    interfaces:
    - name: "toswitch"
      ipv6: true
  srv6:
    source:
      cidr: "2001:db8:1234:5678::/64"
    locator:
      name: "MAIN"
      basePrefix: "fd00:0:32::/48"
      format: "usid-f3216"
```

#### L3VPN

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
  routeDistinguisherSuffix: 100
  routeTarget: "64514:100"
```

### Example outcome

Given the above example resources, here are the pod network and FRR configuration
that will be applied to one of the nodes:

**Pod network configuration**

```
router-88kjs:/# ip a
1: lo: <LOOPBACK,UP,LOWER_UP> mtu 65536 qdisc noqueue state UNKNOWN group default qlen 1000
    link/loopback 00:00:00:00:00:00 brd 00:00:00:00:00:00
    inet 127.0.0.1/8 scope host lo
       valid_lft forever preferred_lft forever
    inet6 ::1/128 scope host proto kernel_lo 
       valid_lft forever preferred_lft forever
2: eth0@if4: <BROADCAST,MULTICAST,UP,LOWER_UP> mtu 1500 qdisc noqueue state UP group default qlen 1000
    link/ether 7a:17:75:c9:ed:a3 brd ff:ff:ff:ff:ff:ff link-netnsid 0
    inet 10.244.1.3/24 brd 10.244.1.255 scope global eth0
       valid_lft forever preferred_lft forever
    inet6 fd00:10:244:1::3/64 scope global 
       valid_lft forever preferred_lft forever
    inet6 fe80::7817:75ff:fec9:eda3/64 scope link proto kernel_ll 
       valid_lft forever preferred_lft forever
3: lound: <BROADCAST,NOARP,UP,LOWER_UP> mtu 1500 qdisc noqueue state UNKNOWN group default 
    link/ether 8e:0f:26:84:94:df brd ff:ff:ff:ff:ff:ff
    inet6 2001:db8:1234:5678::1/128 scope global 
       valid_lft forever preferred_lft forever
    inet6 fe80::8c0f:26ff:fe84:94df/64 scope link proto kernel_ll 
       valid_lft forever preferred_lft forever
4: red: <NOARP,MASTER,UP,LOWER_UP> mtu 65575 qdisc noqueue state UP group default 
    link/ether 16:5a:9b:98:ad:8a brd ff:ff:ff:ff:ff:ff
5: br-pe-100: <BROADCAST,MULTICAST,UP,LOWER_UP> mtu 1500 qdisc noqueue master red state UNKNOWN group default 
    link/ether da:69:f6:59:75:34 brd ff:ff:ff:ff:ff:ff
6: blue: <NOARP,MASTER,UP,LOWER_UP> mtu 65575 qdisc noqueue state UP group default 
    link/ether b6:93:2e:60:c6:12 brd ff:ff:ff:ff:ff:ff
7: br-pe-200: <BROADCAST,MULTICAST,UP,LOWER_UP> mtu 1500 qdisc noqueue master blue state UNKNOWN group default 
    link/ether 62:d3:c2:1c:02:47 brd ff:ff:ff:ff:ff:ff
10: pe-100@if11: <BROADCAST,MULTICAST,UP,LOWER_UP> mtu 1500 qdisc noqueue master red state UP group default 
    link/ether a2:ff:2b:d6:7b:05 brd ff:ff:ff:ff:ff:ff link-netnsid 0
    inet 192.169.10.1/24 brd 192.169.10.255 scope global pe-100
       valid_lft forever preferred_lft forever
    inet6 fe80::a0ff:2bff:fed6:7b05/64 scope link proto kernel_ll 
       valid_lft forever preferred_lft forever
12: pe-200@if13: <BROADCAST,MULTICAST,UP,LOWER_UP> mtu 1500 qdisc noqueue master blue state UP group default 
    link/ether d2:df:f7:06:77:a4 brd ff:ff:ff:ff:ff:ff link-netnsid 0
    inet 192.169.11.1/24 brd 192.169.11.255 scope global pe-200
       valid_lft forever preferred_lft forever
    inet6 fe80::d0df:f7ff:fe06:77a4/64 scope link proto kernel_ll 
       valid_lft forever preferred_lft forever
56: toswitch@if57: <BROADCAST,MULTICAST,UP,LOWER_UP> mtu 9500 qdisc noqueue state UP group default 
    link/ether aa:c1:ab:80:af:ac brd ff:ff:ff:ff:ff:ff link-netnsid 1
    inet 192.168.11.4/24 brd 192.168.11.255 scope global toswitch
       valid_lft forever preferred_lft forever
    inet 172.16.1.1/32 scope global toswitch
       valid_lft forever preferred_lft forever
    inet6 fe80::a8c1:abff:fe80:afac/64 scope link 
       valid_lft forever preferred_lft forever
router-88kjs:/# ip -6 r
2001:db8:1234::1 nhid 41 via fe80::a8c1:abff:fed9:1f62 dev toswitch proto isis metric 20 pref medium
2001:db8:1234::2 nhid 41 via fe80::a8c1:abff:fed9:1f62 dev toswitch proto isis metric 20 pref medium
2001:db8:1234:5678:: nhid 42 via fe80::a8c1:abff:fe96:57d3 dev toswitch proto isis metric 20 pref medium
2001:db8:1234:5678::1 dev lound proto kernel metric 256 pref medium
fd00:0:10::/48 nhid 41 via fe80::a8c1:abff:fed9:1f62 dev toswitch proto isis metric 20 pref medium
fd00:0:11::/48 nhid 41 via fe80::a8c1:abff:fed9:1f62 dev toswitch proto isis metric 20 pref medium
fd00:0:32::/48 nhid 42 via fe80::a8c1:abff:fe96:57d3 dev toswitch proto isis metric 20 pref medium
fd00:0:33:e000:: nhid 27  encap seg6local action End.DT46 vrftable 1 dev red proto bgp metric 20 pref medium
fd00:0:33:e001:: nhid 28  encap seg6local action End.DT46 vrftable 2 dev blue proto bgp metric 20 pref medium
fd00:0:33:e002::/64 nhid 26  encap seg6local action End.X nh6 fe80::a8c1:abff:fe96:57d3 flavors next-csid lblen 32 nflen 16 dev toswitch proto isis metric 20 pref medium
fd00:0:33:e003::/64 nhid 30  encap seg6local action End.X nh6 fe80::a8c1:abff:fed9:1f62 flavors next-csid lblen 32 nflen 16 dev toswitch proto isis metric 20 pref medium
fd00:10:244:1::1 dev eth0 src fd00:10:244:1::3 metric 1024 pref medium
fd00:10:244:1::/64 via fd00:10:244:1::1 dev eth0 src fd00:10:244:1::3 metric 1024 pref medium
fe80::/64 dev eth0 proto kernel metric 256 pref medium
fe80::/64 dev toswitch proto kernel metric 256 pref medium
fe80::/64 dev lound proto kernel metric 256 pref medium
default via fd00:10:244:1::1 dev eth0 metric 1024 pref medium
```

**FRR configuration**

```
router-88kjs:/# vtysh

Hello, this is FRRouting (version 10.6.0_git).
Copyright 1996-2005 Kunihiro Ishiguro, et al.

router-88kjs# show run
Building configuration...

Current configuration:
!
frr version 10.6.0_git
frr defaults traditional
hostname router-88kjs
log file /etc/frr/frr.log
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
interface lound
 ip router isis ISIS
 ipv6 router isis ISIS
 isis passive
exit
!
interface toswitch
 ipv6 router isis ISIS
exit
!
router bgp 64514
 bgp router-id 10.0.0.2
 no bgp ebgp-requires-policy
 no bgp default ipv4-unicast
 no bgp network import-check
 neighbor 2001:db8:1234::1 remote-as 64520
 neighbor 2001:db8:1234::1 ebgp-multihop
 neighbor 2001:db8:1234::1 update-source 2001:db8:1234:5678::1
 neighbor 2001:db8:1234::1 capability extended-nexthop
 neighbor 2001:db8:1234::2 remote-as 64520
 neighbor 2001:db8:1234::2 ebgp-multihop
 neighbor 2001:db8:1234::2 update-source 2001:db8:1234:5678::1
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
exit
!
router bgp 64514 vrf red
 bgp router-id 10.0.0.2
 no bgp ebgp-requires-policy
 no bgp default ipv4-unicast
 no bgp network import-check
 neighbor 192.169.10.3 remote-as 64515
 sid vpn per-vrf export auto
 !
 address-family ipv4 unicast
  network 192.169.10.3/32
  neighbor 192.169.10.3 activate
  neighbor 192.169.10.3 route-map allowall in
  neighbor 192.169.10.3 route-map allowall out
  rd vpn export 10.0.0.2:100
  rt vpn both 64514:100
  export vpn
  import vpn
 exit-address-family
 !
 address-family ipv6 unicast
  neighbor 192.169.10.3 activate
  neighbor 192.169.10.3 route-map allowall in
  neighbor 192.169.10.3 route-map allowall out
  rd vpn export 10.0.0.2:100
  rt vpn both 64514:100
  export vpn
  import vpn
 exit-address-family
exit
!
router bgp 64514 vrf blue
 bgp router-id 10.0.0.2
 no bgp ebgp-requires-policy
 no bgp default ipv4-unicast
 no bgp network import-check
 neighbor 192.169.11.3 remote-as 64515
 sid vpn per-vrf export auto
 !
 address-family ipv4 unicast
  network 192.169.11.3/32
  neighbor 192.169.11.3 activate
  neighbor 192.169.11.3 route-map allowall in
  neighbor 192.169.11.3 route-map allowall out
  rd vpn export 10.0.0.2:200
  rt vpn both 64514:200
  export vpn
  import vpn
 exit-address-family
 !
 address-family ipv6 unicast
  neighbor 192.169.11.3 activate
  neighbor 192.169.11.3 route-map allowall in
  neighbor 192.169.11.3 route-map allowall out
  rd vpn export 10.0.0.2:200
  rt vpn both 64514:200
  export vpn
  import vpn
 exit-address-family
exit
!
router isis ISIS
 is-type level-1
 net 49.0001.0002.0003.0005.00
 segment-routing srv6
  locator MAIN
 exit
exit
!
segment-routing
 srv6
  encapsulation
   source-address 2001:db8:1234:5678::1
  exit
  locators
   locator MAIN
    prefix fd00:0:33::/48 block-len 32 node-len 16
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

### Test Plan

All code is expected to have adequate unit tests as well as E2E tests.

##### Unit tests

In principle every added code should have complete unit test coverage.

##### Integration tests

Not needed.

##### e2e tests

E2E tests will be added under `e2etests/tests` in a new file `l3vpn_routes.go`.
These tests will largely replicate the existing EVPN L3 tests, but for L3VPN.

### Upgrade / Downgrade Strategy

Changes are largely backward compatible. We are also in an alpha version, so
we do not have to worry about upgrades / downgrades.

### Monitoring Requirements

No monitoring planned.

### Dependencies

- Dependent on iBGP work, see [PR260](https://github.com/openperouter/openperouter/pull/260).

## Implementation History

- 2026-04: Early prototype.
