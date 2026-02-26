# Control & Data Plane Resiliency for OpenPERouter

## Summary

OpenPERouter currently ties its data plane lifecycle to the FRR container
lifecycle. When the router pod dies, Kubernetes destroys its network namespace,
tearing down all VXLAN tunnels, VRFs, bridges, veth endpoints, the underlay
NIC, and the VTEP loopback. The kernel could keep forwarding packets with its
existing FDB and routing entries, but the ephemeral netns wipes everything.

This enhancement proposes decoupling the data plane from the FRR container by
running the router inside a **persistent named network namespace** managed by
systemd. Traffic continues flowing when the router container crashes or
restarts, and the control plane recovers within seconds via BGP Graceful
Restart.

## Motivation

### Goals

- **Zero data plane disruption** on FRR process crash or restart: the kernel
  continues forwarding packets using existing routes and FDB entries while FRR
  recovers.
- **Fast control plane recovery** (~7-22 seconds): FRR re-enters the existing
  network namespace, finds all interfaces intact, and re-establishes BGP
  sessions using Graceful Restart.
- **Simplified controller logic**: the controller targets a well-known netns
  path (`/var/run/netns/perouter`) instead of discovering a pod's netns via CRI
  queries or PID file parsing.
- **Decoupled interface provisioning**: the controller can pre-configure
  interfaces before FRR starts, rather than waiting for pod readiness.
