---
weight: 42
title: "Graceful Restart"
description: "Configuring BGP Graceful Restart for router resiliency"
icon: "article"
date: "2026-05-04T00:00:00+02:00"
lastmod: "2026-05-04T00:00:00+02:00"
toc: true
---

BGP Graceful Restart allows FRR to signal to peers that its forwarding state survives restarts. When enabled, peers preserve routes during the restart window instead of withdrawing them, bridging the control-plane gap with zero data-plane disruption.

For the conceptual explanation of how this fits into the resiliency architecture, see [Router Resiliency]({{< ref "/docs/concepts/resiliency" >}}).

## Enabling Graceful Restart

Add the `gracefulRestart` field to your Underlay CR. An empty object enables Graceful Restart with default values:

```yaml
apiVersion: network.openperouter.io/v1alpha1
kind: Underlay
metadata:
  name: underlay
  namespace: openperouter-system
spec:
  asn: 64514
  interfaces:
    - type: NetworkDevice
      networkDevice:
        interfaceName: toswitch
  neighbors:
    - asn: 64512
      address: 192.168.11.2
  gracefulRestart: {}
```

Omitting the `gracefulRestart` field entirely disables Graceful Restart.

## Configuration Fields

| Field | Type | Description | Default | Range |
|-------|------|-------------|---------|-------|
| `gracefulRestart` | object | Enables BGP Graceful Restart when present. Omit to disable. | _(disabled)_ | |
| `gracefulRestart.restartTime` | integer | Seconds that the restarting router requests peers to preserve routes. Peers wait this long before removing stale routes. | 120 | 1–4095 |
| `gracefulRestart.stalePathTime` | integer | Seconds that stale paths from a restarting peer are retained locally. | 360 | 1–4095 |

## Full Example

```yaml
apiVersion: network.openperouter.io/v1alpha1
kind: Underlay
metadata:
  name: underlay
  namespace: openperouter-system
spec:
  asn: 64514
  tunnelEndpoint:
    cidrs:
    - 100.65.0.0/24
  interfaces:
    - type: NetworkDevice
      networkDevice:
        interfaceName: toswitch
  neighbors:
    - asn: 64512
      address: 192.168.11.2
  gracefulRestart:
    restartTimeSeconds: 180
    stalePathTimeSeconds: 600
```

## What Happens When Graceful Restart Is Enabled

When the controller detects the `gracefulRestart` field on the Underlay, it configures FRR and the router pod for resilient restarts:

1. **FRR configuration**: The generated `frr.conf` includes `bgp graceful-restart`, `bgp graceful-restart preserve-fw-state`, and the configured `restart-time` and `stalepath-time` values.
2. **Zebra kernel retention**: Zebra starts with the `-K 60` flag, which tells it to retain existing kernel nexthop objects, routes, and FDB entries for 60 seconds after startup instead of flushing them.
3. **Persistent configuration**: The init container copies the persistent `frr.conf` (written by the controller on every config push) into FRR's startup directory. This means FRR starts with its full BGP configuration — including neighbor definitions and Graceful Restart settings — rather than a blank default.
4. **Fast reconnection**: The controller automatically sets `timers connect 5` on all underlay neighbors, reducing the BGP connect-retry interval to 5 seconds so the new FRR process reconnects quickly after the old session drops.

On restart, FRR sends a BGP OPEN with the F-bit (Forwarding State Preserved), telling peers that the data plane is intact. Peers keep their existing routes active until the session reconverges.

## Peer Requirements

BGP peers (ToR switches, route reflectors) must also support and enable BGP Graceful Restart for the full benefit. Without peer support, the peer will withdraw routes immediately when the session drops, causing traffic disruption until BGP reconverges.

Graceful Restart is widely supported across routing platforms:

- FRR
- Cisco IOS-XE / NX-OS
- Arista EOS
- Juniper Junos
- Nokia SR OS
- BIRD
