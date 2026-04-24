# API Reference

## Packages
- [openpe.openperouter.github.io/v1alpha1](#openpeopenperoutergithubiov1alpha1)


## openpe.openperouter.github.io/v1alpha1

Package v1alpha1 contains API Schema definitions for the openpe v1alpha1 API group.

### Resource Types
- [L2VNI](#l2vni)
- [L3Passthrough](#l3passthrough)
- [L3VNI](#l3vni)
- [RawFRRConfig](#rawfrrconfig)
- [Underlay](#underlay)



#### BFDSettings



BFDSettings defines the BFD configuration for a BGP session.



_Appears in:_
- [Neighbor](#neighbor)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `receiveInterval` _integer_ | The minimum interval that this system is capable of<br />receiving control packets in milliseconds.<br />Defaults to 300ms. |  | Maximum: 60000 <br />Minimum: 10 <br />Optional: \{\} <br /> |
| `transmitInterval` _integer_ | The minimum transmission interval (less jitter)<br />that this system wants to use to send BFD control packets in<br />milliseconds. Defaults to 300ms |  | Maximum: 60000 <br />Minimum: 10 <br />Optional: \{\} <br /> |
| `detectMultiplier` _integer_ | Configures the detection multiplier to determine<br />packet loss. The remote transmission interval will be multiplied<br />by this value to determine the connection loss detection timer. |  | Maximum: 255 <br />Minimum: 2 <br />Optional: \{\} <br /> |
| `echoInterval` _integer_ | Configures the minimal echo receive transmission<br />interval that this system is capable of handling in milliseconds.<br />Defaults to 50ms |  | Maximum: 60000 <br />Minimum: 10 <br />Optional: \{\} <br /> |
| `echoMode` _boolean_ | Enables or disables the echo transmission mode.<br />This mode is disabled by default, and not supported on multi<br />hops setups. |  | Optional: \{\} <br /> |
| `passiveMode` _boolean_ | Mark session as passive: a passive session will not<br />attempt to start the connection and will wait for control packets<br />from peer before it begins replying. |  | Optional: \{\} <br /> |
| `minimumTtl` _integer_ | For multi hop sessions only: configure the minimum<br />expected TTL for an incoming BFD control packet. |  | Maximum: 254 <br />Minimum: 1 <br />Optional: \{\} <br /> |


#### EVPNConfig



EVPNConfig contains EVPN-VXLAN configuration for the underlay.



_Appears in:_
- [UnderlaySpec](#underlayspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `vtepcidr` _string_ | VTEPCIDR is CIDR to be used to assign IPs to the local VTEP on each node.<br />A loopback interface will be created with an IP derived from this CIDR.<br />Mutually exclusive with vtepInterface. |  | Optional: \{\} <br /> |
| `vtepInterface` _string_ | VTEPInterface is the name of an existing interface to use as the VTEP source.<br />The interface must already have an IP address configured that will be used<br />as the VTEP IP. Mutually exclusive with vtepcidr.<br />The ToR must advertise the interface IP into the fabric underlay<br />(e.g. via redistribute connected) so that the VTEP address is reachable<br />from other leaves. |  | MaxLength: 15 <br />Pattern: `^[a-zA-Z][a-zA-Z0-9._-]*$` <br />Optional: \{\} <br /> |


#### GracefulRestartConfig



GracefulRestartConfig holds BGP Graceful Restart parameters.
Its presence on the Underlay enables graceful restart.



_Appears in:_
- [UnderlaySpec](#underlayspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `restartTime` _integer_ | RestartTime is the time in seconds that the restarting router<br />requests its peers to preserve routes. Peers will wait this long<br />before removing stale routes. | 120 | Maximum: 4095 <br />Minimum: 1 <br />Optional: \{\} <br /> |
| `stalePathTime` _integer_ | StalePathTime is the time in seconds that stale paths from a<br />restarting peer are retained locally. | 360 | Maximum: 4095 <br />Minimum: 1 <br />Optional: \{\} <br /> |


#### HostMaster







_Appears in:_
- [L2VNISpec](#l2vnispec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `type` _string_ | Type of the host interface. Supported values: "linux-bridge", "ovs-bridge". |  | Enum: [linux-bridge ovs-bridge] <br />Required: \{\} <br /> |
| `linuxBridge` _[LinuxBridgeConfig](#linuxbridgeconfig)_ | LinuxBridge configuration. Must be set when Type is "linux-bridge". |  | Optional: \{\} <br /> |
| `ovsBridge` _[OVSBridgeConfig](#ovsbridgeconfig)_ | OVSBridge configuration. Must be set when Type is "ovs-bridge". |  | Optional: \{\} <br /> |


#### HostSession



Host Session represents the leg between the router and the host.
A BGP session is established over this leg.



_Appears in:_
- [L3PassthroughSpec](#l3passthroughspec)
- [L3VNISpec](#l3vnispec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `asn` _integer_ | ASN is the local AS number to use to establish a BGP session with<br />the default namespace. |  | Maximum: 4.294967295e+09 <br />Minimum: 1 <br />Required: \{\} <br /> |
| `hostasn` _integer_ | HostASN is the expected AS number for a BGP speaking component running in<br />the default network namespace. Either HostASN or HostType must be set. |  | Maximum: 4.294967295e+09 <br />Minimum: 1 <br />Optional: \{\} <br /> |
| `hosttype` _string_ | HostType is the AS type of the BGP speaking component running in the<br />default network namespace. Either HostASN or HostType must be set. |  | Enum: [external internal] <br />Optional: \{\} <br /> |
| `localcidr` _[LocalCIDRConfig](#localcidrconfig)_ | LocalCIDR is the CIDR configuration for the veth pair<br />to connect with the default namespace. The interface under<br />the PERouter side is going to use the first IP of the cidr on all the nodes.<br />At least one of IPv4 or IPv6 must be provided. |  | Required: \{\} <br /> |


#### L2VNI



L2VNI represents a VXLan VNI to receive EVPN type 2 routes
from.





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `openpe.openperouter.github.io/v1alpha1` | | |
| `kind` _string_ | `L2VNI` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[L2VNISpec](#l2vnispec)_ |  |  |  |
| `status` _[L2VNIStatus](#l2vnistatus)_ |  |  |  |


#### L2VNISpec



L2VNISpec defines the desired state of VNI.



_Appears in:_
- [L2VNI](#l2vni)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `nodeSelector` _[LabelSelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v/#labelselector-v1-meta)_ | NodeSelector specifies which nodes this L2VNI applies to.<br />If empty or not specified, applies to all nodes.<br />Multiple L2VNIs can match the same node. |  | Optional: \{\} <br /> |
| `vrf` _string_ | VRF is the name of the linux VRF to be used inside the PERouter namespace.<br />The field is optional, if not set it the name of the VNI instance will be used. |  | MaxLength: 15 <br />Pattern: `^[a-zA-Z][a-zA-Z0-9_-]*$` <br />Optional: \{\} <br /> |
| `vni` _integer_ | VNI is the VXLan VNI to be used |  | Maximum: 4.294967295e+09 <br />Minimum: 0 <br /> |
| `vxlanport` _integer_ | VXLanPort is the port to be used for VXLan encapsulation. | 4789 |  |
| `hostmaster` _[HostMaster](#hostmaster)_ | HostMaster is the interface on the host the veth should be enslaved to.<br />If not set, the host veth will not be enslaved to any interface and it must be<br />enslaved manually (or by some other means). This is useful if another controller<br />is leveraging the host interface for the VNI. |  | Optional: \{\} <br /> |
| `l2gatewayips` _string array_ | L2GatewayIPs is a list of IP addresses in CIDR notation to be used for the L2 gateway. When this is set, the<br />bridge the veths are enslaved to will be configured with these IP addresses, effectively<br />acting as a distributed gateway for the VNI. This allows for dual-stack (IPv4 and IPv6) support.<br />Maximum of 2 addresses are allowed. If 2 addresses are provided, one must be IPv4 and one must be IPv6. |  | MaxItems: 2 <br />Optional: \{\} <br /> |


#### L2VNIStatus



VNIStatus defines the observed state of VNI.



_Appears in:_
- [L2VNI](#l2vni)



#### L3Passthrough



L3Passthrough represents a session with the host which is not encapsulated and
takes part to the bgp fabric.





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `openpe.openperouter.github.io/v1alpha1` | | |
| `kind` _string_ | `L3Passthrough` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[L3PassthroughSpec](#l3passthroughspec)_ |  |  |  |
| `status` _[L3PassthroughStatus](#l3passthroughstatus)_ |  |  |  |


#### L3PassthroughSpec







_Appears in:_
- [L3Passthrough](#l3passthrough)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `nodeSelector` _[LabelSelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v/#labelselector-v1-meta)_ | NodeSelector specifies which nodes this L3Passthrough applies to.<br />If empty or not specified, applies to all nodes.<br />Multiple L3Passthrough with overlapping node selectors will be rejected. |  | Optional: \{\} <br /> |
| `hostsession` _[HostSession](#hostsession)_ | HostSession is the configuration for the host session. |  |  |


#### L3PassthroughStatus



L3PassthroughStatus defines the observed state of L3Passthrough.



_Appears in:_
- [L3Passthrough](#l3passthrough)



#### L3VNI



L3VNI represents a VXLan L3VNI to receive EVPN type 5 routes
from.





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `openpe.openperouter.github.io/v1alpha1` | | |
| `kind` _string_ | `L3VNI` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[L3VNISpec](#l3vnispec)_ |  |  |  |
| `status` _[L3VNIStatus](#l3vnistatus)_ |  |  |  |


#### L3VNISpec



L3VNISpec defines the desired state of VNI.



_Appears in:_
- [L3VNI](#l3vni)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `nodeSelector` _[LabelSelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v/#labelselector-v1-meta)_ | NodeSelector specifies which nodes this L3VNI applies to.<br />If empty or not specified, applies to all nodes.<br />Multiple L3VNIs can match the same node. |  | Optional: \{\} <br /> |
| `vrf` _string_ | VRF is the name of the linux VRF to be used inside the PERouter namespace. |  | MaxLength: 15 <br />Pattern: `^[a-zA-Z][a-zA-Z0-9_-]*$` <br />Required: \{\} <br /> |
| `vni` _integer_ | VNI is the VXLan VNI to be used |  | Maximum: 4.294967295e+09 <br />Minimum: 0 <br />Optional: \{\} <br /> |
| `vxlanport` _integer_ | VXLanPort is the port to be used for VXLan encapsulation. | 4789 |  |
| `hostsession` _[HostSession](#hostsession)_ | HostSession is the configuration for the host session. |  | Optional: \{\} <br /> |
| `exportRTs` _string array_ | ExportRTs are the Route Targets to be used for exporting routes.<br />RouteTarget defines a BGP Extended Community for route filtering. |  | Optional: \{\} <br /> |
| `importRTs` _string array_ | ImportRTs are the Route Targets to be used for importing routes.<br />RouteTarget defines a BGP Extended Community for route filtering. |  | Optional: \{\} <br /> |


#### L3VNIStatus



L3VNIStatus defines the observed state of L3VNI.



_Appears in:_
- [L3VNI](#l3vni)



#### LinuxBridgeConfig



LinuxBridgeConfig contains configuration for Linux bridge type.



_Appears in:_
- [HostMaster](#hostmaster)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | Name of the Linux bridge interface. |  | MaxLength: 15 <br />Pattern: `^[a-zA-Z][a-zA-Z0-9_-]*$` <br />Optional: \{\} <br /> |
| `autoCreate` _boolean_ | AutoCreate determines if the bridge should be created automatically.<br />When true, the bridge is created with name br-hs-<VNI>. | false | Optional: \{\} <br /> |


#### LocalCIDRConfig







_Appears in:_
- [HostSession](#hostsession)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `ipv4` _string_ | IPv4 is the IPv4 CIDR to be used for the veth pair<br />to connect with the default namespace. The interface under<br />the PERouter side is going to use the first IP of the cidr on all the nodes. |  | Optional: \{\} <br /> |
| `ipv6` _string_ | IPv6 is the IPv6 CIDR to be used for the veth pair<br />to connect with the default namespace. The interface under<br />the PERouter side is going to use the first IP of the cidr on all the nodes. |  | Optional: \{\} <br /> |


#### Neighbor



Neighbor represents a BGP Neighbor we want FRR to connect to.



_Appears in:_
- [UnderlaySpec](#underlayspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `asn` _integer_ | ASN is the AS number of the neighbor. Either ASN or Type must be set. |  | Maximum: 4.294967295e+09 <br />Minimum: 1 <br />Optional: \{\} <br /> |
| `type` _string_ | Type is the AS type of the neighbor. Either ASN or Type must be set. |  | Enum: [external internal] <br />Optional: \{\} <br /> |
| `address` _string_ | Address is the IP address to establish the session with. |  |  |
| `port` _integer_ | Port is the port to dial when establishing the session.<br />Defaults to 179. |  | Maximum: 16384 <br />Minimum: 0 <br />Optional: \{\} <br /> |
| `password` _string_ | Password to be used for establishing the BGP session.<br />Password and PasswordSecret are mutually exclusive. |  | Optional: \{\} <br /> |
| `passwordSecret` _string_ | PasswordSecret is name of the authentication secret for the neighbor.<br />the secret must be of type "kubernetes.io/basic-auth", and created in the<br />same namespace as the perouter daemon. The password is stored in the<br />secret as the key "password".<br />Password and PasswordSecret are mutually exclusive. |  | Optional: \{\} <br /> |
| `holdTime` _[Duration](https://kubernetes.io/docs/reference/generated/kubernetes-api/v/#duration-v1-meta)_ | HoldTime is the requested BGP hold time, per RFC4271.<br />Defaults to 180s. |  | Optional: \{\} <br /> |
| `keepaliveTime` _[Duration](https://kubernetes.io/docs/reference/generated/kubernetes-api/v/#duration-v1-meta)_ | KeepaliveTime is the requested BGP keepalive time, per RFC4271.<br />Defaults to 60s. |  | Optional: \{\} <br /> |
| `connectTime` _[Duration](https://kubernetes.io/docs/reference/generated/kubernetes-api/v/#duration-v1-meta)_ | Requested BGP connect time, controls how long BGP waits between connection attempts to a neighbor. |  | Optional: \{\} <br /> |
| `ebgpMultiHop` _boolean_ | EBGPMultiHop indicates if the BGPPeer is multi-hops away. |  | Optional: \{\} <br /> |
| `bfd` _[BFDSettings](#bfdsettings)_ | BFD defines the BFD configuration for the BGP session. |  | Optional: \{\} <br /> |


#### OVSBridgeConfig



OVSBridgeConfig contains configuration for OVS bridge type.



_Appears in:_
- [HostMaster](#hostmaster)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | Name of the OVS bridge interface. |  | MaxLength: 15 <br />Pattern: `^[a-zA-Z][a-zA-Z0-9_-]*$` <br />Optional: \{\} <br /> |
| `autoCreate` _boolean_ | AutoCreate determines if the OVS bridge should be created automatically.<br />When true, the bridge is created with name br-hs-<VNI>. | false | Optional: \{\} <br /> |


#### RawFRRConfig



RawFRRConfig is the Schema for the rawfrrconfigs API.





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `openpe.openperouter.github.io/v1alpha1` | | |
| `kind` _string_ | `RawFRRConfig` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[RawFRRConfigSpec](#rawfrrconfigspec)_ |  |  |  |
| `status` _[RawFRRConfigStatus](#rawfrrconfigstatus)_ |  |  |  |


#### RawFRRConfigSpec



RawFRRConfigSpec defines the desired state of RawFRRConfig.



_Appears in:_
- [RawFRRConfig](#rawfrrconfig)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `nodeSelector` _[LabelSelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v/#labelselector-v1-meta)_ | NodeSelector specifies which nodes this RawFRRConfig applies to.<br />If empty or not specified, applies to all nodes. |  | Optional: \{\} <br /> |
| `priority` _integer_ | Priority controls the ordering of raw config snippets in the rendered FRR configuration.<br />Lower values are rendered first. Snippets with the same priority have undefined order. | 0 | Minimum: 0 <br />Optional: \{\} <br /> |
| `rawConfig` _string_ | RawConfig is the raw FRR configuration text to append to the rendered configuration.<br />WARNING: This feature is intended for advanced use cases. No validation of FRR syntax<br />is performed at admission time; invalid configuration will cause FRR reload failures. |  | MinLength: 1 <br />Required: \{\} <br /> |


#### RawFRRConfigStatus



RawFRRConfigStatus defines the observed state of RawFRRConfig.



_Appears in:_
- [RawFRRConfig](#rawfrrconfig)



#### Underlay



Underlay is the Schema for the underlays API.





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `openpe.openperouter.github.io/v1alpha1` | | |
| `kind` _string_ | `Underlay` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[UnderlaySpec](#underlayspec)_ |  |  |  |
| `status` _[UnderlayStatus](#underlaystatus)_ |  |  |  |


#### UnderlaySpec



UnderlaySpec defines the desired state of Underlay.



_Appears in:_
- [Underlay](#underlay)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `nodeSelector` _[LabelSelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v/#labelselector-v1-meta)_ | NodeSelector specifies which nodes this Underlay applies to.<br />If empty or not specified, applies to all nodes (backward compatible).<br />Multiple Underlays with overlapping node selectors will be rejected. |  | Optional: \{\} <br /> |
| `asn` _integer_ | ASN is the local AS number to use for the session with the TOR switch. |  | Maximum: 4.294967295e+09 <br />Minimum: 1 <br />Required: \{\} <br /> |
| `routeridcidr` _string_ | RouterIDCIDR is the ipv4 cidr to be used to assign a different routerID on each node. | 10.0.0.0/24 | Optional: \{\} <br /> |
| `neighbors` _[Neighbor](#neighbor) array_ | Neighbors is the list of external neighbors to peer with. |  | MinItems: 1 <br /> |
| `nics` _string array_ | Nics is the list of physical nics to move under the PERouter namespace to connect<br />to external routers. This field is optional when using Multus networks for TOR connectivity. |  | items:MaxLength: 15 <br />items:Pattern: `^[a-zA-Z][a-zA-Z0-9._-]*$` <br /> |
| `evpn` _[EVPNConfig](#evpnconfig)_ |  |  |  |
| `gracefulRestart` _[GracefulRestartConfig](#gracefulrestartconfig)_ | GracefulRestart configures BGP Graceful Restart behaviour.<br />When set, FRR advertises GR capability and preserves forwarding<br />state across restarts so that peers keep stale routes active.<br />Omit to disable graceful restart. |  | Optional: \{\} <br /> |


#### UnderlayStatus



UnderlayStatus defines the observed state of Underlay.



_Appears in:_
- [Underlay](#underlay)



