---
weight: 20
title: "Architecture"
description: "OpenPERouter system architecture, resiliency, and component lifecycle"
icon: "article"
date: "2025-06-15T15:03:22+02:00"
lastmod: "2026-05-04T00:00:00+02:00"
toc: true
---

This document describes the internal architecture of OpenPERouter and how its components work together to provide VPN functionality on Kubernetes nodes.

## System Overview

OpenPERouter consists of three main components:

1. **Router Pod**: Runs [FRR](https://frrouting.org/) inside a persistent named network namespace that survives container restarts
2. **Controller Pod**: Manages network configuration, namespace lifecycle, and orchestrates the router setup
3. **Labeler Pod**: Assigns persistent node indices for resource allocation

## Component Architecture

### Router Pod

The router pod is the core networking component that provides the actual VPN functionality.

The router pod runs as a DaemonSet to allow VPN connectivity to every node.

![](/images/openperouterpod.svg)

#### Purpose and Responsibilities

The router pod runs [FRR](https://frrouting.org/) inside a persistent named network namespace (`/var/run/netns/perouter`) and is responsible for:

- **BGP Sessions**: Establishing and maintaining BGP sessions with external routers and host components
- **VPN Route Processing**: Handling L3VPNs route advertisement and reception (for example, EVPN type 5 routes)
- **VPN Encapsulation**: Managing tunnel encapsulation and decapsulation of the VPN of choice
- **Route Translation**: Converting between BGP routes and L3 VPN routes
- **Network Namespace Management**: Operating inside the controller-managed named network namespace, where all kernel networking state (VRFs, bridges, VXLANs, routes, FDB entries) persists independently of the FRR process lifetime

#### Container Architecture

The router pod runs with `hostNetwork: true` and consists of two containers plus an init container:

- **Init Container**: On startup, checks if a persistent FRR configuration exists at `/etc/perouter/frr.conf` (written by the controller on every config push). If present, copies it into FRR's startup config directory so that FRR starts with its full configuration — including BGP neighbors and Graceful Restart settings — rather than a blank default. This is critical for fast recovery after a crash.
- **FRR Container**: Waits for the named network namespace to appear, then enters it via `nsenter --net=/var/run/netns/perouter` and runs FRR. When FRR exits, the container exits too, triggering a Kubernetes restart.
- **Reloader Sidecar**: Provides an HTTP endpoint on a unix socket that accepts FRR configuration updates and triggers configuration reloads via `frr-reload.py`.

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

#### Resiliency Properties

The named network namespace (`/var/run/netns/perouter`) is held open by a bind mount, independent of any process. When the FRR container crashes or is replaced, the namespace and all kernel networking state inside it persist. The kernel continues forwarding packets using existing routes and FDB entries — **zero data-plane disruption** during FRR restarts.

| Component | Survives FRR crash? | Why |
|-----------|:-------------------:|-----|
| VRFs, bridges, VXLAN interfaces | Yes | Kernel objects tied to the namespace |
| Veth pairs (pe/host) | Yes | Kernel objects tied to the namespace |
| Underlay physical NIC | Yes | Stays inside the namespace |
| VTEP loopback (lo) | Yes | Kernel loopback interface |
| Kernel routing tables | Yes | Installed by zebra, persist in the namespace |
| Bridge FDB entries | Yes | Kernel bridge state |
| ARP / neighbor entries | Yes | Kernel neighbor table |
| IP addresses on interfaces | Yes | Kernel address state |
| **BGP sessions** | **No** | Recovered via [BGP Graceful Restart]({{< ref "/docs/configuration/graceful-restart" >}}) |

BGP Graceful Restart bridges the control-plane gap: FRR signals to peers that its forwarding state is preserved, and peers keep stale routes active during a configurable restart window (~7-22 seconds). For the full conceptual explanation, see [Router Resiliency]({{< ref "/docs/concepts/resiliency" >}}).

### Controller Pod

The controller pod is the orchestration component that manages the router configuration and network setup.

![](/images/openpecontrollerpod.svg)

#### Purpose and Responsibilities

The controller pod handles all the complex network configuration logic and is responsible for:

- **Named Namespace Lifecycle**: Creates and manages the persistent named network namespace (`/var/run/netns/perouter`) via `EnsureNamespace()` on each reconciliation — creating it if it does not exist, or no-oping if it already does
- **Resource Reconciliation**: Watches and reconciles OpenPERouter Custom Resources (CRs)
- **Network Interface Management**: Moves host interfaces into the named network namespace
- **Veth and BGP session Setup**: Creates and configures the veth interfaces for each VPN session
- **EVPNVNI Setup**: Creates and configures network interfaces for each VNI
- **Configuration Generation**: Generates and applies FRR configuration
- **State Management**: Maintains the desired state of network configurations

#### Reconciliation Process

##### EVPN

The controller follows a specific sequence when reconciling VNI configurations:

1. **Namespace Creation**: Ensures the named network namespace exists at `/var/run/netns/perouter`. If missing, creates it and sets up the bind mount.
2. **Network Interface Creation**: For each VNI, creates the required network interfaces (bridge, VRF, VXLAN) for FRR operation inside the named namespace
3. **L3 Veth Pair Setup**: Creates veth pairs to connect VRFs to the host, assigns IPs from the `localCIDR`, and moves one end to the named namespace
4. **L2 Veth Pair Setup**: Creates veth pairs to connect VRFs to the host, enslaves the PERouter side to the bridge
corresponding to the L2 domain, eventually creates a bridge on the host, and enslaves the host side to the bridge it
just created or to an existing bridge (configurable)
5. **Configuration Deployment**: Generates the FRR configuration and sends it to the router pod for application

When reconciling an Underlay instance, the controller moves the host interface connected to the external router into the named network namespace.

#### Non-Recoverable Error Handling

When the controller detects an irrecoverable state divergence — such as the named network namespace being deleted externally, the underlay NIC being removed, or CRD state conflicting with kernel state — it triggers a full rebuild:

1. Deletes the named network namespace (destroying all kernel objects inside it)
2. Removes the persistent `frr.conf` so the next FRR instance starts with a clean configuration
3. Deletes the router pod

On the next reconciliation cycle, the controller recreates the namespace, re-provisions all interfaces from CRD state, and the DaemonSet starts a new router pod. This path is deterministic and always converges to a correct state. Unlike an FRR-only crash (zero data-plane disruption), a full namespace rebuild causes a brief data-plane disruption (~10-25 seconds).

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

## Recovery Lifecycle

OpenPERouter is designed to self-heal in most failure scenarios. Understanding the recovery behavior helps operators set expectations for disruption windows.

### FRR Crash Recovery

When FRR crashes or its container is replaced (e.g., during an image upgrade):

| Phase | Duration |
|-------|----------|
| FRR process dies | Instant |
| Kubernetes restarts the container | ~1-2s |
| FRR enters the existing named netns, reads persistent config | ~2-5s |
| BGP sessions re-establish (Graceful Restart bridges the gap) | ~3-15s |
| **Data-plane outage** | **0 seconds** |
| **Control-plane outage** | **~7-22 seconds** |

The kernel continues forwarding packets throughout because all networking state persists in the named namespace.

### Full Namespace Rebuild

When the named namespace is deleted — either manually (`ip netns delete perouter`) or by the controller via non-recoverable error handling — a full rebuild occurs:

| Phase | Duration |
|-------|----------|
| Namespace deletion | <1s |
| Controller detects missing namespace | <1s |
| Namespace recreation + interface provisioning | ~2-5s |
| FRR restart + BGP recovery | ~5-15s |
| **Total outage (data + control plane)** | **~10-25 seconds** |

This is the recommended recovery procedure when the node's networking state is out of sync. See [Troubleshooting Resiliency]({{< ref "/docs/configuration/troubleshooting-resiliency" >}}) for step-by-step instructions.

### Upgrade Behavior

| Upgrade type | Data-plane disruption | Control-plane disruption |
|-------------|----------------------|-------------------------|
| FRR image upgrade (pod replacement) | 0 seconds | ~7-22s (GR-bridged) |
| Controller upgrade | None | None |
| Node reboot | Full restart (node drained first) | Full restart |

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
- **Router (FRR)**: Runs as a Podman container on the host, entering the persistent named network namespace (`Network=ns:/var/run/netns/perouter`). The same resiliency properties apply: the namespace persists across container restarts, and BGP Graceful Restart bridges the control-plane gap
- **Hostbridge**: The only component that still runs as a Kubernetes pod. It is the component that provides access to the Kubernetes API by exporting API server credentials and the node configuration to the host via a shared volume. Without the hostbridge, stage 2 cannot start and the controller operates exclusively from static configuration

The node index, which in pod mode is assigned by the node labeler, is instead provided via the static `node-config.yaml` file on each host — either as a static integer (`nodeIndex.index`) or derived automatically from a network interface's IP address (`nodeIndex.interfaceName`), preferring IPv4 with fallback to IPv6. An optional `nodeIndex.cidr` can narrow which address is used when the interface has multiple IPs. See [Systemd Mode Configuration]({{< ref "/docs/configuration/systemd-mode" >}}) for details.
