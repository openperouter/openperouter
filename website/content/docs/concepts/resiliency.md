---
weight: 30
title: "Router Resiliency"
description: "How OpenPERouter provides zero data-plane disruption during FRR failures"
icon: "article"
date: "2026-05-04T00:00:00+02:00"
lastmod: "2026-05-04T00:00:00+02:00"
toc: true
---

This page explains how OpenPERouter keeps the data plane running when FRR crashes or restarts.

## Overview

In a traditional container-based router deployment, the container runtime owns the network namespace. When FRR dies, the namespace is destroyed — tearing down all VXLAN tunnels, VRFs, bridges, veths, and routes. Every workload on the node loses VPN connectivity until the container restarts and the control plane reconverges.

OpenPERouter solves this by running FRR inside a **persistent named network namespace** (`/var/run/netns/perouter`). The namespace is created and owned by the controller, held open by a bind mount, and independent of any process. When FRR dies, the namespace stays — and so does the data plane.

## Named Network Namespace

The controller creates the named namespace via `EnsureNamespace()` on every reconciliation loop. The call is idempotent: it creates the namespace if it does not exist, and is a no-op if it already does. A bind mount at `/var/run/netns/perouter` keeps the namespace alive regardless of which processes are running inside it.

All kernel networking objects live inside this namespace:

- **VRFs** — one per VNI, providing L3 routing isolation
- **Bridges** — connecting VXLAN interfaces to VRFs
- **VXLAN interfaces** — handling tunnel encapsulation/decapsulation
- **Veth pairs** — connecting VRFs to the host network namespace
- **Underlay physical NIC** — the interface connected to the ToR switch
- **VTEP loopback (lo)** — the namespace loopback interface carrying the VTEP IP
- **Kernel routing tables, bridge FDB entries, ARP/neighbor entries, IP addresses**

When FRR crashes, all of these survive. The kernel continues forwarding packets using existing routes and FDB entries. There is **zero data-plane disruption**.

## What Survives an FRR Crash

| Component | Survives? | Reason |
|-----------|:---------:|--------|
| VRFs, bridges, VXLAN interfaces | Yes | Kernel objects tied to the namespace, not to FRR |
| Veth pairs (pe ↔ host) | Yes | Kernel objects |
| Underlay physical NIC | Yes | Stays inside the namespace |
| VTEP loopback (lo) | Yes | Kernel loopback interface |
| Kernel routing tables | Yes | Installed by zebra, persist in the namespace |
| Bridge FDB entries (MAC→VTEP) | Yes | Kernel bridge state |
| ARP / neighbor entries | Yes | Kernel neighbor table |
| IP addresses on interfaces | Yes | Kernel address state |
| **BGP sessions** | **No** | Process state — recovered via BGP Graceful Restart |

The only thing lost is the BGP control plane. Sessions drop when the FRR process exits. BGP Graceful Restart bridges this gap.

## BGP Graceful Restart

When [Graceful Restart is enabled]({{< ref "/docs/configuration/graceful-restart" >}}) on the Underlay CRD, FRR advertises the Graceful Restart capability with the `preserve-fw-state` flag. This tells peers:

1. The router's forwarding state is preserved across restarts (the F-bit).
2. Peers should keep stale routes active for a configurable window (`restartTime`, default 120 seconds) instead of withdrawing them.

When FRR restarts, the new process sends a BGP OPEN with the Graceful Restart capability. Peers recognize this as a restart event and continue using their existing routes until the session reconverges. Traffic keeps flowing throughout.

Additionally, zebra starts with the `-K 60` flag, which tells it to retain kernel nexthop objects and routes for 60 seconds after startup rather than flushing them. The init container copies the persistent `frr.conf` so FRR starts with its full configuration — including BGP neighbors — and can reconnect immediately.

BGP peers must also support Graceful Restart. This is widely supported across routing platforms including FRR, Cisco IOS-XE/NX-OS, Arista EOS, Juniper Junos, Nokia SR OS, and BIRD.

## Recovery Scenarios

### FRR Crash or Container Restart

The most common failure mode. The FRR process crashes, Kubernetes restarts the container, FRR re-enters the existing namespace and reconnects.

- **Data-plane outage**: 0 seconds
- **Control-plane outage**: ~7-22 seconds (bridged by Graceful Restart)

### FRR Image Upgrade

When the router pod is replaced with a new image (e.g., during a rolling update), the named namespace persists through the pod replacement. This is identical to a crash recovery from the network's perspective.

- **Data-plane outage**: 0 seconds
- **Control-plane outage**: ~7-22 seconds (bridged by Graceful Restart)

### Manual Namespace Rebuild

When the node's networking state is out of sync — stale interfaces, wrong IPs, partial failures — the operator deletes the namespace and restarts the router pod to trigger a full rebuild:

```bash
# 1. Remove the bind mount (marks the namespace for destruction)
ip netns delete perouter

# 2. Restart the router pod to release open handles — without this,
#    the kernel keeps the namespace alive because FRR holds file
#    descriptors to it
kubectl delete pod -n openperouter-system <router-pod-name>
```

Both steps are required. After the router pod exits, the kernel destroys the namespace and all objects inside it. The controller detects the missing namespace on its next reconciliation, recreates it, and re-provisions all interfaces from CRD state. The new router pod enters the fresh namespace.

- **Total outage (data + control plane)**: ~10-25 seconds

See [Troubleshooting Resiliency]({{< ref "/docs/configuration/troubleshooting-resiliency" >}}) for step-by-step instructions.

### Controller Upgrade

The controller pod is replaced with a new image. The named namespace and router pod are unaffected.

- **Disruption**: None

### Node Reboot

`/var/run` is a tmpfs filesystem — the namespace is destroyed on reboot. The node should be drained before reboot so workloads are moved to other nodes. After reboot, the system follows the initial boot sequence.

## Limitations

- **No kernel/node failure protection**: The named namespace lives in the kernel. A kernel crash or node power loss destroys everything. Protecting against node-level failures requires fabric-level redundancy (multiple nodes advertising reachability for the same prefixes).
- **Control-plane gap during Graceful Restart**: During the ~7-22 second window, topology changes (new routes, withdrawn routes) are not reflected. In practice, this is rarely a problem because topology changes during a brief restart are uncommon.
- **BGP Graceful Restart requires peer support**: If peers do not support or enable GR, they will withdraw routes immediately when the BGP session drops, causing traffic disruption until reconvergence.
