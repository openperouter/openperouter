---
weight: 15
title: "SRv6 - L3VPN"
description: "SRv6 L3VPN concepts"
icon: "article"
date: "2025-06-15T15:03:22+02:00"
lastmod: "2025-06-15T15:03:22+02:00"
toc: true
---

OpenPERouter implements SRv6 (Segment Routing over IPv6) L3VPN to provide
scalable overlay networking. BGP VPNv4/VPNv6 serves as the control plane
protocol that enables the distribution of IP reachability information across
the network fabric, while SRv6 provides the data plane encapsulation for L3
overlay traffic. IS-IS is used as the IGP for underlay reachability.

The solution supports two main overlay types:

- **L3VPN (Layer 3 VPN)**: Creates routed overlay networks where IP
  connectivity is extended across the fabric using SRv6 encapsulation. Each
  L3VPN corresponds to a VRF (Virtual Routing and Forwarding) instance that
  maintains separate routing tables with explicit route distinguishers and
  route targets.

- **L2VNI (Layer 2 Virtual Network Identifier)**: Creates bridged overlay
  networks where Layer 2 connectivity is extended across the fabric. L2VNIs
  use EVPN/VXLAN for East/West Layer 2 traffic (MAC learning, forwarding,
  and BUM traffic), allowing endpoints to communicate at the Ethernet layer
  as if they were on the same physical LAN segment.

Both overlay types can coexist and interoperate — L3VPN handles routed
domains via SRv6 while L2VNI handles bridged domains via EVPN/VXLAN,
enabling flexible network designs that combine both encapsulations as needed.

## Underlay Connectivity

### IS-IS, BGP, and SRv6

Unlike EVPN/VXLAN which uses a BGP-only underlay, SRv6 L3VPN requires IS-IS
as the Interior Gateway Protocol (IGP) to establish underlay reachability.
IS-IS distributes IPv6 routing information and SRv6 locator prefixes across
the fabric, making each node's SIDs reachable.

Each OpenPERouter instance is assigned:
- A unique **router ID** from the configured router ID CIDR
- A unique **SRv6 locator** derived from the base prefix, offset by the
  node index
- A unique **tunnel endpoint** IPv6 address from the configured CIDR

OpenPERouter can establish both eBGP multihop and iBGP sessions with the
fabric peers using VPNv4/VPNv6 address families. BGP is used to exchange
L3VPN routes with SRv6 SIDs identifying tunnel endpoints.


## Overlay Networks (VPNs)

### Virtual Networks

OpenPERouter supports the creation of multiple L3VPNs, each corresponding
to a separate VPN and allowing the extension of a routed domain via an SRv6
overlay. This enables multi-tenancy and network segmentation.

It supports the creation of L2 VNIs, where the veth is directly connected
to a layer 2 domain.

And it also supports a mixed scenario, where an L2 domain also belongs to a
broader L3 domain mapped to an L3 overlay.

### L3VPN Components

In addition to what's described in the
[concepts page]({{< ref "concepts/#l3-vpn-components" >}}), for each L3VPN
OpenPERouter automatically creates:

- **SRv6 SID Allocation**: Allocates SRv6 SIDs from the locator for VRF
  encapsulation/decapsulation
- **Route Translation**: Converts between BGP routes and VPNv4/VPNv6 routes
  with SRv6 encapsulation

#### IP Allocation Strategy

The IP addresses for the veth pair are allocated from the configured
`localcidr` for each L3VPN:

- **Router side**: Always gets the first IP in the CIDR
  (e.g., `192.169.11.0`)
- **Host side**: Each node gets a different IP from the CIDR, starting from
  the second value (e.g., `192.169.11.15`)

This consistent allocation strategy of the router IP simplifies
configuration across all nodes, as any BGP-speaking component on the host
can use the same IP address for the router side of the veth pair.

## Control Plane Operations

### Route Advertisement (Host → Fabric)

When a BGP-speaking component (like MetalLB) advertises a prefix to
OpenPERouter over an L3VPN session:

1. The host advertises the route with the veth interface IP as the next hop
2. OpenPERouter learns the route via the BGP session
3. OpenPERouter translates the route to a VPNv4/VPNv6 route with the local
   SRv6 SID
