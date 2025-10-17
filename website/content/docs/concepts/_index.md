---
weight: 10
title: "Concepts"
description: "What is OpenPERouter and how to use it"
icon: "article"
date: "2025-06-15T15:03:22+02:00"
lastmod: "2025-06-15T15:03:22+02:00"
toc: true
---

This section explains the core concepts behind OpenPERouter and how it integrates with your network infrastructure.

## Overview

OpenPERouter transforms Kubernetes nodes into Provider Edge (PE) routers by running [FRR](https://frrouting.org/) in a dedicated network namespace. This enables EVPN (Ethernet VPN) functionality directly on your Kubernetes nodes, eliminating the need for external PE routers.

## Network Architecture

### Traditional vs. OpenPERouter Architecture

In traditional deployments, Kubernetes nodes connect to Top-of-Rack (ToR) switches via VLANs, and external PE routers handle EVPN tunneling. OpenPERouter moves this PE functionality directly into the Kubernetes nodes.

![](/images/openpedescription.svg)

### Key Components

OpenPERouter consists of three main components:

1. **Router Pod**: Runs [FRR](https://frrouting.org/) in a dedicated network namespace
2. **Controller Pod**: Manages network configuration and orchestrates the router setup
3. **Node Labeler**: Assigns persistent node indices for resource allocation

## Underlay Connectivity

### Fabric Integration

OpenPERouter integrates with your network fabric by establishing BGP sessions with external routers (typically ToR switches).

#### Network Interface Management

OpenPERouter works by moving the physical network interface connected to the ToR switch into the router's network namespace:

![](/images/openpehostnic.svg)

This allows the router to establish direct BGP sessions with the fabric and receive routing information.

## Overlay Networks (VPNs)

### Virtual Networks

OpenPERouter supports the creation of multiple VPNs, each corresponding to a separate VPN tunnel. This enables multi-tenancy and network segmentation.

OpenPERouter supports the creation of multiple VPNs, allowing the extension of a routed domain via a
VPN overlay:

![](/images/openpehostvpnl3.svg)

It supports the creation of L2 VPNs (where possible), where the veth is directly connected to a layer 2 domain

![](/images/openpel2hostvpn.svg)

And it also supports a mixed scenario, where an L2 domain also belongs to a broader L3 domain
mapped to an L3 overlay

![](/images/openpel2l3host.svg)

### L3 VPN Components

For each Layer 3 VNI, OpenPERouter automatically creates:

- **Veth Pair**: Named after the VPN (e.g., `host-200@pe-200` in case of EVPN) for host connectivity
- **BGP Session**: Configured over the veth pair connecting the router to the host, to allow BGP
connectivity with the host
- **Linux VRF**: Isolates the routing space for each VNI within the router's network namespace
- **Route Translation**: Converts between BGP routes and L3 VPN routes

#### IP Allocation Strategy

The IP addresses for the veth pair are allocated from the configured `localcidr` for each VNI:

- **Router side**: Always gets the first IP in the CIDR (e.g., `192.169.11.0`)
- **Host side**: Each node gets a different IP from the CIDR, starting from the second value (e.g., `192.169.11.15`)

This consistent allocation strategy of the router IP simplifies configuration across all nodes, as any BGP-speaking
component on the host can use the same IP address for the router side of the veth pair.

## Control Plane Operations

### Route Advertisement (Host → Fabric)

When a BGP-speaking component (like MetalLB) advertises a prefix to OpenPERouter:

![](/images/openpeadvertise.svg)

1. The host advertises the route with the veth interface IP as the next hop
2. OpenPERouter learns the route via the BGP session
3. OpenPERouter translates the route to an L3VPN route
4. The VPN route is advertised to the fabric with the local router as the next hop

### Route Reception (Fabric → Host)

When EVPN Type 5 routes are received from the fabric:

![](/images/openpereceive.svg)

1. OpenPERouter installs the routes in the VRF corresponding to the VPN
2. OpenPERouter translates the VPN routes to BGP routes
3. The BGP routes are advertised to the host via the veth interface
4. The host's BGP-speaking component learns and installs the routes

## L2 VPN

For each Layer 2 VPN, OpenPERouter automatically creates:

- **Veth Pair**: Named after the VNI (e.g., `host-200@pe-200`) for Layer 2 host connectivity
- **Linux VRF**: Optional, isolates the routing space for each VPN within the router's network namespace

### Host Interface Management

Given the Layer 2 nature of these connections, OpenPERouter supports multiple interface management options:

- **Attaching to an existing bridge**: If a bridge already exists and is used by other components, OpenPERouter can attach the veth interface to it
- **Creating a new bridge**: OpenPERouter can create a bridge and attach the veth interface directly to it.
- **Direct veth usage**: With the understanding that the veth interface may disappear if the pod gets restarted, the veth can be used directly to extend an existing Layer 2 domain

#### Automatic Bridge Creation

The automatic bridge creation is useful for those scenarios where an existing layer 2 domain is extended automatically through the veth interface:
when the router pod is deleted or restarted, the veth interface is removed (and then recreated upon reconciliation), while the bridge remains intact,
making it a good candidate for attaching to an existing layer 2 domain (i.e. setting it as master of a macvlan multus interface).
