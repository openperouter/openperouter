# Control & Data Plane Resiliency for OpenPERouter

## Summary

OpenPERouter currently ties its data plane lifecycle to the FRR container
lifecycle. When the router pod dies, Kubernetes destroys its network namespace,
tearing down all VXLAN tunnels, VRFs, bridges, veth endpoints, the underlay
NIC, and the VTEP loopback. The kernel could keep forwarding packets with its
existing FDB and routing entries, but the ephemeral netns wipes everything.

This enhancement proposes decoupling the data plane from the FRR container by
running the router inside a **persistent named network namespace**. The netns
is held open by a bind mount, independent of any container or process lifetime.
Traffic continues flowing when the router container crashes or restarts, and
the control plane recovers within seconds via BGP Graceful Restart.

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
network namespace** (`/var/run/netns/perouter`) created at boot and held open
by a bind mount. The FRR process runs inside this namespace; when FRR dies, the
bind-mounted namespace persists, keeping all kernel networking state alive —
VRFs, bridges, VXLANs, routes, FDB entries, and the underlay NIC all survive.

### Router pod deployment model
The persistent named netns is the **core idea** of this proposal. It is
orthogonal to how the FRR container is deployed, which can be in any of the
following alternatives:

- **Podman quadlet**: a systemd oneshot creates the netns; the FRR container
  joins it via `Network=ns:/var/run/netns/perouter`. Systemd manages the
  lifecycle.
- **Kubernetes hostNetwork pod**: a privileged init container (or a DaemonSet
  sidecar) creates the named netns; the FRR container uses `nsenter` to run
  inside it. The pod runs with `hostNetwork: true` but FRR itself operates in
  the named netns.
