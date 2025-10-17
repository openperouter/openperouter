---
weight: 20
title: "Architecture"
description: "OpenPERouter system architecture and components"
icon: "article"
date: "2025-06-15T15:03:22+02:00"
lastmod: "2025-06-15T15:03:22+02:00"
toc: true
---

This document describes the internal architecture of OpenPERouter and how its components work together to provide VPN functionality on Kubernetes nodes.

## System Overview

OpenPERouter consists of three main components:

1. **Router Pod**: Runs [FRR](https://frrouting.org/) in a dedicated network namespace
2. **Controller Pod**: Manages network configuration and orchestrates the router setup
3. **Labeler Pod**: Assigns persistent node indices for resource allocation

## Component Architecture

### Router Pod

The router pod is the core networking component that provides the actual VPN functionality.

The router pod runs as a Daemonset to allow VPN connectivity to every node.

![](/images/openperouterpod.svg)

#### Purpose and Responsibilities

The router pod runs [FRR](https://frrouting.org/) in a dedicated network namespace and is responsible for:

- **BGP Sessions**: Establishing and maintaining BGP sessions with external routers and host components
- **VPN Route Processing**: Handling handling L3VPNs route advertisement and reception (for example, EVPN type 5 routes)
- **VPN Encapsulation**: Managing tunnel encapsulation and decapsulation of the VPN of choice
- **Route Translation**: Converting between BGP routes and L3 VPN routes
- **Network Namespace Management**: Operating in an isolated network namespace for security and isolation

#### Container Architecture

The router pod consists of two containers:

- **FRR Container**: Runs the Free Range Routing daemon and handles all BGP and VPN operations
- **Reloader Sidecar**: Provides an HTTP endpoint that accepts FRR configuration updates and triggers configuration reloads

The reloader sidecar container enables dynamic configuration updates without requiring pod restarts, allowing the controller to push new FRR configurations as network conditions change.

#### EVPN Network Configuration Requirements

To enable EVPN functionality, FRR requires specific network interfaces and configurations:

- **Linux VRF**: Creates isolated Layer 3 routing domains for each VNI
- **Linux Bridge**: Connects VXLAN interfaces to the VRF for proper traffic flow
- **VXLAN Interface**: Handles tunnel encapsulation/decapsulation with the configured VNI
- **VTEP Loopback**: Provides the VTEP IP address for tunnel endpoint identification

#### FRR Configuration

The router pod configures FRR with the following key settings:

- **VTEP Advertisement**: Advertises the local VTEP IP to the fabric for route reachability
- **EVPN Address Family**: Enables EVPN route exchange with external routers
- **Host BGP Sessions**: Establishes BGP sessions with components running on the host (L3VNIs)
- **Route Policies**: Configures import/export policies for proper route filtering

For detailed FRR configuration information, refer to the [official FRR documentation](https://docs.frrouting.org/en/latest/evpn.html?highlight=evpn).

### Controller Pod

The controller pod is the orchestration component that manages the router configuration and network setup.

![](/images/openpecontrollerpod.svg)

#### Purpose and Responsibilities

The controller pod handles all the complex network configuration logic and is responsible for:

- **Resource Reconciliation**: Watches and reconciles OpenPERouter Custom Resources (CRs)
- **Network Interface Management**: Moves host interfaces into the router's network namespace
- **Veth and BGP session Setup**: Creates and configures the veth interfaces for each VPN session
- **EVPNVNI Setup**: Creates and configures network interfaces for each VNI
- **Configuration Generation**: Generates and applies FRR configuration
- **State Management**: Maintains the desired state of network configurations

#### Reconciliation Process

##### EVPN

The controller follows a specific sequence when reconciling VNI configurations:

1. **Network Interface Creation**: For each VNI, creates the required network interfaces (bridge, VRF, VXLAN) for FRR operation
2. **L3 Veth Pair Setup**: Creates veth pairs to connect VRFs to the host, assigns IPs from the `localCIDR`, and moves one end to the router's namespace
3. **L2 Veth Pair Setup**: Creates veth pairs to connect VRFs to the host, enslaves the PERouter side to the bridge
corresponding to the L2 domain, eventually creates a bridge on the host, and enslaves the host side to the bridge it
just created or to an existing bridge (configurable)
4. **Configuration Deployment**: Generates the FRR configuration and sends it to the router pod for application

When reconciling an Underlay instance, the controller moves the host interface connected to the external router into the router's pod network namespace.

### Node Labeler

The node labeler is a critical component that ensures consistent resource allocation across the cluster.

#### Purpose and Responsibilities

The node labeler provides persistent node indexing and is responsible for:

- **Node Index Assignment**: Assigns a unique, persistent index to each node in the cluster
- **Resource Allocation**: Enables deterministic allocation of any IP that requires to be different on each node
(for example EVPN VTEP IPs and local CIDRs)
- **State Persistence**: Ensures resource allocations remain consistent across pod restarts and cluster reboots

#### Index Persistence

The node labeler persists the assigned index as a Kubernetes node label, making it available to other components. This ensures:

- **Consistency**: Each node maintains its assigned index even after pod rescheduling
- **Deterministic Allocation**: VTEP IPs and CIDRs are allocated based on the persistent index
