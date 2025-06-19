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

1. **Router Pod**: Runs FRR in a dedicated network namespace
2. **Controller Pod**: Manages network configuration and BGP sessions
3. **Node Labeller**: Assigns persistent node indices for resource allocation

## Underlay Connectivity

### Fabric Integration

OpenPERouter integrates with your network fabric by establishing BGP sessions with external routers (typically ToR switches).

#### Network Interface Management

OpenPERouter works by moving the physical network interface connected to the ToR switch into the router's network namespace:

![](/images/openpehostnic.svg)

This allows the router to establish direct BGP sessions with the fabric and receive routing information.

#### VTEP IP Assignment

Each OpenPERouter instance is assigned a unique VTEP (Virtual Tunnel End Point) IP address from a configured CIDR range. This VTEP IP serves as the identifier for the router within the fabric.

OpenPERouter establishes a BGP session with the fabric, advertising its VTEP IP to other routers. The VPN address family is enabled on this session, allowing the exchange of EVPN routes required for overlay connectivity.

![](/images/openpebgpfabric.png)

## Overlay Networks (VNIs)

### Virtual Network Identifiers

OpenPERouter supports the creation of multiple VNIs (Virtual Network Identifiers), each corresponding to a separate EVPN tunnel. This enables multi-tenancy and network segmentation.

![](/images/openpebgphost.png)

### VNI Components

For each Layer 3 VNI, OpenPERouter automatically creates:

- **Veth Pair**: Named after the VNI (e.g., `host-200@pe-200`) for host connectivity
- **BGP Session**: Established between the host and router over the veth pair
- **Linux VRF**: Isolates the routing space for each VNI within the router
- **VXLAN Interface**: Handles tunnel encapsulation/decapsulation
- **Route Translation**: Converts between BGP routes and EVPN Type 5 routes

### IP Allocation Strategy

IP addresses are allocated from the configured `localcidr` for each VNI:

- **Router side**: Always gets the first IP in the CIDR (e.g., `192.169.11.1`)
- **Host side**: Gets the second IP in the CIDR (e.g., `192.169.11.2`)

This consistent allocation strategy simplifies configuration across all nodes.

## Control Plane Operations

### Route Advertisement (Host → Fabric)

When a BGP-speaking component (like MetalLB) advertises a prefix to OpenPERouter:

![](/images/openpeadvertise.svg)

1. The host advertises the route with the veth interface IP as the next hop
2. OpenPERouter learns the route via the BGP session
3. OpenPERouter translates the route to an EVPN Type 5 route
4. The EVPN route is advertised to the fabric with the local VTEP as the next hop

### Route Reception (Fabric → Host)

When EVPN Type 5 routes are received from the fabric:

1. OpenPERouter installs the routes in the VRF corresponding to the VNI
2. OpenPERouter translates the EVPN routes to BGP routes
3. The BGP routes are advertised to the host via the veth interface
4. The host's BGP-speaking component learns and installs the routes

## Data Plane Operations

### Egress Traffic Flow

Traffic destined for networks learned via EVPN follows this path:

1. **Host Routing**: Traffic is redirected to the veth interface corresponding to the VNI
2. **Encapsulation**: OpenPERouter encapsulates the traffic in VXLAN packets with the appropriate VNI
3. **Fabric Routing**: The fabric routes the VXLAN packets to the destination VTEP
4. **Delivery**: The destination endpoint instance receives and processes the traffic

### Ingress Traffic Flow

VXLAN packets received from the fabric are processed as follows:

1. **Decapsulation**: OpenPERouter removes the VXLAN header
2. **VRF Routing**: Traffic is routed within the VRF corresponding to the VNI
3. **Host Delivery**: Traffic is forwarded to the host via the veth interface
4. **Final Routing**: The host routes the traffic to the appropriate destination