- **Preserve the Multus underlay option**: the current deployment model supports
  underlay connectivity via either moving a physical NIC or via Multus
  `NetworkAttachmentDefinition`. The preferred proposal retains compatibility
  with both approaches (see [Multus Integration](#multus-integration)).

### Non-Goals

- Protection against full node (kernel) failure. Per-node resilience only
  addresses FRR process-level failures; node-level failures are handled at the
  fabric/cluster layer.
- Hitless upgrades in a single-instance deployment. Rolling upgrades without
  traffic loss require redundant instances (see
  [Alternatives](#alternatives)).
- Changes to workload pod networking. Multus-attached secondary interfaces on
  application pods and KubeVirt VMs are unaffected by this proposal.

## Proposal

### Overview

Replace the ephemeral pod-owned network namespace with a **persistent named
network namespace** (`/var/run/netns/perouter`) created by a systemd oneshot
unit at boot. The FRR container (running as a Podman quadlet) joins this
namespace instead of getting its own. When FRR dies, the bind-mounted namespace
persists, keeping all kernel networking state alive.

### Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                        K8s Node (Host)                          │
│                                                                 │
│  ┌──────────────────────────────────────┐                       │
│  │  Named Netns: /var/run/netns/perouter│  ◄── persists across  │
│  │  (created by systemd oneshot at boot)│      container death  │
│  │                                      │                       │
│  │  ┌─────────┐  ┌──────────┐           │                       │
│  │  │ lound   │  │ eth1     │           │                       │
│  │  │ (VTEP)  │  │(underlay)│           │                       │
│  │  └─────────┘  └──────────┘           │                       │
│  │                                      │                       │
│  │  ┌─────────────────────────────┐     │                       │
│  │  │ vrf100                      │     │                       │
│  │  │  ├─ br100 ─── vxlan100      │     │                       │
│  │  │  └─ pe0 (veth) ─────────────┼──┐  │                       │
│  │  └─────────────────────────────┘  │  │                       │
│  │                                   │  │                       │
│  │  FRR processes (come and go)      │  │                       │
│  │   bgpd, zebra, bfdd, staticd      │  │                       │
│  └───────────────────────────────────┼──┘                       │
│                                      │                          │
│  Host Network Namespace              │                          │
│  ┌───────────────────────────────────┼─────────────────┐        │
│  │  host0 (veth) ◄───────────────────┘                 │        │
│  │    ↕ BGP session (frr-k8s / Calico / Cilium)        │        │
│  │                                                     │        │
│  │  br-hs-{vni} (host bridges for L2VNI)               │        │
│  │  eth0 / CNI interfaces (K8s pod networking)         │        │
│  └─────────────────────────────────────────────────────┘        │
│                                                                 │
│  Systemd Units:                                                 │
│  ├─ perouter-netns.service  (oneshot, creates named netns)      │
│  ├─ routerpod-pod.service   (FRR + reloader, joins named netns) │
│  └─ controllerpod-pod.service (or K8s DaemonSet)                │
│                                                                 │
│  K8s Workload Pods (unchanged):                                 │
│  ├─ Pod A ── eth0 (CNI) + net1 (Multus macvlan → br-hs-100)     │
│  └─ Pod B ── eth0 (CNI) + net1 (Multus macvlan → br-hs-200)     │
└─────────────────────────────────────────────────────────────────┘
```

### User Stories

#### Story 1: FRR Process Crash

As a cluster operator, I want traffic to continue flowing when the FRR process
crashes, so that workloads experience zero data plane disruption while the
control plane recovers automatically within seconds.

#### Story 2: Router Software Upgrade

As a cluster operator, I want to restart the FRR container for a version
upgrade without tearing down VXLAN tunnels or VRFs, so that existing traffic
flows are not interrupted during the upgrade.

#### Story 3: Simplified Debugging

As a developer, I want the router's network namespace to be a well-known path
(`/var/run/netns/perouter`) so I can inspect it with standard tools
(`ip netns exec perouter ...`) without needing to discover a pod PID or netns
inode.

### Risks and Mitigations

| Risk | Mitigation |
|------|------------|
| Named netns is not cleaned up on node shutdown | `perouter-netns.service` has `ExecStop=ip netns delete perouter`; netns is also cleaned on reboot since `/var/run` is a tmpfs |
| FRR restarts too quickly before interfaces are ready | Interfaces persist in the named netns; FRR always finds them ready |
| BGP Graceful Restart not supported by all peers | GR is widely supported (FRR, BIRD, Cisco, Arista); document peer requirements |
| `lound` dummy interface never set to UP state (existing bug) | Fix `createLoopback()` in `internal/hostnetwork/underlay.go` to call `netlink.LinkSetUp(lound)` |

## Design Details

### Named Netns Lifecycle

A systemd oneshot unit creates the namespace at boot:

```ini
# perouter-netns.service
[Unit]
Description=Create persistent PE router network namespace
Before=routerpod-pod.service

[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/usr/sbin/ip netns add perouter
ExecStop=/usr/sbin/ip netns delete perouter

[Install]
WantedBy=multi-user.target
```

This creates `/var/run/netns/perouter` — a bind mount that holds the netns open
independently of any process. It persists until explicitly deleted or the node
reboots.

### FRR Quadlet Configuration

The key change in `frr.container`:

```ini
[Container]
# Join the persistent named netns instead of getting an ephemeral one:
Network=ns:/var/run/netns/perouter
```

When the container starts, Podman runs it inside `/var/run/netns/perouter`.
When the container dies, the netns stays. When systemd restarts the container,
it re-enters the same netns with all interfaces intact.

### Controller Netns Discovery

The `RouterProvider` interface
(`internal/controller/routerconfiguration/router.go`) already abstracts netns
discovery. A new `RouterNamedNSProvider` returns the well-known path:

```go
type RouterNamedNSProvider struct {
    NamespacePath string // "/run/netns/perouter"
}

func (r *RouterNamedNSProvider) TargetNS(ctx context.Context) (string, error) {
    return r.manager.NamespacePath, nil
}

func (r *RouterNamedNSProvider) HandleNonRecoverableError(ctx context.Context) error {
	client, err := systemdctl.NewClient()
	if err != nil {
		return fmt.Errorf("failed to create systemd client %w", err)
	}
	slog.Info("restarting router systemd unit", "unit", "routerpod-pod.service")
	if err := client.Restart(ctx, "routerpod-pod.service"); err != nil {
		return fmt.Errorf("failed to restart routerpod service")
	}
	slog.Info("router systemd unit restarted", "unit", "routerpod-pod.service")

	return nil
}

func (r *RouterNamedNSProvider) CanReconcile(ctx context.Context) (bool, error) {
    ns, err := netns.GetFromPath(r.manager.NamespacePath)
    if err != nil {
        return false, nil  // netns not ready yet
    }
    defer ns.Close()
    return true, nil
}
```

### Decoupled Lifecycle Sequence

With a named netns, the controller can pre-configure interfaces before FRR
starts:

1. **Boot** - `perouter-netns.service` creates the netns
2. **Controller starts** - immediately configures interfaces in the netns
   (VRFs, bridges, VXLANs, veths, underlay NIC)
3. **FRR starts later** - enters the netns, finds everything already set up,
   starts routing

### What Survives an FRR Container Death

When FRR dies, the kernel state in the named netns is completely preserved:

| Component | Survives? | Why |
|-----------|-----------|-----|
| VRFs | Yes | Kernel objects, not tied to any process |
| Bridges | Yes | Kernel objects |
| VXLAN interfaces | Yes | Kernel objects |
| Veth pairs (pe/host) | Yes | Kernel objects |
| Underlay physical NIC | Yes | Stays in the netns |
| `lound` (VTEP loopback) | Yes | Kernel dummy interface |
| Kernel routing tables | Yes | Installed by zebra, persist after zebra dies |
| Bridge FDB entries | Yes | Kernel bridge state |
| ARP/neighbor entries | Yes | Kernel neighbor table |
| IP addresses on interfaces | Yes | Kernel address state |

The only thing lost is the BGP control plane — sessions drop. With **BGP
Graceful Restart**, peers preserve routes for a configurable window, giving FRR
time to restart and re-establish sessions without data plane disruption.

### FRR Restart Sequence

1. FRR process dies
2. Named netns persists (held by the bind mount, not by any process)
3. All interfaces, routes, FDB entries remain intact
4. **Data plane keeps forwarding** using existing kernel state
5. Systemd restarts the FRR container (~1-2s)
6. Container re-enters the named netns via `Network=ns:/var/run/netns/perouter`
7. FRR starts, reads config, zebra discovers existing interfaces (~2-5s)
8. FRR re-establishes BGP sessions (~3-10s)
9. With BGP Graceful Restart, peers preserved routes during the downtime

### BGP Graceful Restart Configuration

```
router bgp 64514
  bgp graceful-restart
  bgp graceful-restart preserve-fw-state
  bgp graceful-restart stalepath-time 30
  bgp graceful-restart restart-time 120
```

Peers preserve routes for up to 120s (restart-time) while FRR recovers.
`preserve-fw-state` tells FRR not to flush kernel routes on restart.

### Systemd Fast Restart

```ini
[Service]
Restart=always
RestartSec=1
WatchdogSec=10
```

Systemd restarts FRR within 1 second of failure. With a named netns, FRR
re-enters the existing netns, discovers existing interfaces, and begins
re-establishing BGP sessions.

### BFD Timer Tuning

Increase BFD timers to prevent BFD from triggering faster than FRR can restart:

```
bfd
  profile slow-detect
    receive-interval 3000
    transmit-interval 3000
    detect-multiplier 5
```

This gives FRR 15 seconds to restart before BFD declares the session down.

### Multus Integration

There are two distinct Multus use cases. They behave differently with this
proposal:

#### Multus for Underlay Connectivity (Router)

The router pod can optionally receive its underlay interface via a Multus
`NetworkAttachmentDefinition`, as an alternative to the controller moving a
physical NIC.

With the quadlet approach, the router is no longer a K8s pod, so Multus CNI
cannot attach interfaces to it directly. However, the controller can replicate
the Multus behavior by creating a macvlan interface from the physical NIC and
moving it into the named netns — no Multus dependency needed.

**The Multus underlay option is preserved as a deployment choice.** Users who
prefer the Multus-based underlay for its integration with existing CNI
workflows can continue using the K8s DaemonSet deployment model (Option A
below). Users who prefer the quadlet model get equivalent functionality via
controller-created macvlan interfaces. This flexibility is a benefit of the
proposal: operators choose the deployment model that fits their environment
without losing underlay configuration options.

#### Multus for Workload Pods (L2VNI Secondary Interfaces)

**Completely unaffected.** Workload pods are still regular K8s pods. The host
bridges (`br-hs-{vni}`) are created by the controller in the host network
namespace. Multus still attaches macvlan/bridge interfaces to workload pods.
The data path is unchanged:

```
Workload Pod → macvlan on br-hs-{vni} (host netns) →
  veth → bridge in named netns → vxlan → underlay → remote VTEP
```

### Controller Deployment Options

The controller needs access to CRDs (K8s API). Two options:

#### Option A: Controller Stays as a K8s DaemonSet

- Watches CRDs natively via controller-runtime
- Targets `/run/netns/perouter` instead of discovering a pod's netns via CRI
- Already mounts `/run/netns` from the host
- No CRI socket needed anymore (simpler)
- Hybrid: K8s control plane, quadlet data plane
- **Preserves Multus underlay support** for environments that prefer it

#### Option B: Controller Is Also a Quadlet

- Uses the host kubeconfig to talk to the K8s API
- Already works in host mode today (`runHostMode()` in
  `cmd/hostcontroller/main.go`)
- Can start with static config and transition to K8s when the API is available
- Full independence from K8s for data plane

Option A is simpler for Kubernetes-first deployments. Option B gives full
independence — the router works even before the K8s API is available
(boot-time routing).

### Node Drain and Lifecycle

Since the router is a quadlet (not a K8s pod), `kubectl drain` does not touch
it. This is **desirable** — the router keeps forwarding traffic while workload
pods are being drained/migrated. The data plane stays up during maintenance.

If you need to explicitly stop the router during decommissioning, use
`systemctl stop routerpod-pod.service` on the node.

### Recovery Timeline

| Phase | Duration | What happens |
|-------|----------|-------------|
| FRR crash | 0s | Process dies |
| Data plane | Unaffected | Kernel continues forwarding (named netns) |
| Systemd restart | ~1-2s | FRR container re-enters named netns |
| FRR init | ~2-5s | Daemons start, read config, discover interfaces |
| BGP session setup | ~3-10s | TCP connect, OPEN, capability exchange |
| Route exchange | ~1-5s | With GR, stale routes already installed |
| **Total control plane outage** | **~7-22s** | |
| **Data plane outage** | **0s** | Kernel forwarding never stopped |

### Changes Required

| Component | Current | Proposed | Effort |
|-----------|---------|----------|--------|
| Netns creation | Implicit (pod/container lifecycle) | Explicit systemd oneshot | New file |
| `frr.container` quadlet | Own netns | `Network=ns:/var/run/netns/perouter` | One line change |
| Controller netns discovery | CRI query or PID file | Well-known path | New `RouterProvider` impl (trivial) |
| Interface configuration | Waits for pod ready | Can pre-configure netns | Logic simplification |
| Multus for underlay | Optional (annotation) | Optional (controller-created macvlan or Multus via DaemonSet) | Minor refactor |
| Multus for workloads | Via host bridges | Unchanged | None |
| Cluster CNI interface | Created but unused | Not created | Cleaner |
| Health monitoring | K8s probes | Quadlet `HealthCmd` | Already exists for controller |
| `HandleNonRecoverableError` | Delete pod / restart systemd unit | Restart systemd unit + optionally recreate netns | Minor |

### Test Plan

- **Unit tests**: New `RouterNamedNSProvider` implementation with mock netns
  paths.
- **Integration tests**: Verify that the controller can configure interfaces in
  a named netns before FRR starts, and that FRR discovers them on startup.
- **Resilience tests**: Kill the FRR container and verify:
  - All kernel objects survive (VRFs, bridges, VXLANs, veths, routes, FDB).
  - Data plane traffic continues flowing during the FRR outage.
  - FRR re-enters the netns and re-establishes BGP sessions.
  - BGP Graceful Restart preserves routes at peers during the outage window.
- **Lifecycle tests**: Verify `perouter-netns.service` creates and cleans up
  the netns correctly on boot and shutdown.
- **Upgrade tests**: Restart the FRR container with a new image version and
  confirm zero data plane disruption.

### Graduation Criteria

#### Alpha

- Named netns creation via systemd oneshot.
- FRR quadlet joins the named netns.
- `RouterNamedNSProvider` implementation.
- Basic resilience test (kill FRR, verify data plane survives).

#### Beta

- BGP Graceful Restart enabled on all sessions by default.
- BFD timer tuning integrated into the FRR configuration template.
- Controller pre-configures interfaces before FRR starts.
- Full resilience test suite.
- `lound` LinkSetUp bug fix.

#### GA

- Production validation across multiple deployment environments.
- Documentation for both DaemonSet (Option A) and full-quadlet (Option B)
  controller deployment models.
- Multus underlay compatibility verified in DaemonSet mode.

## Drawbacks

- **Control plane gap**: ~7-22 seconds where no new routes are learned or
  advertised. During this window, network topology changes are not reflected.
- **No protection against kernel/node failure**: This proposal only addresses
  FRR process-level failures. Full node failure requires fabric-level
  redundancy (cross-node EVPN, service VIP multi-homing).
- **BGP Graceful Restart dependency**: All BGP peers must support and enable
  Graceful Restart for seamless recovery. This is widely supported but must be
  documented as a requirement.
- **Stale routes risk**: During the control plane gap, routes may become stale
  if the network topology changes simultaneously (unlikely but possible).

## Alternatives

### Alternative 1: Redundant Router Instances (BGP Multi-homing)

Run two independent FRR instances on the same node, each in its own persistent
named netns, each with its own VTEP IP and router ID. This eliminates the
single point of failure entirely — one instance serves traffic while the other
recovers.

This approach can operate in two modes:

#### Active-Active (Dual VTEPs with ECMP)

Both instances carry traffic simultaneously. Remote VTEPs and the host-side BGP
speaker use ECMP to load-balance across both instances. When one dies, traffic
shifts entirely to the survivor.

```
┌────────────────────────────────────────────────────────────────────────────┐
│                           K8s Node                                         │
│                                                                            │
│  ┌─────────────────────────┐   ┌──────────────────────────┐                │
│  │ netns: perouter-a       │   │ netns: perouter-b        │                │
│  │                         │   │                          │                │
│  │ lound: 100.65.0.0/32    │   │ lound: 100.65.0.1/32     │                │
│  │ macvlan-a (underlay)    │   │ macvlan-b (underlay)     │                │
│  │                         │   │                          │                │
│  │ vrf100:                 │   │ vrf100:                  │                │
│  │  br-pe-100              │   │  br-pe-100               │                │
│  │   └─ vni100             │   │   └─ vni100              │                │
│  │  pe-100 ────────────┐   │   │  pe-100 ────────────┐    │                │
│  │                     │   │   │                     │    │                │
│  │ FRR-A (bgpd,zebra)  │   │   │ FRR-B (bgpd,zebra)  │    │                │
│  │ Router-ID: 10.0.0.1 │   │   │ Router-ID: 10.0.0.2 │    │                │
│  └─────────────────────┼───┘   └─────────────────────┼────┘                │
│                        │                             │                     │
│  Host Netns            │                             │                     │
│  ┌─────────────────────┼─────────────────────────────┼──────────────────┐  │
│  │  host-100-a ◄───────┘                             └──► host-100-b    │  │
│  │  192.169.10.3                                          192.169.10.4  │  │
│  │                                                                      │  │
│  │         Host BGP speaker (frr-k8s / Calico)                          │  │
│  │           sees TWO peers, installs ECMP routes                       │  │
│  └──────────────────────────────────────────────────────────────────────┘  │
└────────────────────────────────────────────────────────────────────────────┘
```

#### Active-Standby (Floating VTEP)

Only one instance is active at a time. The standby has interfaces
pre-configured but does not advertise routes. On failure, the standby takes
over the VTEP IP via GARP and activates its BGP sessions.

#### Why This Is Not Preferred

While redundant instances provide stronger resilience guarantees, they come at
significantly higher cost:

- **Double resource consumption**: 2x CPU, memory, and NIC bandwidth per node.
  Every node in the cluster pays this overhead whether or not a failure ever
  occurs.
- **Double IP consumption**: 2x VTEP IPs, router IDs, and host-side veth IPs.
  Address pools must be doubled (e.g. a `/24` VTEP CIDR that supported 256
  nodes now supports 128).
- **L2VNI complexity requires EVPN Multi-homing**: In active-active mode, two
  VTEPs behind the same host bridge cause BUM traffic duplication and MAC
  advertisement conflicts. Correct handling requires EVPN-MH (RFC 7432 / RFC
  8365) with ESI configuration, Designated Forwarder election, and
  split-horizon filtering. This is significant operational and configuration
  complexity.
- **Underlay NIC sharing**: A physical NIC can only live in one netns. Dual
  instances require macvlan, SR-IOV, or two physical NICs — each with its own
  trade-offs and hardware requirements.
- **Controller complexity**: The controller must manage two netns, two FRR
  configs, two sets of interfaces, and (in active-standby mode) a failover
  arbiter (keepalived, BFD, or health-check-driven switchover).
- **Fabric impact**: The ToR sees double the BGP sessions and EVPN routes per
  node, increasing control plane load on the fabric.

For most deployments, the named netns approach provides sufficient resilience
at a fraction of the cost. FRR crashes are rare, and the combination of
persistent data plane + BGP Graceful Restart bridges the brief control plane
gap. Redundant instances should be reserved for environments with strict SLA
requirements where even seconds of control plane outage is unacceptable, or
where hitless upgrades (upgrade one instance while the other serves traffic)
are a hard requirement.

#### Comparison

| Aspect | Named Netns (Proposed) | Redundant Instances (BGP Multi-homing) |
|--------|------------------------|----------------------------------------|
| **Data plane failover** | 0s (persistent netns) | Instant (ECMP) or 6-40s (active-standby) |
| **Control plane failover** | 7-22s (GR-bridged) | Instant (active-active) or 6-40s (active-standby) |
| **Resource overhead** | None | 2x per node |
| **IP consumption** | 1x | 2x VTEP, router ID, veth IPs |
| **L2VNI support** | Simple | Requires EVPN-MH (active-active) |
| **Fabric complexity** | 1x BGP sessions/routes | 2x BGP sessions/routes |
| **Controller changes** | Minor | Major |
| **Underlay NIC** | Physical (as-is) | macvlan / SR-IOV / dual NIC |
| **Implementation effort** | Low | High |
| **Multus underlay compat** | Yes (DaemonSet mode) | No (macvlan required) |

### Alternative 2: Cross-Node Redundancy via EVPN Fabric

Rely on the EVPN fabric itself for resilience rather than per-node redundancy.
Multiple nodes advertise reachability for the same IP prefix; when one node's
router fails, remote VTEPs withdraw that node's routes and use remaining paths.

This only works when the destination is reachable via multiple nodes (e.g.
replicated services, MetalLB VIPs). **It does not protect single-homed
workloads on the failing node.** Cross-node redundancy is a fabric-level
property that complements but does not replace per-node resilience. It requires
no code changes — it is a deployment best practice.

## Implementation History

- 2026-02-26: Initial proposal drafted.