4. The VPN route is advertised to the fabric peers via the BGP session

### Route Reception (Fabric → Host)

When VPNv4/VPNv6 routes are received from the fabric:

1. OpenPERouter installs the routes in the VRF corresponding to the L3VPN
2. OpenPERouter translates the VPN routes to BGP routes
3. The BGP routes are advertised to the host via the veth interface
4. The host's BGP-speaking component learns and installs the routes

## Data Plane Operations

### Egress Traffic Flow

Traffic destined for networks learned via SRv6 L3VPN follows this path:

1. **Host Routing**: Traffic is redirected to the veth interface
   corresponding to the L3VPN
2. **Encapsulation**: OpenPERouter encapsulates the traffic using SRv6 with
   the appropriate SID
3. **Fabric Routing**: The fabric routes the IPv6-encapsulated packets to
   the destination PE using IS-IS
4. **Delivery**: The destination PE decapsulates and delivers the traffic

### Ingress Traffic Flow

SRv6-encapsulated packets received from the fabric are processed as follows:

1. **Decapsulation**: OpenPERouter removes the SRv6 encapsulation based on
   the local SID
2. **VRF Routing**: Traffic is routed within the VRF corresponding to the
   L3VPN
3. **Host Delivery**: Traffic is forwarded to the host via the veth interface
4. **Final Routing**: The host routes the traffic to the appropriate
   destination

## L2 VNI

While L3VPNs use SRv6 for routed domains, L2VNIs use EVPN/VXLAN for
East/West Layer 2 traffic. L2 frames are encapsulated in VXLAN with EVPN as
the control plane for MAC/IP advertisement, even when the broader L3 domain
uses SRv6.

> **Note**: `l2GatewayIPs` is not supported when using L3VPN (SRv6).
> L2VNIs with L3VPN provide Layer 2 pod-to-pod connectivity only.
> For Layer 3 connectivity to external hosts, use the L3VPN path via the
> pod's primary interface (eth0). The `l2GatewayIPs` field is only
> available when using L3VNI (EVPN/VXLAN).

For each Layer 2 VNI, OpenPERouter automatically:

- **Creates a VXLAN Interface**: Handles VXLAN tunnel
  encapsulation/decapsulation for L2 traffic
- **Integrates with the L3VPN**: The L2VNI shares the same VRF as the
  L3VPN, bridging Layer 2 and Layer 3 domains within a single routing
  instance

#### Host Interface Management

Given the Layer 2 nature of these connections, OpenPERouter supports
multiple interface management options:

- **Attaching to an existing bridge**: If a bridge already exists and is
  used by other components, OpenPERouter can attach the veth interface to it
- **Creating a new bridge**: OpenPERouter can create a bridge and attach the
  veth interface directly to it.
- **Direct veth usage**: With the understanding that the veth interface may
  disappear if the pod gets restarted, the veth can be used directly to
  extend an existing Layer 2 domain

##### Automatic Bridge Creation

The automatic bridge creation is useful for those scenarios where an
existing layer 2 domain is extended automatically through the veth
interface: when the router pod is deleted or restarted, the veth interface
is removed (and then recreated upon reconciliation), while the bridge
remains intact, making it a good candidate for attaching to an existing
layer 2 domain (i.e. setting it as master of a macvlan multus interface).

## Data Plane Operations

The following sections describe complete Layer 2 and Layer 3 scenarios for
reference:

### Egress Traffic Flow

When Layer 2 traffic arrives at the veth interface and the destination
belongs to the same subnet, the traffic is encapsulated and directed to the
VTEP where the endpoint with the MAC address corresponding to the
destination IP is located.

If the destination IP is on a different subnet, the traffic is routed via
the L3VPN corresponding to the VRF that the VPN is connected to.

### Ingress Traffic Flow

The ingress flow follows the reverse path of the egress flow. For Layer 2
traffic, encapsulated packets are decapsulated and forwarded to the
appropriate veth interface. For Layer 3 traffic, packets are routed through
the VRF and then forwarded to the host via the veth interface. The process
is straightforward and doesn't require additional explanation beyond what
has already been covered in the egress flow descriptions.
