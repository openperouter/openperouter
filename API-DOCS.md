# API Reference

## Packages
- [openpe.openperouter.github.io/v1alpha1](#openpeopenperoutergithubiov1alpha1)


## openpe.openperouter.github.io/v1alpha1

Package v1alpha1 contains API Schema definitions for the openpe v1alpha1 API group.

### Resource Types
- [L2VNI](#l2vni)
- [L3VNI](#l3vni)
- [Underlay](#underlay)



#### HostMaster







_Appears in:_
- [L2VNISpec](#l2vnispec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | Name of the host interface. Must match VRF name validation if set. |  | MaxLength: 15 <br />Pattern: `^[a-zA-Z][a-zA-Z0-9_-]*$` <br /> |
| `type` _string_ | Type of the host interface. Currently only "bridge" is supported. |  | Enum: [bridge] <br /> |
| `autocreate` _boolean_ | If true, the interface will be created automatically if not present.<br />The name of the bridge is of the form br-hs-<VNI>. | false |  |


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
| `vrf` _string_ | VRF is the name of the linux VRF to be used inside the PERouter namespace.<br />The field is optional, if not set it the name of the VNI instance will be used. |  | MaxLength: 15 <br />Pattern: `^[a-zA-Z][a-zA-Z0-9_-]*$` <br /> |
| `vni` _integer_ | VNI is the VXLan VNI to be used |  | Maximum: 4.294967295e+09 <br />Minimum: 0 <br /> |
| `vxlanport` _integer_ | VXLanPort is the port to be used for VXLan encapsulation. | 4789 |  |
| `hostmaster` _[HostMaster](#hostmaster)_ | HostMaster is the interface on the host the veth should be enslaved to.<br />If not set, the host veth will not be enslaved to any interface and it must be<br />enslaved manually (or by some other means). This is useful if another controller<br />is leveraging the host interface for the VNI. |  |  |
| `l2gatewayip` _string_ | L2GatewayIP is the IP address to be used for the L2 gateway. When this is set, the<br />bridge the veths are enslaved to will be configured with this IP address, effectively<br />acting as a distributed gateway for the VNI. |  |  |


#### L2VNIStatus



VNIStatus defines the observed state of VNI.



_Appears in:_
- [L2VNI](#l2vni)



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
| `asn` _integer_ | ASN is the local AS number to use to establish a BGP session with<br />the default namespace. |  | Maximum: 4.294967295e+09 <br />Minimum: 1 <br /> |
| `vrf` _string_ | VRF is the name of the linux VRF to be used inside the PERouter namespace.<br />The field is optional, if not set it the name of the VNI instance will be used. |  | MaxLength: 15 <br />Pattern: `^[a-zA-Z][a-zA-Z0-9_-]*$` <br /> |
| `hostasn` _integer_ | ASN is the expected AS number for a BGP speaking component running in<br />the default network namespace. If not set, the ASN field is going to be used. |  | Maximum: 4.294967295e+09 <br />Minimum: 0 <br /> |
| `vni` _integer_ | VNI is the VXLan VNI to be used |  | Maximum: 4.294967295e+09 <br />Minimum: 0 <br /> |
| `localcidr` _[LocalCIDRConfig](#localcidrconfig)_ | LocalCIDR is the CIDR configuration for the veth pair<br />to connect with the default namespace. The interface under<br />the PERouter side is going to use the first IP of the cidr on all the nodes.<br />At least one of IPv4 or IPv6 must be provided. |  |  |
| `vxlanport` _integer_ | VXLanPort is the port to be used for VXLan encapsulation. | 4789 |  |


#### L3VNIStatus



L3VNIStatus defines the observed state of L3VNI.



_Appears in:_
- [L3VNI](#l3vni)



#### LocalCIDRConfig







_Appears in:_
- [L3VNISpec](#l3vnispec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `ipv4` _string_ | IPv4 is the IPv4 CIDR to be used for the veth pair<br />to connect with the default namespace. The interface under<br />the PERouter side is going to use the first IP of the cidr on all the nodes. |  |  |
| `ipv6` _string_ | IPv6 is the IPv6 CIDR to be used for the veth pair<br />to connect with the default namespace. The interface under<br />the PERouter side is going to use the first IP of the cidr on all the nodes. |  |  |


#### Neighbor



Neighbor represents a BGP Neighbor we want FRR to connect to.



_Appears in:_
- [UnderlaySpec](#underlayspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `asn` _integer_ | ASN is the AS number to use for the local end of the session. |  | Maximum: 4.294967295e+09 <br />Minimum: 1 <br /> |
| `address` _string_ | Address is the IP address to establish the session with. |  |  |
| `port` _integer_ | Port is the port to dial when establishing the session.<br />Defaults to 179. |  | Maximum: 16384 <br />Minimum: 0 <br /> |
| `password` _string_ | Password to be used for establishing the BGP session.<br />Password and PasswordSecret are mutually exclusive. |  |  |
| `passwordSecret` _string_ | PasswordSecret is name of the authentication secret for the neighbor.<br />the secret must be of type "kubernetes.io/basic-auth", and created in the<br />same namespace as the perouter daemon. The password is stored in the<br />secret as the key "password".<br />Password and PasswordSecret are mutually exclusive. |  |  |
| `holdTime` _[Duration](https://kubernetes.io/docs/reference/generated/kubernetes-api/v/#duration-v1-meta)_ | HoldTime is the requested BGP hold time, per RFC4271.<br />Defaults to 180s. |  |  |
| `keepaliveTime` _[Duration](https://kubernetes.io/docs/reference/generated/kubernetes-api/v/#duration-v1-meta)_ | KeepaliveTime is the requested BGP keepalive time, per RFC4271.<br />Defaults to 60s. |  |  |
| `connectTime` _[Duration](https://kubernetes.io/docs/reference/generated/kubernetes-api/v/#duration-v1-meta)_ | Requested BGP connect time, controls how long BGP waits between connection attempts to a neighbor. |  |  |
| `ebgpMultiHop` _boolean_ | EBGPMultiHop indicates if the BGPPeer is multi-hops away. |  |  |


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
| `asn` _integer_ | ASN is the local AS number to use for the session with the TOR switch. |  | Maximum: 4.294967295e+09 <br />Minimum: 1 <br /> |
| `vtepcidr` _string_ | VTEPCIDR is CIDR to be used to assign IPs to the local VTEP on each node. |  |  |
| `neighbors` _[Neighbor](#neighbor) array_ | Neighbors is the list of external neighbors to peer with. |  | MinItems: 1 <br /> |
| `nics` _string array_ | Nics is the list of physical nics to move under the PERouter namespace to connect<br />to external routers. This field is optional when using Multus networks for TOR connectivity. |  |  |


#### UnderlayStatus



UnderlayStatus defines the observed state of Underlay.



_Appears in:_
- [Underlay](#underlay)