The deployment model affects how the container is managed and restarted, but
the resilience properties — persistent data plane, BGP Graceful Restart
bridging the control plane gap — are the same in all cases. The trade-offs
between these options are discussed in
[Router Deployment Model](#router-deployment-model).

### Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                        K8s Node (Host)                          │
│                                                                 │
│  ┌──────────────────────────────────────┐                       │
│  │  Named Netns: /var/run/netns/perouter│  ◄── persists across  │
│  │  (created by the controller)         │      container death  │
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
│  Components:                                                    │
│  ├─ Controller (K8s DaemonSet)                                  │
│  │    creates netns, provisions interfaces, manages FRR config  │
│  └─ FRR + Reloader (Quadlet or hostNetwork Pod)                 │
│       joins named netns, runs routing daemons                   │
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

As a cluster operator, I want to upgrade the OpenPERouter bits (controller,
router, etc) for a version upgrade without tearing down VXLAN tunnels or VRFs,
so that existing traffic flows are not interrupted during the upgrade.

#### Story 3: Recovery from Out-of-Sync Namespace

As a support engineer troubleshooting a node where the router namespace has
drifted out of sync with the desired configuration (e.g. due to a controller
bug, a partial failure, or manual intervention), I want a single, well-known
recovery procedure — delete the namespace and restart the router service — that
deterministically rebuilds the entire data plane from scratch without requiring
node reboot or CRD re-creation.

### Risks and Mitigations

| Risk | Mitigation |
|------|------------|
| Named netns is not cleaned up on node shutdown | `/var/run` is a tmpfs, so the bind mount is automatically removed on reboot. The controller can also explicitly delete the netns during graceful shutdown. |
| FRR restarts too quickly before interfaces are ready | Interfaces persist in the named netns; FRR always finds them ready |
| BGP Graceful Restart not supported by all peers | GR is widely supported (FRR, BIRD, Cisco, Arista); document peer requirements |
| Router netns drifts out of sync with desired config (controller bug, partial failure, manual interference) | The system is designed for full netns teardown and rebuild: `ip netns delete perouter` followed by a restart of the router process deterministically recreates a correct state. The controller detects the missing netns, recreates it, and re-provisions all interfaces from CRD state. See [Recovery from a Deleted or Emptied Namespace](#recovery-from-a-deleted-or-emptied-namespace). |

## Design Details

### Named Netns Lifecycle

The controller pod is responsible for creating and managing the well-known
network namespace (`/var/run/netns/perouter`). This is a natural fit because
the controller already provisions all interfaces inside the netns — it simply
adds namespace creation as the first step in its reconciliation loop.

On startup, the controller checks whether `/var/run/netns/perouter` exists. If
not, it creates it:

```go
func (r *RouterNamedNSProvider) EnsureNamespace(ctx context.Context) error {
    if _, err := os.Stat(r.NamespacePath); err == nil {
        return nil // netns already exists
    }

    runtime.LockOSThread()
    defer runtime.UnlockOSThread()

    // Create a new network namespace
    newNS, err := netns.New()
    if err != nil {
        return fmt.Errorf("failed to create netns: %w", err)
    }
    defer newNS.Close()

    // Bind-mount it to the well-known path so it persists
    if err := os.MkdirAll(filepath.Dir(r.NamespacePath), 0o755); err != nil {
        return fmt.Errorf("failed to create netns directory: %w", err)
    }
    f, err := os.OpenFile(r.NamespacePath, os.O_CREATE|os.O_EXCL, 0o444)
    if err != nil {
        return fmt.Errorf("failed to create netns mount point: %w", err)
    }
    f.Close()

    src := fmt.Sprintf("/proc/self/fd/%d", int(newNS))
    if err := unix.Mount(src, r.NamespacePath, "", unix.MS_BIND, ""); err != nil {
        os.Remove(r.NamespacePath)
        return fmt.Errorf("failed to bind-mount netns: %w", err)
    }

    slog.Info("created persistent network namespace", "path", r.NamespacePath)
    return nil
}
```

This creates `/var/run/netns/perouter` — a bind mount that holds the netns open
independently of any process. It persists until explicitly deleted or the node
reboots (since `/var/run` is a tmpfs).

Having the controller own the netns lifecycle has several advantages:

- **Single owner**: the same component that creates interfaces inside the netns
  also creates the netns itself. There is no ordering dependency on a separate
  systemd unit.
- **Works in all deployment models**: whether the controller runs as a K8s
  DaemonSet or a Podman quadlet, it creates the netns the same way. No
  host-level systemd units are required.
- **Idempotent**: `EnsureNamespace` is safe to call on every reconciliation
  loop — it is a no-op if the netns already exists.
- **Recovery-aware**: after a namespace deletion (manual or via
  `HandleNonRecoverableError`), the controller detects the missing netns on its
  next reconciliation and recreates it automatically.

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

With a named netns, the controller can create the namespace and pre-configure
interfaces before FRR starts:

1. **Controller starts** - creates the named netns via `EnsureNamespace()`,
   then configures interfaces in it (VRFs, bridges, VXLANs, veths, underlay
   NIC)
2. **FRR starts** - attempts to enter the netns. If the controller has not yet
   created it, the router pod crash-loops until the netns is ready. Once
   available, FRR enters the netns, finds everything already set up, and starts
   routing

The following sequence diagrams illustrate how the controller, FRR, and the
BGP peers interact across the three key lifecycle scenarios.

#### Initial Boot Sequence

```mermaid
sequenceDiagram
    autonumber
    participant netns as Named Netns<br/>/var/run/netns/perouter
    participant ctrl as Controller<br/>(K8s DaemonSet)
    participant k8s as K8s API
    participant reloader as Reloader<br/>(routerpod)
    participant frr as FRR<br/>(routerpod)
    participant tor as ToR Switch<br/>(BGP Peer)
    participant hostBGP as Host BGP Speaker<br/>(frr-k8s / Calico)

    Note over ctrl: Node boots, K8s schedules<br/>controller DaemonSet pod

    rect rgb(20, 40, 70)
        Note right of ctrl: Phase 1: Controller Creates Named Netns
        activate ctrl
        ctrl->>k8s: List Underlay, L3VNI, L2VNI,<br/>L3Passthrough CRDs
        k8s-->>ctrl: CRD specs (filtered by node selector)

        ctrl->>netns: EnsureNamespace():<br/>Create netns + bind mount
        activate netns
        Note over netns: Bind mount created at<br/>/var/run/netns/perouter<br/>Persists independently of any process
    end

    rect rgb(20, 55, 35)
        Note right of ctrl: Phase 2: Controller Provisions Interfaces
        Note over ctrl,netns: Pre-configure interfaces before FRR starts

        ctrl->>netns: SetupUnderlay()<br/>Move physical NIC (eth1) into netns
        ctrl->>netns: Create lound dummy interface<br/>Assign VTEP IP (e.g. 100.65.0.X/32)
        ctrl->>netns: SetupL3VNI() for each L3VNI:<br/>Create VRF, bridge, VXLAN,<br/>veth pair (pe-side in netns, host-side in host)
        ctrl->>netns: SetupL2VNI() for each L2VNI:<br/>Create VRF, bridge, VXLAN,<br/>veth pair, host-side bridge (br-hs-{vni})
        ctrl->>netns: SetupPassthrough() if configured:<br/>Create veth pair for host BGP connectivity

        Note over ctrl: All interfaces ready in netns<br/>before FRR starts
    end

    rect rgb(70, 45, 15)
        Note right of frr: Phase 3: FRR + Reloader Start
        Note over frr: Router pod may have been crash-looping<br/>waiting for netns to appear — now succeeds
        frr->>netns: Enter named netns<br/>(Network=ns: or nsenter)
        activate frr
        Note over frr: FRR finds all interfaces<br/>already configured by controller

        activate reloader
        reloader-->>reloader: Listen on Unix socket<br/>/etc/perouter/frr.socket<br/>Health endpoint :9080/healthz
    end

    rect rgb(55, 25, 60)
        Note right of ctrl: Phase 4: FRR Configuration & BGP Sessions
        ctrl->>ctrl: APItoFRR(): generate FRR config<br/>from CRD specs + nodeIndex<br/>New netns → omit preserve-fw-state
        ctrl->>reloader: POST config via Unix socket<br/>/etc/perouter/frr.socket
        reloader->>reloader: frr-reload.py --test (validate)
        reloader->>frr: frr-reload.py --reload (apply)
        reloader-->>ctrl: HTTP 200 OK

        frr->>frr: zebra discovers existing interfaces<br/>(VRFs, bridges, VXLANs, veths, lound)
        frr->>tor: BGP OPEN (underlay session)<br/>ASN 64514, capabilities: EVPN, GR
        tor-->>frr: BGP OPEN (accept)
        frr->>tor: EVPN Type-5 routes (L3VNI prefixes)<br/>EVPN Type-2 routes (MAC/IP for L2VNI)
        tor-->>frr: EVPN routes from fabric

        frr->>hostBGP: BGP OPEN (host session via veth)<br/>Per-L3VNI and/or passthrough sessions
        hostBGP-->>frr: BGP OPEN (accept)
        hostBGP->>frr: Host routes (CNI, services, VIPs)
        frr->>hostBGP: VNI routes from EVPN fabric
    end

    Note over netns,frr: System fully operational<br/>Data plane forwarding, control plane converged
```

#### FRR Container Crash & Recovery

```mermaid
sequenceDiagram
    autonumber
    participant kernel as Kernel<br/>(Data Plane)
    participant netns as Named Netns<br/>/var/run/netns/perouter
    participant ctrl as Controller<br/>(K8s DaemonSet)
    participant reloader as Reloader
    participant frr as FRR
    participant tor as ToR Switch
    participant hostBGP as Host BGP Speaker

    Note over kernel,hostBGP: System running normally — traffic flowing

    rect rgb(70, 25, 25)
        Note right of frr: FRR Process Dies
        frr->>frr: CRASH (segfault / OOM / kill)
        Note over reloader,frr: Router pod exits<br/>Both FRR and reloader are gone

        Note over tor: BGP session drops<br/>(TCP RST or hold timer expiry)
        Note over hostBGP: BGP session drops

        tor->>tor: BGP Graceful Restart activated<br/>Preserve stale routes for restart-time (120s)<br/>Mark routes as stale, keep forwarding
        hostBGP->>hostBGP: BGP Graceful Restart activated<br/>Preserve stale routes, keep forwarding
    end

    rect rgb(20, 55, 35)
        Note right of kernel: Data Plane Continues (zero disruption)
        Note over netns: Named netns persists!<br/>Held by bind mount, not by FRR process
        Note over kernel,netns: All kernel state intact in netns:
        Note over kernel: VRFs, bridges, VXLAN interfaces<br/>Veth pairs (pe ↔ host)<br/>Underlay NIC (eth1), lound (VTEP)<br/>Kernel routing tables (installed by zebra)<br/>Bridge FDB entries<br/>ARP/neighbor entries<br/>IP addresses on all interfaces

        kernel->>kernel: Continue forwarding packets<br/>using existing routes and FDB<br/>VXLAN encap/decap still works<br/>Inter-VRF routing still works
    end

    rect rgb(70, 45, 15)
        Note right of frr: Router Pod Restart (~1-2s)
        Note over frr: Kubelet or systemd detects failure<br/>and restarts the router pod
        frr->>netns: Re-enter named netns<br/>(Network=ns: or nsenter)
        activate frr
        activate reloader
        Note over frr: FRR re-enters the SAME named netns<br/>All interfaces already present and UP
    end

    rect rgb(20, 40, 70)
        Note right of frr: FRR Recovery (~2-5s)
        frr->>frr: Read /etc/frr/frr.conf<br/>Start zebra, bgpd, bfdd, staticd
        frr->>frr: zebra discovers existing interfaces<br/>VRFs, bridges, VXLANs all found intact
        frr->>frr: preserve-fw-state enabled<br/>(existing netns — kernel routes are valid,<br/>do NOT flush)
        reloader-->>reloader: Health endpoint :9080 ready
    end

    rect rgb(55, 25, 60)
        Note right of frr: BGP Session Re-establishment (~3-10s)
        frr->>tor: BGP OPEN with GR capability<br/>Indicates restart, requests route preservation
        tor-->>frr: BGP OPEN (accept restart)
        tor->>tor: Clear stale flag on preserved routes
        frr->>tor: Re-advertise EVPN routes<br/>(same routes as before crash)
        tor-->>frr: Send current fabric routes

        frr->>hostBGP: BGP OPEN with GR capability
        hostBGP-->>frr: BGP OPEN (accept restart)
        hostBGP->>hostBGP: Clear stale flag on preserved routes
        frr->>hostBGP: Re-advertise VNI routes
        hostBGP->>frr: Re-send host routes
    end

    rect rgb(40, 45, 50)
        Note right of ctrl: Controller Observes Recovery
        ctrl->>ctrl: CanReconcile() returns true<br/>(netns exists + health check passes)
        ctrl->>ctrl: Normal reconciliation resumes<br/>No interface re-creation needed<br/>(everything survived in netns)
    end

    Note over kernel,hostBGP: Full recovery complete<br/>Data plane: 0s outage<br/>Control plane: ~7-22s outage (bridged by GR)
```

#### Reconfiguration (New L2VNI CRD Created)

```mermaid
sequenceDiagram
    autonumber
    participant user as Operator
    participant k8s as K8s API
    participant ctrl as Controller
    participant netns as Named Netns<br/>/var/run/netns/perouter
    participant hostNS as Host Netns
    participant reloader as Reloader
    participant frr as FRR<br/>(zebra / bgpd)
    participant tor as ToR Switch

    Note over user,tor: System running with existing L3VNI config

    rect rgb(20, 40, 70)
        Note right of user: CRD Creation
        user->>k8s: kubectl apply -f l2vni-100.yaml<br/>VNI: 100, VRF: l2vni-100<br/>L2 Gateway: 10.100.0.1/24<br/>Host bridge: br-hs-100 (linux-bridge)
    end

    rect rgb(70, 45, 15)
        Note right of ctrl: Watch Event & Reconciliation Trigger
        k8s-->>ctrl: L2VNI watch event (CREATE)
        ctrl->>k8s: List all CRDs:<br/>Underlays, L3VNIs, L2VNIs, L3Passthroughs
        k8s-->>ctrl: Full config (includes new L2VNI-100)
        ctrl->>ctrl: Filter CRDs by node selector<br/>Merge with static config (if any)
        ctrl->>ctrl: Validate all resources<br/>(VNI uniqueness, IP formats, VRF names)
    end

    rect rgb(20, 55, 35)
        Note right of ctrl: Readiness Check
        ctrl->>ctrl: RouterNamedNSProvider.CanReconcile()<br/>Check /var/run/netns/perouter exists
        ctrl->>reloader: Health check :9080/healthz
        reloader-->>ctrl: 200 OK (FRR is running and healthy)
    end

    rect rgb(55, 25, 60)
        Note right of ctrl: Interface Configuration in Router Netns
        ctrl->>netns: setupVNI(VNI=100):<br/>1. netlink.LinkAdd(VRF "l2vni-100")<br/>2. netlink.LinkAdd(Bridge "br-pe-100")<br/>3. netlink.LinkAdd(VXLAN "vxlan-100"<br/>   VNI=100, VTEP=lound IP)<br/>4. Enslave vxlan-100 → br-pe-100<br/>5. Link set all UP

        ctrl->>ctrl: setupNamespacedVeth():<br/>Create veth pair<br/>vni-100-ns ↔ vni-100-host

        ctrl->>netns: Move vni-100-ns into router netns<br/>Assign IP to vni-100-ns<br/>Enslave vni-100-ns → br-pe-100<br/>Assign L2 gateway IP 10.100.0.1/24<br/>to br-pe-100

        ctrl->>hostNS: Keep vni-100-host in host netns<br/>Assign IP to vni-100-host<br/>Create host bridge br-hs-100<br/>(if autoCreate=true)<br/>Enslave vni-100-host → br-hs-100

        ctrl->>ctrl: Start BridgeRefresher(L2VNI-100)<br/>Periodic ARP probes every 60s<br/>from 10.100.0.1 on br-pe-100<br/>to refresh EVPN Type-2 routes
    end

    rect rgb(55, 50, 20)
        Note right of ctrl: FRR Configuration Update
        ctrl->>ctrl: APItoFRR(): regenerate full FRR config<br/>(underlay + all L3VNIs + passthrough)
        Note over ctrl: L2VNIs are data-plane only,<br/>no FRR BGP config needed for L2.<br/>EVPN auto-discovers VXLANs via zebra.

        ctrl->>reloader: POST updated config via Unix socket<br/>/etc/perouter/frr.socket
        reloader->>reloader: Write /etc/perouter/frr.conf
        reloader->>reloader: frr-reload.py --test (validate)
        reloader->>frr: frr-reload.py --reload (apply delta)
        reloader-->>ctrl: HTTP 200 OK
    end

    rect rgb(20, 40, 70)
        Note right of frr: FRR Discovers New VXLAN
        frr->>frr: zebra detects new vxlan-100 interface<br/>in VRF l2vni-100
        frr->>frr: EVPN auto-associates VNI 100<br/>with the VXLAN interface
        frr->>tor: EVPN Type-3 (IMET): advertise<br/>BUM replication for VNI 100
        frr->>tor: EVPN Type-2: advertise local<br/>MAC/IP entries from br-pe-100 FDB
        tor-->>frr: EVPN Type-2/3 from remote VTEPs<br/>for VNI 100
        frr->>frr: zebra installs remote FDB entries<br/>into br-pe-100 (VXLAN tunnel MACs)
    end

    rect rgb(40, 45, 50)
        Note right of ctrl: Cleanup
        ctrl->>ctrl: RemoveNonConfigured():<br/>Compare existing VNIs vs desired config<br/>Remove VNIs no longer in any CRD<br/>Stop their BridgeRefreshers
    end

    Note over hostNS: Workload pods can now attach to br-hs-100<br/>via Multus NetworkAttachmentDefinition<br/>(macvlan/bridge on br-hs-100)

    Note over user,tor: L2VNI-100 fully operational<br/>Workload ↔ br-hs-100 ↔ veth ↔ br-pe-100 ↔ vxlan-100 ↔ fabric
```

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

The FRR configuration for Graceful Restart depends on whether the router is
recovering into an **existing** netns (FRR crash) or a **new** netns (namespace
rebuild).

#### FRR Crash Recovery (existing netns)

When FRR restarts into the same named netns, all kernel routes and FDB entries
are still intact. The controller generates the config with `preserve-fw-state`
so that FRR does not flush these valid kernel routes on startup:

```
router bgp 64514
  bgp graceful-restart
  bgp graceful-restart preserve-fw-state
  bgp graceful-restart stalepath-time 120
  bgp graceful-restart restart-time 120
```

Peers preserve routes for up to 120s (restart-time) while FRR recovers.
`preserve-fw-state` tells FRR not to flush kernel routes on restart — this is
correct because the data plane state in the netns is still valid.

#### Namespace Rebuild Recovery (new netns)

When the netns was deleted and recreated, the old kernel routes are gone. If
`preserve-fw-state` were enabled in this scenario, FRR would tell its peers
that the old (now broken) routes are still valid, and peers would keep
forwarding traffic into a black hole until the stale path timer expires. The
controller **must not** set `preserve-fw-state` when it detects a new netns:

```
router bgp 64514
  bgp graceful-restart
  bgp graceful-restart stalepath-time 120
  bgp graceful-restart restart-time 120
```

Without `preserve-fw-state`, FRR flushes any stale kernel routes on startup
and peers withdraw stale routes as soon as the BGP session re-establishes,
allowing traffic to reconverge quickly on valid paths.

#### How the Controller Decides

The controller tracks whether the netns is new (just created by
`EnsureNamespace()`) or existing (already present when reconciliation started).
It passes this state to `APItoFRR()` when generating the FRR configuration:

- **Existing netns** → include `preserve-fw-state`
- **New netns** → omit `preserve-fw-state`

After FRR has fully converged and re-established all BGP sessions, subsequent
reconciliation loops generate the config with `preserve-fw-state` again, since
the netns is no longer new.

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

### Recovery from a Deleted or Emptied Namespace

In production, the most common support remedy for an out-of-sync node is to
trash the router namespace and let the system rebuild it. This can happen when:

- A controller bug leaves stale interfaces or misconfigured VRFs in the netns.
- A partial failure (e.g. OOM during reconciliation) leaves the netns half
  configured.
- Manual debugging (`ip link delete`, `ip netns exec ... ip route flush`)
  leaves the netns in an inconsistent state.
- An underlay change triggers `HandleNonRecoverableError`, which needs to
  rebuild the namespace from scratch.

The design must ensure this is a **safe, deterministic, single-command
operation** that always converges to the correct state.

#### Recovery Procedure

The operator deletes the netns to force a clean slate:

```bash
# Delete the netns (destroys all interfaces, routes, FDB inside it)
ip netns delete perouter
```

No further manual steps are required. On the next reconciliation loop, the
controller detects the missing netns, recreates it via `EnsureNamespace()`,
re-provisions all interfaces from CRD state, and restarts the FRR process.
The recovery is fully automatic once the netns is removed.

Equivalently, `HandleNonRecoverableError` can perform this programmatically
when it detects an unrecoverable divergence.

#### Why This Works

Deleting the named netns destroys all kernel objects inside it — VRFs, bridges,
VXLANs, veth endpoints (which also destroys the host-side peer), the underlay
NIC (returned to the host netns), and the `lound` dummy interface. This is a
clean slate. The system then follows the exact same sequence as a fresh start:

1. The controller detects the missing netns and calls `EnsureNamespace()` to
   create a new empty netns
2. The controller runs a full reconciliation — re-creating all interfaces from
   CRD state
3. The controller restarts the FRR process, which enters the new netns, finds
   interfaces configured by the controller, and re-establishes BGP sessions

The controller is already idempotent — it creates interfaces only if they don't
exist, and `RemoveNonConfigured()` cleans up anything not in the desired state.
An empty netns is simply the extreme case: nothing exists, everything gets
created.

#### Controller Detection

The controller calls `EnsureNamespace()` on every reconciliation loop. If
`/var/run/netns/perouter` does not exist, the controller recreates it. After
that, the normal reconciliation logic compares the data plane objects in the
netns against the desired VNI configuration from the Kubernetes API and
reconstructs any missing interfaces. No special detection heuristic is needed
— the existing idempotent reconciliation handles both a completely empty netns
and a partially degraded one the same way.

#### HandleNonRecoverableError Extension

The existing `HandleNonRecoverableError` restarts the FRR process. For the
named netns model, it can optionally also recreate the netns:

```go
func (r *RouterNamedNSProvider) HandleNonRecoverableError(ctx context.Context) error {
    slog.Info("recreating router namespace", "path", r.NamespacePath)

    // Delete the netns (destroys all kernel objects inside it)
    if err := exec.Command("ip", "netns", "delete", "perouter").Run(); err != nil {
        slog.Warn("netns delete failed (may already be gone)", "error", err)
    }

    // Recreate the netns (the controller owns this)
    if err := r.EnsureNamespace(ctx); err != nil {
        return fmt.Errorf("failed to recreate netns: %w", err)
    }

    // Restart the FRR process so it enters the new netns.
    // The mechanism is deployment-specific: systemd restart for quadlets,
    // pod deletion for K8s DaemonSets (kubelet recreates the pod).
    if err := r.restartRouter(ctx); err != nil {
        return fmt.Errorf("failed to restart router: %w", err)
    }

    return nil
}
```

#### Sequence Diagram: Namespace Deletion and Rebuild

```mermaid
sequenceDiagram
    autonumber
    participant op as Operator /<br/>HandleNonRecoverableError
    participant netns as Named Netns<br/>/var/run/netns/perouter
    participant ctrl as Controller
    participant k8s as K8s API
    participant reloader as Reloader
    participant frr as FRR
    participant tor as ToR Switch
    participant hostBGP as Host BGP Speaker

    Note over op,hostBGP: System in out-of-sync state<br/>(stale interfaces, wrong IPs, partial config)

    rect rgb(70, 25, 25)
        Note right of op: Phase 1: Tear Down
        op->>netns: ip netns delete perouter
        Note over netns: All kernel objects destroyed:<br/>VRFs, bridges, VXLANs, veths, lound<br/>Underlay NIC returned to host netns
        Note over tor: BGP sessions drop<br/>(underlay NIC gone)
        Note over hostBGP: BGP sessions drop<br/>(veth peers destroyed)
        tor->>tor: Graceful Restart activated<br/>Preserve stale routes
        hostBGP->>hostBGP: Graceful Restart activated<br/>Preserve stale routes
    end

    rect rgb(20, 40, 70)
        Note right of ctrl: Phase 2: Controller Recreates Netns
        ctrl->>ctrl: Detect missing netns<br/>(/var/run/netns/perouter gone)
        ctrl->>netns: EnsureNamespace():<br/>Create new netns + bind mount
        activate netns
        Note over netns: Fresh empty netns created<br/>New inode, no interfaces
    end

    rect rgb(20, 55, 35)
        Note right of ctrl: Phase 3: Controller Full Reconciliation
        ctrl->>k8s: List all CRDs:<br/>Underlays, L3VNIs, L2VNIs, L3Passthroughs
        k8s-->>ctrl: Full desired config

        ctrl->>netns: SetupUnderlay():<br/>Move physical NIC into new netns<br/>Create lound with VTEP IP
        ctrl->>netns: SetupL3VNI() for each L3VNI:<br/>Create VRF, bridge, VXLAN, veth
        ctrl->>netns: SetupL2VNI() for each L2VNI:<br/>Create VRF, bridge, VXLAN, veth,<br/>host-side bridge
        ctrl->>netns: SetupPassthrough() if configured

        Note over ctrl: All interfaces recreated from<br/>CRD state — guaranteed correct
    end

    rect rgb(70, 45, 15)
        Note right of ctrl: Phase 4: Restart FRR
        ctrl->>ctrl: restartRouter():<br/>Restart FRR process<br/>(systemd or pod recreation)
        ctrl->>frr: FRR enters new netns
        activate frr
        ctrl->>reloader: Reloader starts
        activate reloader
        Note over frr: FRR finds all interfaces<br/>already configured by controller
    end

    rect rgb(55, 25, 60)
        Note right of ctrl: Phase 5: FRR Configuration & BGP Recovery
        ctrl->>ctrl: APItoFRR(): new netns detected<br/>→ omit preserve-fw-state<br/>(old routes are gone, peers must withdraw stale paths)
        ctrl->>reloader: POST full FRR config via Unix socket
        reloader->>frr: frr-reload.py --reload
        reloader-->>ctrl: HTTP 200 OK

        frr->>frr: zebra discovers all interfaces<br/>Flush any stale kernel routes<br/>(preserve-fw-state not set)
        frr->>tor: BGP OPEN with GR capability
        tor-->>frr: BGP OPEN (accept restart)
        tor->>tor: Withdraw stale routes<br/>(old paths no longer valid)
        frr->>tor: Advertise EVPN routes<br/>(fresh routes from new netns)
        tor-->>frr: Fabric routes

        frr->>hostBGP: BGP OPEN with GR capability
        hostBGP-->>frr: BGP OPEN (accept restart)
        hostBGP->>hostBGP: Withdraw stale routes
        frr->>hostBGP: VNI routes
        hostBGP->>frr: Host routes
    end

    Note over op,hostBGP: System fully rebuilt from CRD state<br/>Guaranteed consistent — no stale artifacts
```

#### Recovery Timeline (Namespace Rebuild)

| Phase | Duration | What happens |
|-------|----------|-------------|
| Netns deletion | <1s | All kernel objects destroyed, clean slate |
| Netns recreation | <1s | Controller calls `EnsureNamespace()` |
| Controller reconciliation | ~2-5s | Full interface re-creation from CRDs |
| FRR restart | ~1-2s | FRR process re-enters new netns |
| FRR init + BGP recovery | ~5-15s | Config reload, session re-establishment |
| **Total outage** | **~10-25s** | |
| **Data plane outage** | **~10-25s** | Full outage (netns was destroyed) |

Unlike an FRR-only crash (0s data plane outage), a namespace rebuild **does
cause a data plane disruption** — this is the expected trade-off. The operator
is deliberately choosing to sacrifice continuity in exchange for a guaranteed
return to a correct state. This is strictly better than the current situation
where recovery from an out-of-sync state may require a full node reboot.

### Multus Integration

There are two distinct Multus use cases. They behave differently with this
proposal:

#### Multus for Underlay Connectivity (Router)

The router pod can optionally receive its underlay interface via a Multus
`NetworkAttachmentDefinition`, as an alternative to the controller moving a
physical NIC.

With either the quadlet, and the host networked approaches, the router cannot
integrate with Multus CNI, meaning Multus cannot attach interfaces to it
directly. However, the controller can create a macvlan/ipvlan interface from
the physical NIC and move it into the named netns for a similar (if less
flexible) user experience.

#### Multus for Workload Pods (L2VNI Secondary Interfaces)

**Completely unaffected.** Workload pods are still regular K8s pods. The host
bridges (`br-hs-{vni}`) are created by the controller in the host network
namespace. Multus still attaches macvlan/bridge interfaces to workload pods.
The data path is unchanged:

```
Workload Pod → macvlan on br-hs-{vni} (host netns) →
  veth → bridge in named netns → vxlan → underlay → remote VTEP
```

**NOTE:** if the network namespace is deleted as part of a remediation
procedure the workloads will be affected (the data-plane was removed).

### Router Deployment Model

The persistent named netns provides the same resilience guarantees regardless
of how FRR is deployed. The deployment model determines how the container is
managed, restarted, and how it enters the named netns. Each option has
different trade-offs around Kubernetes integration, operational tooling, and
underlay provisioning.

**Note on Multus underlay:** Neither deployment model supports Multus for
underlay connectivity. Both options run the router outside of the standard CNI
chain (quadlet is not a K8s pod; hostNetwork pods skip CNI entirely), so Multus
cannot attach interfaces to the router. In all cases, the controller provisions
the underlay directly — e.g. moving the physical NIC or creating a
macvlan/ipvlan interface and placing it into the named netns.

#### Option A: Podman Quadlet

FRR runs as a Podman container managed by systemd. The quadlet's
`Network=ns:/var/run/netns/perouter` directive tells Podman to run the
container inside the named netns. The controller creates the netns as part of
its reconciliation loop, before FRR starts.

##### FRR Quadlet Configuration

The key change in `frr.container`:

```ini
[Container]
# Join the persistent named netns instead of getting an ephemeral one:
Network=ns:/var/run/netns/perouter
```

When the container starts, Podman runs it inside `/var/run/netns/perouter`.
When the container dies, the netns stays. When systemd restarts the container,
it re-enters the same netns with all interfaces intact.

**Pros:**

- **Native systemd lifecycle**: `RestartSec=1`, `WatchdogSec`, and systemd
  dependency ordering give fast, reliable restarts with no Kubernetes API
  involvement.
- **No Kubernetes dependency for the data plane**: the router starts at boot
  via systemd, even before kubelet or the API server are available. This
  enables boot-time routing for infrastructure nodes.
- **Simple netns joining**: `Network=ns:` is a first-class Podman feature — no
  `nsenter` wrapper, no privileged init containers.
- **Drain-safe**: `kubectl drain` does not touch the router. Traffic continues
  flowing while workload pods are being drained/migrated.
- **Existing implementation**: OpenPERouter already ships quadlet definitions
  under `systemdmode/quadlets/`.

**Cons:**

- **Out-of-band management**: operators need SSH or equivalent access to run
  `systemctl` commands; standard `kubectl` tooling does not apply.
- **No K8s-native observability**: no pod status, events, or resource metrics
  from kubelet. Monitoring relies on systemd journal, node-level exporters,
  or custom health checks.

#### Option B: Kubernetes hostNetwork Pod

FRR runs as a container in a K8s DaemonSet pod with `hostNetwork: true`. The
controller creates the named netns as part of its reconciliation loop — the
same component that already provisions interfaces, routes, and FDB entries
inside the netns also ensures the netns exists before populating it. The FRR
container's entrypoint wraps the actual process with
`nsenter --net=/var/run/netns/perouter`. The pod requires `privileged: true`
or `CAP_SYS_ADMIN` for the `nsenter` call.

Because the controller owns the netns lifecycle, the FRR pod does not need
init containers. The controller creates the netns, provisions interfaces, and
only then does the FRR container enter it via `nsenter`. If the netns does not
yet exist when FRR starts, the container simply fails and kubelet restarts it
— by which time the controller has created the netns.

**Pros:**

- **Standard K8s lifecycle**: managed by a DaemonSet controller, with pod
  status, events, resource requests/limits, and rolling updates via the K8s
  API.
- **Familiar operational tooling**: `kubectl logs`, `kubectl describe pod`,
  `kubectl rollout restart` all work as expected.
- **K8s-native observability**: pod metrics, liveness/readiness probes, and
  events are all available.
- **No init containers**: the controller creates the netns, eliminating the
  need for privileged init containers or host-level systemd units to
  bootstrap the namespace.
- **Drain interaction**: the network namespace where the data-plane is
  configured will survive a node drain.

**Cons:**

- **Requires nsenter wrapper**: the FRR entrypoint must be wrapped to switch
  into the named netns. This adds a layer of indirection and requires
  privileged security context.
- **K8s API dependency**: if the API server is unavailable, the DaemonSet
  controller cannot reconcile the pod. Existing pods continue running, but
  crashed pods may not be restarted until the API recovers (kubelet can
  restart containers within a pod, but cannot recreate deleted pods without
  the API server).
- **Controller ordering**: the controller must create the netns before FRR
  can enter it. If the controller is slow to start or its reconciliation is
  delayed, the FRR container will crash-loop until the netns appears. This
  is self-healing (kubelet retries with backoff) but may delay initial
  convergence.

#### Comparison

| Aspect | Podman Quadlet | hostNetwork Pod                                    |
|--------|---------------|----------------------------------------------------|
| **Netns joining** | `Network=ns:` (native) | `nsenter` wrapper                                  |
| **K8s API dependency** | None | DaemonSet controller                               |
| **kubectl tooling** | No (`systemctl`) | Yes                                                |
| **Privileged required** | Container capabilities | `CAP_SYS_ADMIN` / privileged                       |
| **Drain behavior** | Unaffected | Unaffected (net namespace survives the node drain) |
| **Boot-time routing** | Yes (systemd) | No (needs API server)                              |
| **Implementation maturity** | Existing quadlets | Requires nsenter wrapper                           |
| **Observability** | systemd journal | K8s pod metrics/events                             |

#### Recommendation

We recommend **Option B (Kubernetes hostNetwork Pod)** for simplicity. It keeps
both the controller and the router within the Kubernetes lifecycle, giving
operators a single management plane (`kubectl`) for all components. The
`nsenter` wrapper is straightforward to implement, and the controller already
handles netns creation and interface provisioning regardless of the deployment
model.

### Recovery Timeline (router restart)

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
| Netns creation | Implicit (pod/container lifecycle) | Controller creates via `EnsureNamespace()` | New `RouterProvider` method |
| Router pod | Own netns | `hostNetwork: true` + `nsenter` into named netns | nsenter wrapper |
| Controller netns discovery | CRI query or PID file | Well-known path | New `RouterProvider` impl (trivial) |
| Interface configuration | Waits for pod ready | Can pre-configure netns | Logic simplification |
| Multus for underlay | Optional (annotation) | Controller-provisioned (macvlan/ipvlan) | Minor refactor |
| Multus for workloads | Via host bridges | Unchanged | None |
| Cluster CNI interface | Created but unused | Not created (hostNetwork) | Cleaner |
| Health monitoring | K8s probes | K8s probes (hostNetwork pod) | Unchanged |
| `HandleNonRecoverableError` | Delete pod | Delete netns + `EnsureNamespace()` + restart router | Minor |

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
- **Namespace rebuild tests**: Delete the netns while the system is running and
  verify:
  - The controller detects the empty/new netns and runs full reconciliation.
  - All interfaces are recreated correctly from CRD state.
  - FRR re-enters the new netns and re-establishes BGP sessions.
  - No stale artifacts remain from the previous namespace.
  - `HandleNonRecoverableError` can perform the full rebuild programmatically.
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
- **Router loses Kubernetes lifecycle**: Since the router would no longer be
  executed as a pod, it would stop benefiting from the Kubernetes lifecycle
  management. Operators would now need to manually stop the router during
  decomission.

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
