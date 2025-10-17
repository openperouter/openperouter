---
weight: 20
title: "L3 Passthrough"
description: "L3 Passthrough concepts"
icon: "article"
date: "2025-01-27T10:00:00+02:00"
lastmod: "2025-01-27T10:00:00+02:00"
toc: true
---

## Overview

L3 Passthrough is a networking mode in OpenPERouter that provides direct BGP connectivity between the host and the BGP fabric without encapsulation. Unlike EVPN mode which uses VXLAN tunnels, passthrough mode allows the host to participate directly in the BGP fabric as a peer.

## Key Characteristics

### Direct BGP Participation

In passthrough mode, OpenPERouter establishes a BGP session directly with BGP-speaking components on the host (such as MetalLB). This session operates in the same BGP domain as the fabric, allowing the host to:

- Advertise routes directly to the fabric
- Receive routes directly from the fabric
- Participate in the BGP routing decisions without encapsulation overhead

### No Isolation

Unlike the other VPN modes (currently EVPN), passthrough does not provide traffic isolation nor encapsulation. Traffic flows directly between the host and the fabric without tunnel overhead, making it suitable for scenarios where the host needs direct access to the fabric.

- Low latency is critical
- Simple routing is preferred over complex overlay networks
- Direct fabric participation is required

**Note**: Only one L3 passthrough configuration can be defined per OpenPERouter instance, as passthrough mode operates exclusively within the default VRF and does not support VRF isolation.

## Architecture Components

### Veth Pair Configuration

OpenPERouter automatically creates a veth pair for passthrough connectivity:

- **Host side**: `pt-host` - Connected to the host network namespace
- **Router side**: `pt-ns` - Connected to the OpenPERouter network namespace

### IP Allocation Strategy

The IP addresses for the veth pair are allocated from the configured `localcidr`:

- **Router side**: Gets the first IP in the CIDR (e.g., `192.169.10.1`)
- **Host side**: Gets the second IP in the CIDR (e.g., `192.169.10.2`)

This consistent allocation ensures that BGP-speaking components on the host can always use the same router IP address for establishing BGP sessions.

### BGP Session Configuration

The passthrough BGP session is configured with:

- **Local ASN**: The router's ASN (e.g., 64514)
- **Remote ASN**: The host's ASN (e.g., 64515)
- **Neighbor IP**: The host side of the veth pair
- **Address families**: IPv4 and/or IPv6 unicast

## Control Plane Operations

### Route Advertisement (Host → Fabric)

![](/images/openpeadvertisepassthrough.svg)

When a BGP-speaking component on the host advertises routes:

1. The host establishes a BGP session with OpenPERouter using the veth interface IP
2. Routes are advertised with the veth interface IP as the next hop
3. OpenPERouter receives the routes and installs them in the global routing table
4. OpenPERouter advertises these routes to the fabric neighbors
5. The fabric learns the routes with OpenPERouter's router ID as the next hop

### Route Reception (Fabric → Host)

When routes are received from the fabric:

1. OpenPERouter receives routes from fabric neighbors
2. Routes are installed in the global routing table
3. OpenPERouter advertises these routes to the host via the BGP session
4. The host's BGP-speaking component learns and installs the routes
5. Traffic can now be routed directly to the learned destinations

## Data Plane Operations

### Traffic Flow

In passthrough mode, traffic flows directly without encapsulation:

![](/images/openpeadvertisedatapassthrough.svg)

1. **Host Routing**: Traffic is routed to the veth interface
2. **Direct Forwarding**: OpenPERouter forwards traffic directly to fabric interfaces
3. **Fabric Routing**: The fabric routes traffic to the destination
4. **Return Path**: Return traffic follows the same direct path


