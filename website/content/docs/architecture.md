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
- **VPN Route Processing**: Handling L3VPNs route advertisement and reception (for example, EVPN type 5 routes)
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

## Systemd Mode Architecture

In systemd mode, the controller and router run directly on the host as Podman containers managed by systemd, instead of running as pods inside Kubernetes. As soon as Kubernetes is running, a host bridge pod provides the controller running on the host the credentials required to fetch additional configuration from the API server.

<!-- Image: High-level view of a node in systemd mode. Show the host with the controller and router (FRR) running as Podman containers, the static config files on disk, and the hostbridge pod inside the Kubernetes cluster connected to the host via a shared volume. Show the overlay network (VXLAN tunnels) going out to external routers / other nodes. -->
![](/images/systemd_architecture_hostbridge.svg)

This enables OpenPERouter to start at boot time and provide overlay network connectivity before the Kubernetes cluster is available. The network established by OpenPERouter can then be used as the underlay for Kubernetes itself.

### Two-Stage Boot Process

The systemd mode follows a two-stage boot process to transition from a standalone configuration to full integration with the Kubernetes API.

#### Stage 1: Static Configuration

At boot time, OpenPERouter starts and processes the static configuration files from `/var/lib/openperouter/configs/`. This establishes the overlay network using only the local file-based configuration. During this stage:

- The controller reads and merges all `openpe_*.yaml` files
- Network interfaces, VRFs, VXLAN tunnels, and BGP sessions are configured
- The overlay network becomes operational
- Changes to the configuration files are watched and trigger a reconciliation cycle, allowing updates without restarting the service

![](/images/systemd_architecture_host_only.svg)

#### Stage 2: API Server Integration

Once the Kubernetes API server becomes reachable (through the hostbridge pod exporting credentials to the host), OpenPERouter connects to it and starts reconciling Kubernetes Custom Resources. At this point:

- Configuration from the API server is merged with the static file-based configuration
- Both sources are reconciled together into a single FRR configuration
- Changes from either source (file updates or CR changes) trigger a new reconciliation
- The static configuration remains active, ensuring the base overlay survives API server unavailability

![](/images/systemd_architecture_host_and_k8s.svg)

### Host Components

In systemd mode, the components that normally run as pods inside the cluster run directly on the host:

- **Controller**: Runs as a Podman container on the host, reads static configuration files and (when available) connects to the Kubernetes API
- **Router (FRR)**: Runs as a Podman container on the host in a dedicated network namespace
- **Hostbridge**: The only component that still runs as a Kubernetes pod. It is the component that provides access to the Kubernetes API by exporting API server credentials and the node configuration to the host via a shared volume. Without the hostbridge, stage 2 cannot start and the controller operates exclusively from static configuration

The node index, which in pod mode is assigned by the node labeler, is instead provided via the static `node-config.yaml` file on each host.
