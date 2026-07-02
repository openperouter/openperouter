# API Reference

## Packages
- [network.openperouter.io/v1alpha1](#networkopenperouteriov1alpha1)


## network.openperouter.io/v1alpha1

Package v1alpha1 contains API Schema definitions for the openpe v1alpha1 API group.

### Resource Types
- [L2VNI](#l2vni)
- [L3Passthrough](#l3passthrough)
- [L3VNI](#l3vni)
- [L3VPN](#l3vpn)
- [RawFRRConfig](#rawfrrconfig)
- [RouterNodeConfigurationStatus](#routernodeconfigurationstatus)
- [Underlay](#underlay)



#### BFDSettings



BFDSettings defines the BFD configuration for a BGP session.



_Appears in:_
- [Neighbor](#neighbor)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `receiveInterval` _integer_ | receiveInterval is the minimum interval that this system is capable of<br />receiving control packets in milliseconds.<br />Defaults to 300ms. |  | Maximum: 60000 <br />Minimum: 10 <br />Optional: \{\} <br /> |
| `transmitInterval` _integer_ | transmitInterval is the minimum transmission interval (less jitter)<br />that this system wants to use to send BFD control packets in<br />milliseconds. Defaults to 300ms |  | Maximum: 60000 <br />Minimum: 10 <br />Optional: \{\} <br /> |
| `detectMultiplier` _integer_ | detectMultiplier configures the detection multiplier to determine<br />packet loss. The remote transmission interval will be multiplied<br />by this value to determine the connection loss detection timer. |  | Maximum: 255 <br />Minimum: 2 <br />Optional: \{\} <br /> |
| `echoInterval` _integer_ | echoInterval configures the minimal echo receive transmission<br />interval that this system is capable of handling in milliseconds.<br />Defaults to 50ms |  | Maximum: 60000 <br />Minimum: 10 <br />Optional: \{\} <br /> |
| `echoMode` _boolean_ | echoMode enables or disables the echo transmission mode.<br />This mode is disabled by default, and not supported on multi<br />hops setups. |  | Optional: \{\} <br /> |
| `passiveMode` _boolean_ | passiveMode marks session as passive: a passive session will not<br />attempt to start the connection and will wait for control packets<br />from peer before it begins replying. |  | Optional: \{\} <br /> |
| `minimumTTL` _integer_ | minimumTTL configures, for multi hop sessions only, the minimum<br />expected TTL for an incoming BFD control packet. |  | Maximum: 254 <br />Minimum: 1 <br />Optional: \{\} <br /> |


#### CNIConfigType

_Underlying type:_ _string_

CNIConfigType selects the source of the CNI configuration.
It is the discriminator of the CNIDevice union and is designed to be
extended with future config sources (e.g. a NetworkAttachmentDefinition
reference or a filesystem path).

_Validation:_
- Enum: [RawConfig]

_Appears in:_
- [CNIDevice](#cnidevice)

| Field | Description |
| --- | --- |
| `RawConfig` | CNIConfigTypeRawConfig embeds the CNI config JSON directly in the spec.<br /> |


#### CNIDevice



CNIDevice invokes a CNI plugin to provision an interface in the router
netns. The config source is a discriminated union — additional source
variants can be added later if a concrete user need emerges.



_Appears in:_
- [UnderlayInterface](#underlayinterface)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `type` _[CNIConfigType](#cniconfigtype)_ | type selects the source of the CNI configuration. |  | Enum: [RawConfig] <br />Required: \{\} <br /> |
| `rawConfig` _[JSON](https://kubernetes.io/docs/reference/generated/kubernetes-api/v/#json-v1-apiextensions-k8s-io)_ | rawConfig embeds a CNI conflist JSON blob directly in this spec.<br />Only CNI spec >= 1.0.0 configurations are accepted. Immutable once<br />set: to change it, delete and recreate the<br />Underlay. Immutability is enforced by the validation webhook because<br />CEL transition rules cannot be evaluated inside atomic lists. |  | Type: object <br />Optional: \{\} <br /> |
| `interfaceName` _string_ | interfaceName is the name of the interface the CNI plugin creates<br />inside the router netns (passed as CNI_IFNAME). Defaults to "net1". | net1 | MaxLength: 15 <br />MinLength: 1 <br />Pattern: `^[a-zA-Z][a-zA-Z0-9._-]*$` <br />Optional: \{\} <br /> |
| `runtimeConfig` _[JSON](https://kubernetes.io/docs/reference/generated/kubernetes-api/v/#json-v1-apiextensions-k8s-io)_ | runtimeConfig is an opaque JSON object mapping CNI capability names<br />to the payloads passed as capability arguments to the CNI<br />invocation. Only keys that the plugin declares in its<br />"capabilities" config block are forwarded; undeclared keys are<br />silently stripped. Well-known capabilities include ips, mac,<br />bandwidth, portMappings, ipRanges and deviceID. Immutable once<br />set: to change it, delete and recreate the Underlay. Immutability<br />is enforced by the validation webhook because CEL transition rules<br />cannot be evaluated inside atomic lists. |  | Type: object <br />Optional: \{\} <br /> |


#### FailedResource



FailedResource describe failing router API resource



_Appears in:_
- [RouterNodeConfigurationStatusStatus](#routernodeconfigurationstatusstatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `kind` _[FailedResourceKind](#failedresourcekind)_ | kind resource type name (e.g.: L3VNI, L2VNI). |  | Enum: [Underlay L2VNI L3VNI FrrConfiguration L3Passthrough] <br />Required: \{\} <br /> |
| `name` _string_ | name failed API resource metadata.name. |  | MaxLength: 253 <br />MinLength: 1 <br />Required: \{\} <br /> |
| `reason` _[FailedResourceReason](#failedresourcereason)_ | reason failure reason. |  | Enum: [ValidationFailed DependencyFailed OverlayAttachmentFailed FrrConfigurationFailed] <br />MaxLength: 100 <br />MinLength: 1 <br />Required: \{\} <br /> |
| `message` _string_ | message human-readable failure description. |  | MaxLength: 500 <br />MinLength: 1 <br />Required: \{\} <br /> |


#### FailedResourceKind

_Underlying type:_ _string_



_Validation:_
- Enum: [Underlay L2VNI L3VNI FrrConfiguration L3Passthrough]

_Appears in:_
- [FailedResource](#failedresource)



#### FailedResourceReason

_Underlying type:_ _string_

FailedResourceReason machine-readable reason for a failure.

_Validation:_
- Enum: [ValidationFailed DependencyFailed OverlayAttachmentFailed FrrConfigurationFailed]
- MaxLength: 100
- MinLength: 1

_Appears in:_
- [FailedResource](#failedresource)

| Field | Description |
| --- | --- |
| `ValidationFailed` | FailedResourceReasonValidationFailed indicates failed pre-emptive semantic validation<br />(e.g., interface not found, VNI conflict).<br /> |
| `DependencyFailed` | FailedResourceReasonDependencyFailed dependent-on resource is not ready<br />(e.g., L2VNI specify an interface managed by failing Underlay resource).<br /> |
| `OverlayAttachmentFailed` | FailedResourceReasonOverlayAttachmentFailed provisioning failure at the logical network layer of the router<br />(e.g.: failed to create VRF, move interface to router namespace).<br /> |
| `FrrConfigurationFailed` | FailedResourceReasonFrrConfigurationFailed applying FRR configuration failed.<br /> |


#### GracefulRestartConfig



GracefulRestartConfig holds BGP Graceful Restart parameters.
Its presence on the Underlay enables graceful restart.



_Appears in:_
- [UnderlaySpec](#underlayspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `restartTimeSeconds` _integer_ | restartTimeSeconds is the time in seconds that the restarting router<br />requests its peers to preserve routes. Peers will wait this long<br />before removing stale routes. | 120 | Maximum: 4095 <br />Minimum: 1 <br />Optional: \{\} <br /> |
| `stalePathTimeSeconds` _integer_ | stalePathTimeSeconds is the time in seconds that stale paths from a<br />restarting peer are retained locally. | 360 | Maximum: 4095 <br />Minimum: 1 <br />Optional: \{\} <br /> |


#### HostMaster







_Appears in:_
- [L2VNISpec](#l2vnispec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `type` _string_ | type of the host interface. Supported values: "linux-bridge", "ovs-bridge". |  | Enum: [linux-bridge ovs-bridge] <br />Required: \{\} <br /> |
| `linuxBridge` _[LinuxBridgeConfig](#linuxbridgeconfig)_ | linuxBridge configuration. Must be set when Type is "linux-bridge". |  | Optional: \{\} <br /> |
| `ovsBridge` _[OVSBridgeConfig](#ovsbridgeconfig)_ | ovsBridge configuration. Must be set when Type is "ovs-bridge". |  | Optional: \{\} <br /> |


#### HostSession



Host Session represents the leg between the router and the host.
A BGP session is established over this leg.



_Appears in:_
- [L3PassthroughSpec](#l3passthroughspec)
- [L3VNISpec](#l3vnispec)
- [L3VPNSpec](#l3vpnspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `asn` _integer_ | asn is the local AS number to use to establish a BGP session with<br />the default namespace. |  | Maximum: 4.294967295e+09 <br />Minimum: 1 <br />Required: \{\} <br /> |
| `hostasn` _integer_ | hostasn is the expected AS number for a BGP speaking component running in<br />the default network namespace. Either HostASN or HostType must be set. |  | Maximum: 4.294967295e+09 <br />Minimum: 1 <br />Optional: \{\} <br /> |
| `hosttype` _string_ | hosttype is the AS type of the BGP speaking component running in the<br />default network namespace. Either HostASN or HostType must be set. |  | Enum: [external internal] <br />Optional: \{\} <br /> |
| `localcidr` _[LocalCIDRConfig](#localcidrconfig)_ | localcidr is the CIDR configuration for the veth pair<br />to connect with the default namespace. The interface under<br />the PERouter side is going to use the first IP of the cidr on all the nodes.<br />At least one of IPv4 or IPv6 must be provided. |  | Required: \{\} <br /> |


#### IPFamily

_Underlying type:_ _string_

IPFamily specifies which address families are enabled.

_Validation:_
- Enum: [ipv4 ipv6 dualstack]

_Appears in:_
- [ISISInterface](#isisinterface)

| Field | Description |
| --- | --- |
| `ipv4` |  |
| `ipv6` |  |
| `dualstack` |  |


#### ISISConfig



ISISConfig contains ISIS configuration for the underlay.



_Appears in:_
- [UnderlaySpec](#underlayspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `baseNet` _[ISISNet](#isisnet)_ | baseNet holds the ISIS NET address.<br />The configured Net address is a base address which is offset by the node index of each node.<br />Only accepts the simplified NSAP format with a fixed AreaID length of 3 bytes and a 6 byte SystemID in compliance<br />with the U.S. GOSIP version 2.0 for a total of 10 bytes. |  | MaxLength: 25 <br />MinLength: 25 <br />Required: \{\} <br /> |
| `features` _[ISISFeature](#isisfeature) array_ | features enables ISIS boolean features.<br />Supported features are:<br />advertisePassiveOnly: configures ISIS to advertise only prefixes that belong to passive interfaces. |  | Enum: [advertisePassiveOnly] <br />MaxItems: 32 <br />MaxLength: 128 <br />MinLength: 1 <br />Optional: \{\} <br /> |
| `interfaces` _[ISISInterface](#isisinterface) array_ | interfaces holds additional ISIS interface level configuration and / or per<br />interface overrides. By default, OpenPERouter enables IPv6 on all required<br />interfaces with default settings. |  | MaxItems: 128 <br />Optional: \{\} <br /> |
| `level` _integer_ | level configures the ISIS type, system wide. It defaults to level-1-2 unless specified otherwise. |  | Enum: [1 2] <br />Optional: \{\} <br /> |


#### ISISFeature

_Underlying type:_ _string_

ISISFeature represents a single ISIS feature.

_Validation:_
- Enum: [advertisePassiveOnly]
- MaxLength: 128
- MinLength: 1

_Appears in:_
- [ISISConfig](#isisconfig)



#### ISISInterface



ISISInterface holds ISIS interface level configuration.



_Appears in:_
- [ISISConfig](#isisconfig)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | name of the interface that these settings shall apply to. |  | MaxLength: 15 <br />MinLength: 1 <br />Required: \{\} <br /> |
| `ipFamily` _[IPFamily](#ipfamily)_ | ipFamily configures which address families ISIS is enabled for on this interface. |  | Enum: [ipv4 ipv6 dualstack] <br />Optional: \{\} <br /> |
| `features` _[ISISInterfaceFeature](#isisinterfacefeature) array_ | features enables ISIS interface boolean features.<br />Supported features are:<br />passive: configures ISIS passive mode on this interface. |  | Enum: [passive] <br />MaxItems: 32 <br />MaxLength: 128 <br />MinLength: 1 <br />Optional: \{\} <br /> |


#### ISISInterfaceFeature

_Underlying type:_ _string_

ISISInterfaceFeature represents a single ISIS feature of an ISIS interface.

_Validation:_
- Enum: [passive]
- MaxLength: 128
- MinLength: 1

_Appears in:_
- [ISISInterface](#isisinterface)



#### ISISNet

_Underlying type:_ _string_

ISISNet represents a single ISIS NET address.
Only accepts the simplified NSAP format with a fixed AreaID length of 3 bytes and a 6 byte SystemID in compliance
with the U.S. GOSIP version 2.0 for a total of 10 bytes.

_Validation:_
- MaxLength: 25
- MinLength: 25

_Appears in:_
- [ISISConfig](#isisconfig)



#### L2VNI



L2VNI represents a VXLan VNI to receive EVPN type 2 routes
from.





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `network.openperouter.io/v1alpha1` | | |
| `kind` _string_ | `L2VNI` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  | Optional: \{\} <br /> |
| `spec` _[L2VNISpec](#l2vnispec)_ | spec defines the desired state of L2VNI. |  | Required: \{\} <br /> |
| `status` _[L2VNIStatus](#l2vnistatus)_ | status defines the observed state of L2VNI. |  | Optional: \{\} <br /> |


#### L2VNISpec



L2VNISpec defines the desired state of VNI.



_Appears in:_
- [L2VNI](#l2vni)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `nodeSelector` _[LabelSelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v/#labelselector-v1-meta)_ | nodeSelector specifies which nodes this L2VNI applies to.<br />If empty or not specified, applies to all nodes.<br />Multiple L2VNIs can match the same node. |  | Optional: \{\} <br /> |
| `vrf` _string_ | vrf is the name of the linux VRF to be used inside the PERouter namespace.<br />The field is optional, if not set it the name of the VNI instance will be used. |  | MaxLength: 15 <br />Pattern: `^[a-zA-Z][a-zA-Z0-9_-]*$` <br />Optional: \{\} <br /> |
| `vni` _integer_ | vni is the VXLan VNI to be used |  | Maximum: 1.6777215e+07 <br />Minimum: 1 <br />Required: \{\} <br /> |
| `vxlanport` _integer_ | vxlanport is the port to be used for VXLan encapsulation. | 4789 | Optional: \{\} <br /> |
| `underlayAddressFamily` _string_ | underlayAddressFamily selects which VTEP address family to use for this VNI's<br />VXLAN interface. When omitted, defaults to the available family in the underlay<br />(IPv4 preferred in dual-stack). |  | Enum: [ipv4 ipv6] <br />Optional: \{\} <br /> |
| `hostmaster` _[HostMaster](#hostmaster)_ | hostmaster is the interface on the host the veth should be enslaved to.<br />If not set, the host veth will not be enslaved to any interface and it must be<br />enslaved manually (or by some other means). This is useful if another controller<br />is leveraging the host interface for the VNI. |  | Optional: \{\} <br /> |
| `l2gatewayips` _string array_ | l2gatewayips is a list of IP addresses in CIDR notation to be used for the L2 gateway. When this is set, the<br />bridge the veths are enslaved to will be configured with these IP addresses, effectively<br />acting as a distributed gateway for the VNI. This allows for dual-stack (IPv4 and IPv6) support.<br />Maximum of 2 addresses are allowed. If 2 addresses are provided, one must be IPv4 and one must be IPv6. |  | MaxItems: 2 <br />Optional: \{\} <br /> |


#### L2VNIStatus



VNIStatus defines the observed state of VNI.



_Appears in:_
- [L2VNI](#l2vni)



#### L3Passthrough



L3Passthrough represents a session with the host which is not encapsulated and
takes part to the bgp fabric.





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `network.openperouter.io/v1alpha1` | | |
| `kind` _string_ | `L3Passthrough` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  | Optional: \{\} <br /> |
| `spec` _[L3PassthroughSpec](#l3passthroughspec)_ | spec defines the desired state of L3Passthrough. |  | Required: \{\} <br /> |
| `status` _[L3PassthroughStatus](#l3passthroughstatus)_ | status defines the observed state of L3Passthrough. |  | Optional: \{\} <br /> |


#### L3PassthroughSpec







_Appears in:_
- [L3Passthrough](#l3passthrough)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `nodeSelector` _[LabelSelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v/#labelselector-v1-meta)_ | nodeSelector specifies which nodes this L3Passthrough applies to.<br />If empty or not specified, applies to all nodes.<br />Multiple L3Passthrough with overlapping node selectors will be rejected. |  | Optional: \{\} <br /> |
| `hostsession` _[HostSession](#hostsession)_ | hostsession is the configuration for the host session. |  | Required: \{\} <br /> |


#### L3PassthroughStatus



L3PassthroughStatus defines the observed state of L3Passthrough.



_Appears in:_
- [L3Passthrough](#l3passthrough)



#### L3VNI



L3VNI represents a VXLan L3VNI to receive EVPN type 5 routes
from.





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `network.openperouter.io/v1alpha1` | | |
| `kind` _string_ | `L3VNI` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  | Optional: \{\} <br /> |
| `spec` _[L3VNISpec](#l3vnispec)_ | spec defines the desired state of L3VNI. |  | Required: \{\} <br /> |
| `status` _[L3VNIStatus](#l3vnistatus)_ | status defines the observed state of L3VNI. |  | Optional: \{\} <br /> |


#### L3VNISpec



L3VNISpec defines the desired state of VNI.



_Appears in:_
- [L3VNI](#l3vni)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `nodeSelector` _[LabelSelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v/#labelselector-v1-meta)_ | nodeSelector specifies which nodes this L3VNI applies to.<br />If empty or not specified, applies to all nodes.<br />Multiple L3VNIs can match the same node. |  | Optional: \{\} <br /> |
| `vrf` _string_ | vrf is the name of the linux VRF to be used inside the PERouter namespace. |  | MaxLength: 15 <br />MinLength: 1 <br />Pattern: `^[a-zA-Z][a-zA-Z0-9_-]*$` <br />Required: \{\} <br /> |
| `vni` _integer_ | vni is the VXLan VNI to be used |  | Maximum: 1.6777215e+07 <br />Minimum: 1 <br />Required: \{\} <br /> |
| `vxlanport` _integer_ | vxlanport is the port to be used for VXLan encapsulation. | 4789 | Optional: \{\} <br /> |
| `underlayAddressFamily` _string_ | underlayAddressFamily selects which VTEP address family to use for this VNI's<br />VXLAN interface. When omitted, defaults to the available family in the underlay<br />(IPv4 preferred in dual-stack). |  | Enum: [ipv4 ipv6] <br />Optional: \{\} <br /> |
| `hostsession` _[HostSession](#hostsession)_ | hostsession is the configuration for the host session. |  | Optional: \{\} <br /> |
| `exportRTs` _[RouteTarget](#routetarget) array_ | exportRTs are the Route Targets to be used for exporting routes.<br />RouteTarget defines a BGP Extended Community for route filtering. |  | MaxItems: 100 <br />MaxLength: 21 <br />Optional: \{\} <br /> |
| `importRTs` _[RouteTarget](#routetarget) array_ | importRTs are the Route Targets to be used for importing routes.<br />RouteTarget defines a BGP Extended Community for route filtering. |  | MaxItems: 100 <br />MaxLength: 21 <br />Optional: \{\} <br /> |


#### L3VNIStatus



L3VNIStatus defines the observed state of L3VNI.



_Appears in:_
- [L3VNI](#l3vni)



#### L3VPN



L3VPN represents an SRv6 IP VPN.





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `network.openperouter.io/v1alpha1` | | |
| `kind` _string_ | `L3VPN` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  | Optional: \{\} <br /> |
| `spec` _[L3VPNSpec](#l3vpnspec)_ | spec defines the desired state of L3VPN. |  | Required: \{\} <br /> |
| `status` _[L3VPNStatus](#l3vpnstatus)_ | status defines the observed state of L3VPN. |  | Optional: \{\} <br /> |


#### L3VPNSpec



L3VPNSpec defines the desired state of L3VPN.



_Appears in:_
- [L3VPN](#l3vpn)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `nodeSelector` _[LabelSelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v/#labelselector-v1-meta)_ | nodeSelector specifies which nodes this L3VPN applies to.<br />If empty or not specified, applies to all nodes.<br />Multiple L3VPNs can match the same node. |  | Optional: \{\} <br /> |
| `vrf` _string_ | vrf is the name of the linux VRF to be used inside the PERouter namespace. |  | MaxLength: 15 <br />MinLength: 1 <br />Pattern: `^[a-zA-Z][a-zA-Z0-9_-]*$` <br />Required: \{\} <br /> |
| `exportRTs` _[RouteTarget](#routetarget) array_ | exportRTs are the Route Targets to be used for exporting routes.<br />If no exportRTs are provided, defaults to single export Route Target<br /><asn>:<rdAssignedNumber>. |  | MaxItems: 100 <br />MaxLength: 21 <br />Optional: \{\} <br /> |
| `importRTs` _[RouteTarget](#routetarget) array_ | importRTs are the Route Targets to be used for importing routes.<br />importRTs must always be provided explicitly. |  | MaxItems: 100 <br />MaxLength: 21 <br />Required: \{\} <br /> |
| `rdAssignedNumber` _integer_ | rdAssignedNumber sets the Route Distinguisher's Assigned Number subfield.<br />The Administrator subfield is automatically set to the value of the router<br />ID. OpenPERouter uses Type 1 Route Distinguishers as defined in RFC4364,<br />meaning <Administrator subfield>:<Assigned Number subfield>. |  | Maximum: 65535 <br />Minimum: 1 <br />Required: \{\} <br /> |
| `hostsession` _[HostSession](#hostsession)_ | hostsession is the configuration for the host session. |  | Optional: \{\} <br /> |


#### L3VPNStatus



L3VPNStatus defines the observed state of L3VPN.



_Appears in:_
- [L3VPN](#l3vpn)



#### LinuxBridgeConfig



LinuxBridgeConfig contains configuration for Linux bridge type.



_Appears in:_
- [HostMaster](#hostmaster)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | name of the Linux bridge interface. |  | MaxLength: 15 <br />Pattern: `^[a-zA-Z][a-zA-Z0-9_-]*$` <br />Optional: \{\} <br /> |
| `autoCreate` _boolean_ | autoCreate determines if the bridge should be created automatically.<br />When true, the bridge is created with name br-hs-<VNI>. | false | Optional: \{\} <br /> |


#### LocalCIDRConfig







_Appears in:_
- [HostSession](#hostsession)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `ipv4` _string_ | ipv4 is the IPv4 CIDR to be used for the veth pair<br />to connect with the default namespace. The interface under<br />the PERouter side is going to use the first IP of the cidr on all the nodes. |  | Optional: \{\} <br /> |
| `ipv6` _string_ | ipv6 is the IPv6 CIDR to be used for the veth pair<br />to connect with the default namespace. The interface under<br />the PERouter side is going to use the first IP of the cidr on all the nodes. |  | Optional: \{\} <br /> |


#### Neighbor



Neighbor represents a BGP Neighbor we want FRR to connect to.



_Appears in:_
- [UnderlaySpec](#underlayspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `asn` _integer_ | asn is the AS number of the neighbor. Either ASN or Type must be set. |  | Maximum: 4.294967295e+09 <br />Minimum: 1 <br />Optional: \{\} <br /> |
| `type` _string_ | type is the AS type of the neighbor. Either ASN or Type must be set. |  | Enum: [external internal] <br />Optional: \{\} <br /> |
| `address` _string_ | address is the IP address to establish the session with. The IP address<br />can be either IPv4 or IPv6. |  | MaxLength: 39 <br />MinLength: 1 <br />Optional: \{\} <br /> |
| `interface` _string_ | interface is the interface name for BGP unnumbered sessions. The session will be established via IPv6 link locals. |  | MaxLength: 15 <br />MinLength: 1 <br />Optional: \{\} <br /> |
| `port` _integer_ | port is the port to dial when establishing the session.<br />Defaults to 179. |  | Maximum: 16384 <br />Minimum: 0 <br />Optional: \{\} <br /> |
| `password` _string_ | password to be used for establishing the BGP session.<br />Password and PasswordSecret are mutually exclusive. |  | Optional: \{\} <br /> |
| `passwordSecret` _string_ | passwordSecret is name of the authentication secret for the neighbor.<br />the secret must be of type "kubernetes.io/basic-auth", and created in the<br />same namespace as the perouter daemon. The password is stored in the<br />secret as the key "password".<br />Password and PasswordSecret are mutually exclusive. |  | Optional: \{\} <br /> |
| `holdTimeSeconds` _integer_ | holdTimeSeconds is the requested BGP hold time in seconds, per RFC4271.<br />Defaults to 180. |  | Optional: \{\} <br /> |
| `keepaliveTimeSeconds` _integer_ | keepaliveTimeSeconds is the requested BGP keepalive time in seconds, per RFC4271.<br />Defaults to 60. |  | Optional: \{\} <br /> |
| `connectTimeSeconds` _integer_ | connectTimeSeconds controls how long BGP waits between connection attempts to a neighbor, in seconds. |  | Maximum: 65535 <br />Minimum: 1 <br />Optional: \{\} <br /> |
| `ebgpMultiHop` _boolean_ | ebgpMultiHop indicates if the BGPPeer is multi-hops away. |  | Optional: \{\} <br /> |
| `bfd` _[BFDSettings](#bfdsettings)_ | bfd defines the BFD configuration for the BGP session. |  | Optional: \{\} <br /> |
| `addressFamilies` _[NeighborAddressFamily](#neighboraddressfamily) array_ | addressFamilies specifies the BGP address families that shall be enabled<br />for this BGP neighbor. evpn and ipv4vpn/ipv6vpn are mutually exclusive.<br />If ipv4vpn or ipv6vpn are set, the update source of this neighbor will<br />be set to the loopback's IPv6 address.<br />If addressFamilies is not provided or empty, the following defaults are<br />chosen:<br />For unnumbered neighbors:<br />- ipv4unicast<br />- ipv6unicast if passthrough is configured with IPv6 local CIDR<br />- evpn if L2VNIs or L3VNIs are present.<br />For IPv4 neighbors:<br />- ipv4unicast<br />- ipv6unicast if passthrough is configured with IPv6 local CIDR<br />- evpn if L2VNIs or L3VNIs are present.<br />For IPv6 neighbors:<br />- ipv4unicast if L2VNIs or L3VNIs are present, or if passthrough is configured with IPv4 local CIDR<br />- ipv6unicast<br />- evpn if L2VNIs or L3VNIs are present<br />- ipv4vpn if L3VPNs and SRv6 configuration are present.<br />- ipv6vpn if L3VPNs and SRv6 configuration are present. |  | MaxItems: 4 <br />Optional: \{\} <br /> |


#### NeighborAddressFamily



NeighborAddressFamily represents a single BGP address family configuration
for a neighbor.



_Appears in:_
- [Neighbor](#neighbor)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `type` _string_ | type is the address family type. |  | Enum: [ipv4unicast ipv6unicast evpn ipv4vpn ipv6vpn] <br />MaxLength: 11 <br />MinLength: 1 <br />Required: \{\} <br /> |


#### NetworkDevice



NetworkDevice moves an existing host network device into the router netns.



_Appears in:_
- [UnderlayInterface](#underlayinterface)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `interfaceName` _string_ | interfaceName is the name of the host network device to move into<br />the router netns. |  | MaxLength: 15 <br />MinLength: 1 <br />Pattern: `^[a-zA-Z][a-zA-Z0-9._-]*$` <br />Required: \{\} <br /> |


#### OVSBridgeConfig



OVSBridgeConfig contains configuration for OVS bridge type.



_Appears in:_
- [HostMaster](#hostmaster)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | name of the OVS bridge interface. |  | MaxLength: 15 <br />Pattern: `^[a-zA-Z][a-zA-Z0-9_-]*$` <br />Optional: \{\} <br /> |
| `autoCreate` _boolean_ | autoCreate determines if the OVS bridge should be created automatically.<br />When true, the bridge is created with name br-hs-<VNI>. | false | Optional: \{\} <br /> |


#### RawFRRConfig



RawFRRConfig is the Schema for the rawfrrconfigs API.





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `network.openperouter.io/v1alpha1` | | |
| `kind` _string_ | `RawFRRConfig` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  | Optional: \{\} <br /> |
| `spec` _[RawFRRConfigSpec](#rawfrrconfigspec)_ | spec defines the desired state of RawFRRConfig. |  | Required: \{\} <br /> |
| `status` _[RawFRRConfigStatus](#rawfrrconfigstatus)_ | status defines the observed state of RawFRRConfig. |  | Optional: \{\} <br /> |


#### RawFRRConfigSpec



RawFRRConfigSpec defines the desired state of RawFRRConfig.



_Appears in:_
- [RawFRRConfig](#rawfrrconfig)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `nodeSelector` _[LabelSelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v/#labelselector-v1-meta)_ | nodeSelector specifies which nodes this RawFRRConfig applies to.<br />If empty or not specified, applies to all nodes. |  | Optional: \{\} <br /> |
| `priority` _integer_ | priority controls the ordering of raw config snippets in the rendered FRR configuration.<br />Lower values are rendered first. Snippets with the same priority have undefined order. | 0 | Minimum: 0 <br />Optional: \{\} <br /> |
| `rawConfig` _string_ | rawConfig is the raw FRR configuration text to append to the rendered configuration.<br />WARNING: This feature is intended for advanced use cases. No validation of FRR syntax<br />is performed at admission time; invalid configuration will cause FRR reload failures. |  | MinLength: 1 <br />Required: \{\} <br /> |


#### RawFRRConfigStatus



RawFRRConfigStatus defines the observed state of RawFRRConfig.



_Appears in:_
- [RawFRRConfig](#rawfrrconfig)



#### RouteTarget

_Underlying type:_ _string_

RouteTarget defines a BGP Extended Community for route filtering.

_Validation:_
- MaxLength: 21

_Appears in:_
- [L3VNISpec](#l3vnispec)
- [L3VPNSpec](#l3vpnspec)



#### RouterNodeConfigurationStatus



RouterNodeConfigurationStatus describes a node router state.





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `network.openperouter.io/v1alpha1` | | |
| `kind` _string_ | `RouterNodeConfigurationStatus` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `status` _[RouterNodeConfigurationStatusStatus](#routernodeconfigurationstatusstatus)_ | status node router configuration status. |  | Optional: \{\} <br /> |


#### RouterNodeConfigurationStatusStatus







_Appears in:_
- [RouterNodeConfigurationStatus](#routernodeconfigurationstatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `failedResources` _[FailedResource](#failedresource) array_ | failedResources list of failed configuration resources on the node. |  | Optional: \{\} <br /> |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v/#condition-v1-meta) array_ | conditions list of conditions. |  | Optional: \{\} <br /> |


#### SRV6Config



SRV6Config contains SRV6 configuration for the underlay.



_Appears in:_
- [UnderlaySpec](#underlayspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `locator` _[SRV6Locator](#srv6locator)_ | locator defines the locator for this SRv6 VPN. |  | Required: \{\} <br /> |


#### SRV6Locator



SRV6Locator holds the configuration of a locator for SRv6.



_Appears in:_
- [SRV6Config](#srv6config)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `basePrefix` _string_ | basePrefix is the CIDR to be used for the locator, offset by the router index. |  | MaxLength: 43 <br />MinLength: 1 <br />Required: \{\} <br /> |
| `format` _string_ | format specifies the format of the locator. Defaults to usid-f3216 |  | Enum: [usid-f3216] <br />MaxLength: 40 <br />MinLength: 1 <br />Required: \{\} <br /> |


#### TunnelEndpointConfig



TunnelEndpointConfig contains tunnel endpoint configuration for the underlay.



_Appears in:_
- [UnderlaySpec](#underlayspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `cidrs` _string array_ | cidrs is a list of CIDRs to be used to assign IPs to the local tunnel endpoint on<br />each node. IPs derived from these CIDRs will be assigned to the local loopback.<br />At least one IPv4 or IPv6 CIDR is required. At most one of each family may be specified. |  | MaxItems: 2 <br />MinItems: 1 <br />Required: \{\} <br /> |


#### Underlay



Underlay is the Schema for the underlays API.





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `network.openperouter.io/v1alpha1` | | |
| `kind` _string_ | `Underlay` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  | Optional: \{\} <br /> |
| `spec` _[UnderlaySpec](#underlayspec)_ | spec defines the desired state of Underlay. |  | Required: \{\} <br /> |
| `status` _[UnderlayStatus](#underlaystatus)_ | status defines the observed state of Underlay. |  | Optional: \{\} <br /> |


#### UnderlayInterface



UnderlayInterface defines how the router obtains a single underlay link.
Exactly one of the sub-structs must match the type field.
The union is designed to be extended with future modes
for controller-provisioned interfaces.



_Appears in:_
- [UnderlaySpec](#underlayspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `type` _[UnderlayInterfaceType](#underlayinterfacetype)_ | type selects how the router obtains this underlay link. |  | Enum: [NetworkDevice CNIDevice] <br />Required: \{\} <br /> |
| `networkDevice` _[NetworkDevice](#networkdevice)_ | networkDevice moves an existing host network device into the router netns.<br />The device can be of any kind (physical NIC, bridge, macvlan, etc.).<br />Must be set when type is "NetworkDevice". |  | Optional: \{\} <br /> |
| `cniDevice` _[CNIDevice](#cnidevice)_ | cniDevice invokes a CNI plugin to provision an interface in the router<br />netns. IPAM is delegated to the CNI plugin. Must be set when type is<br />"CNIDevice". |  | Optional: \{\} <br /> |


#### UnderlayInterfaceType

_Underlying type:_ _string_

UnderlayInterfaceType selects how the router obtains an underlay link.
It is the discriminator of the UnderlayInterface union and is designed to be
extended with future modes.

_Validation:_
- Enum: [NetworkDevice CNIDevice]

_Appears in:_
- [UnderlayInterface](#underlayinterface)

| Field | Description |
| --- | --- |
| `NetworkDevice` | UnderlayInterfaceTypeNetworkDevice moves an existing host network device<br />into the router netns.<br /> |
| `CNIDevice` | UnderlayInterfaceTypeCNIDevice invokes a CNI plugin to provision an interface<br />in the router netns.<br /> |


#### UnderlaySpec



UnderlaySpec defines the desired state of Underlay.



_Appears in:_
- [Underlay](#underlay)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `nodeSelector` _[LabelSelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v/#labelselector-v1-meta)_ | nodeSelector specifies which nodes this Underlay applies to.<br />If empty or not specified, applies to all nodes (backward compatible).<br />Multiple Underlays with overlapping node selectors will be rejected. |  | Optional: \{\} <br /> |
| `asn` _integer_ | asn is the local AS number to use for the session with the TOR switch. |  | Maximum: 4.294967295e+09 <br />Minimum: 1 <br />Required: \{\} <br /> |
| `routeridcidr` _string_ | routeridcidr is the ipv4 cidr to be used to assign a different routerID on each node. | 10.0.0.0/24 | Optional: \{\} <br /> |
| `neighbors` _[Neighbor](#neighbor) array_ | neighbors is the list of external BGP neighbors to peer with.<br />Multiple neighbors are supported for connecting to multiple TOR switches<br />or establishing redundant BGP sessions. Each neighbor address must be unique.<br />At least one neighbor is required. |  | MaxItems: 128 <br />MinItems: 1 <br />Required: \{\} <br /> |
| `interfaces` _[UnderlayInterface](#underlayinterface) array_ | interfaces is the list of interfaces the router uses for underlay<br />connectivity. Each entry is a discriminated union describing how the<br />interface is obtained. At least one interface is required. All the<br />entries must be of the same type: mixing NetworkDevice and CNIDevice<br />interfaces is not supported. |  | MinItems: 1 <br />Required: \{\} <br /> |
| `tunnelEndpoint` _[TunnelEndpointConfig](#tunnelendpointconfig)_ | tunnelEndpoint contains tunnel endpoint configuration for the underlay. |  | Optional: \{\} <br /> |
| `gracefulRestart` _[GracefulRestartConfig](#gracefulrestartconfig)_ | gracefulRestart configures BGP Graceful Restart behaviour.<br />When set, FRR advertises GR capability and preserves forwarding<br />state across restarts so that peers keep stale routes active.<br />Omit to disable graceful restart. |  | Optional: \{\} <br /> |
| `isis` _[ISISConfig](#isisconfig)_ | isis holds the ISIS configuration for the underlay. |  | Optional: \{\} <br /> |
| `srv6` _[SRV6Config](#srv6config)_ | srv6 holds the SRv6 configuration. Requires ISIS or Neighbors configuration. |  | Optional: \{\} <br /> |


#### UnderlayStatus



UnderlayStatus defines the observed state of Underlay.



_Appears in:_
- [Underlay](#underlay)



