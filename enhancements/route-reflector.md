# Enhancement: Internal iBGP Route Reflector

## Summary

In environments where the external network does not support distributing routes between router pods via the fabric or can't be changed to do so, the only option is a full-mesh iBGP topology, which does not scale.

This enhancement adds an internal iBGP Route Reflector for route distribution between router pods:

- **East/West**: iBGP via an internal RR. Data plane goes directly between nodes.
- **North/South**: eBGP with the ToR for IPv4/IPv6 (unchanged).

No dedicated FRR pods are added. The hostcontroller natively configures the local FRR process as a route reflector when its Underlay CR contains a `routeReflector` section. Users add the RR node IPs as iBGP neighbors in client Underlay CRs to enable east/west — no new CRDs.

## Motivation

- **Cloud environments**: the ToR cannot be used for east/west traffic
- **Hybrid deployments**: depending on the on-prem ToR for route reflection adds latency and cost to every East/West flow
- **Full-mesh iBGP**: N*(N-1)/2 sessions, does not scale

### Goals

- Enable East/West without relying on the ToR
- Keep East/West data plane traffic within the cluster (no hairpin through the ToR)
- HA via multiple RR nodes (typically control plane nodes, 3 in production)
- No new CRDs — opt-in by adding a `routeReflector` section to the RR nodes' Underlay CR and adding RR node IPs as iBGP neighbors in client Underlay CRs

### Non-Goals

- Exposing RR peering to external routers

## Dependencies

- **[PR #260](https://github.com/openperouter/openperouter/pull/260) — iBGP support**: Adds the `PeerASN` type with `"internal"`/`"external"` modes, conditional `allowas-in` (eBGP only) and `next-hop-self force` (iBGP only) in FRR templates, and removes validation that blocked same-ASN neighbors. This enhancement builds on top of PR #260's iBGP support for client-to-RR and inter-RR peering.

## Design

The primary use case for the internal RR is environments where the ToR cannot be used for east/west traffic. In cloud deployments, control plane and worker nodes are commonly placed on different subnets. The examples throughout this document use this multi-subnet topology as it is the most complex case and the one the RR is originally designed to address.

### How It Works

When the Underlay CR contains a `routeReflector` section, the hostcontroller generates the RR-specific FRR configuration (`bgp listen range`, `route-reflector-client` peer-group, `bgp cluster-id`, inter-RR peers) through the existing template and conversion pipeline.

**Deployment**: Any node with a router pod whose Underlay CR contains a `routeReflector` section becomes an RR. The user is responsible for ensuring the router DaemonSet is scheduled on those nodes (e.g., via tolerations). No separate DaemonSet is ever created.

RR nodes get their own Underlay CR with a `routeReflector` section. Its contents depend on whether the RR nodes also participate in the data plane (ToR + full routing) or only do route reflection (NIC + ASN only, no ToR).

**Client configuration**: Users configure the openperouter instances to connect to the instances configured as route reflectors. The FRR templates already handle iBGP correctly — `allowas-in` is emitted only for eBGP neighbors.

**Inter-RR peering**: Users list all RR node IPs as `type: internal` neighbors in the RR Underlay CR. The hostcontroller on each RR node filters out its own IP and configures the remaining entries as explicit iBGP neighbors. This ensures a full mesh between all RRs. FRR explicit `neighbor` takes precedence over a matching `bgp listen range`, so inter-RR sessions use explicit peering.

**`bgp listen range`**: Users explicitly configure all subnets from which iBGP clients may connect via `listenRanges` in the RR Underlay CR. No auto-derivation — all ranges are explicit, which keeps the hostcontroller simple and supports multi-NIC / multi-subnet environments.

### Session Topology

```
            ToR (eBGP)
               |
  +------------+------------+
  |            |            |
cp-1 (RR) <--iBGP--> cp-2 (RR)       (explicit full mesh between RRs)
  ^  bgp listen range       ^
  |  route-reflector-client  |
  +----------+--------------+
             |
   worker-1 (client)    worker-2 (client)    (connect to all RRs)
```

- **Clients -> RRs**: clients configure RR IPs as explicit iBGP neighbors via the Underlay CR. RRs accept passively via `bgp listen range` covering all underlay subnets.
- **RR <-> RR**: explicit iBGP peers from the RR Underlay CR (each hostcontroller filters out its own IP). Not via listen range.
- **All nodes -> ToR**: eBGP unchanged, `allowas-in` applies only to eBGP neighbors.
- **Path selection**: iBGP (local-preference 100) beats the longer eBGP AS path, so VXLAN goes directly between nodes rather than hairpinning through the ToR.
- **RR-only nodes**: when RR nodes do not participate in the data plane, they do NOT have a ToR eBGP session — they only do route reflection.

### Example: 4-Node Dual-Stack Cluster

Setup: ASN 64514, ToR at 192.168.11.2 / fd00:11::2 (ASN 64512). Control plane nodes are on a separate subnet (192.168.10.0/24, fd00:10::/64) from workers (192.168.11.0/24, fd00:11::/64), as is typical in cloud environments.

| Node | Role | Underlay NIC IPs | VTEP IP |
|------|------|-----------------|---------|
| cp-1 | RR (control plane) | 192.168.10.3/24, fd00:10::3/64 | 100.65.0.0 |
| cp-2 | RR (control plane) | 192.168.10.4/24, fd00:10::4/64 | 100.65.0.1 |
| worker-1 | client | 192.168.11.5/24, fd00:11::5/64 | 100.65.0.2 |
| worker-2 | client | 192.168.11.6/24, fd00:11::6/64 | 100.65.0.3 |

#### Underlay CRs — CP nodes with full routing

When control plane nodes participate in the data plane, they get a full Underlay with ToR. Workers include the control plane RR IPs as iBGP neighbors. The control plane Underlay lists all CP node IPs as `type: internal` neighbors for inter-RR peering — each hostcontroller filters out its own IP:

```yaml
# Worker Underlay
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: Underlay
metadata:
  name: underlay-workers
  namespace: openperouter-system
spec:
  nodeSelector:
    matchExpressions:
      - key: node-role.kubernetes.io/control-plane
        operator: DoesNotExist
  asn: 64514
  evpn:
    vtepcidr: 100.65.0.0/24
  nics:
    - toswitch
  neighbors:
    - asn: 64512                       # ToR (eBGP, IPv4)
      address: 192.168.11.2
    - asn: 64512                       # ToR (eBGP, IPv6)
      address: fd00:11::2
    - type: internal                   # cp-1 RR (iBGP, IPv4)
      address: 192.168.10.3
    - type: internal                   # cp-1 RR (iBGP, IPv6)
      address: fd00:10::3
    - type: internal                   # cp-2 RR (iBGP, IPv4)
      address: 192.168.10.4
    - type: internal                   # cp-2 RR (iBGP, IPv6)
      address: fd00:10::4
---
# Control plane Underlay (full routing + RR overlay)
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: Underlay
metadata:
  name: underlay-controlplane
  namespace: openperouter-system
spec:
  nodeSelector:
    matchLabels:
      node-role.kubernetes.io/control-plane: ""
  asn: 64514
  evpn:
    vtepcidr: 100.65.0.0/24
  nics:
    - toswitch
  routeReflector:
    listenRanges:
      - 192.168.10.0/24
      - 192.168.11.0/24
      - fd00:10::/64
      - fd00:11::/64
  neighbors:
    - asn: 64512                       # ToR (eBGP, IPv4)
      address: 192.168.10.2
    - asn: 64512                       # ToR (eBGP, IPv6)
      address: fd00:10::2
    - type: internal                   # cp-1 (iBGP, IPv4, filtered on cp-1)
      address: 192.168.10.3
    - type: internal                   # cp-1 (iBGP, IPv6, filtered on cp-1)
      address: fd00:10::3
    - type: internal                   # cp-2 (iBGP, IPv4, filtered on cp-2)
      address: 192.168.10.4
    - type: internal                   # cp-2 (iBGP, IPv6, filtered on cp-2)
      address: fd00:10::4
```

#### Underlay CRs — CP nodes as RR only

When control plane nodes only do route reflection (no data plane), workers get the ToR + RR neighbors. Control plane nodes get an Underlay with NIC, ASN, and all CP node IPs for inter-RR peering (no ToR):

```yaml
# Worker Underlay
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: Underlay
metadata:
  name: underlay-workers
  namespace: openperouter-system
spec:
  nodeSelector:
    matchExpressions:
      - key: node-role.kubernetes.io/control-plane
        operator: DoesNotExist
  asn: 64514
  evpn:
    vtepcidr: 100.65.0.0/24
  nics:
    - toswitch
  neighbors:
    - asn: 64512                       # ToR (eBGP, IPv4)
      address: 192.168.11.2
    - asn: 64512                       # ToR (eBGP, IPv6)
      address: fd00:11::2
    - type: internal                   # cp-1 RR (iBGP, IPv4)
      address: 192.168.10.3
    - type: internal                   # cp-1 RR (iBGP, IPv6)
      address: fd00:10::3
    - type: internal                   # cp-2 RR (iBGP, IPv4)
      address: 192.168.10.4
    - type: internal                   # cp-2 RR (iBGP, IPv6)
      address: fd00:10::4
---
# Control plane Underlay (RR only, no data plane)
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: Underlay
metadata:
  name: underlay-controlplane
  namespace: openperouter-system
spec:
  nodeSelector:
    matchLabels:
      node-role.kubernetes.io/control-plane: ""
  asn: 64514
  nics:
    - toswitch
  routeReflector:
    listenRanges:
      - 192.168.10.0/24
      - 192.168.11.0/24
      - fd00:10::/64
      - fd00:11::/64
  neighbors:
    - type: internal                   # cp-1 (iBGP, IPv4, filtered on cp-1)
      address: 192.168.10.3
    - type: internal                   # cp-1 (iBGP, IPv6, filtered on cp-1)
      address: fd00:10::3
    - type: internal                   # cp-2 (iBGP, IPv4, filtered on cp-2)
      address: 192.168.10.4
    - type: internal                   # cp-2 (iBGP, IPv6, filtered on cp-2)
      address: fd00:10::4
```

#### Key FRR Additions on RR Nodes

The hostcontroller generates the following RR-specific FRR stanzas through the template and conversion pipeline. These are added on top of the normal router configuration (or standalone for RR-only nodes):

**Cluster ID** — identifies the RR cluster for loop prevention (RFC 4456):

```
bgp cluster-id 10.0.0.1
```

**Dynamic client acceptance** — a peer group with `bgp listen range` so RR nodes passively accept iBGP sessions from clients on any configured subnet, without listing each client explicitly:

```
neighbor CLIENTS peer-group
neighbor CLIENTS remote-as internal
bgp listen range 192.168.10.0/24 peer-group CLIENTS
bgp listen range 192.168.11.0/24 peer-group CLIENTS
bgp listen range fd00:10::/64 peer-group CLIENTS
bgp listen range fd00:11::/64 peer-group CLIENTS
```

**Route reflection** — marks the CLIENTS peer group as RR clients in the L2VPN EVPN address family so the RR reflects EVPN routes to them:

```
address-family l2vpn evpn
  neighbor CLIENTS activate
  neighbor CLIENTS route-reflector-client
exit-address-family
```

**Explicit inter-RR peering** — full mesh between RR nodes via explicit neighbors (each hostcontroller filters out its own IP). Explicit `neighbor` takes precedence over a matching `bgp listen range`:

```
neighbor 192.168.10.4 remote-as internal
neighbor fd00:10::4 remote-as internal
```

**RR-only nodes**: when CP nodes only do route reflection (no data plane), the config contains only the RR stanzas above — no ToR eBGP session, no VTEP, no VNI.

#### Client Nodes

Client nodes use standard iBGP neighbors pointing at the RR IPs (configured via the Underlay CR). The existing iBGP support from PR #260 handles these — `allowas-in` is emitted only for eBGP neighbors, `next-hop-self force` only for iBGP. No RR-specific FRR stanzas are needed on clients.

#### BGP sessions at runtime — CP node as RR + normal router (cp-1)

```
L2VPN EVPN Summary (BGP router identifier 10.0.0.1, local AS 64514):

Neighbor        V    AS   Up/Down  State/PfxRcd  Desc
*192.168.11.5   4  64514  00:25:56           5   <- dynamic client (worker-1, IPv4)
*192.168.11.6   4  64514  00:25:56           2   <- dynamic client (worker-2, IPv4)
*fd00:11::5     4  64514  00:25:56           5   <- dynamic client (worker-1, IPv6)
*fd00:11::6     4  64514  00:25:56           2   <- dynamic client (worker-2, IPv6)
192.168.10.2    4  64512  00:25:56           9   <- ToR (eBGP, IPv4)
fd00:10::2      4  64512  00:25:56           9   <- ToR (eBGP, IPv6)
192.168.10.4    4  64514  00:25:56           9   <- other RR cp-2 (explicit, IPv4)
fd00:10::4      4  64514  00:25:56           9   <- other RR cp-2 (explicit, IPv6)

* = dynamic neighbor accepted via bgp listen range
```

#### BGP sessions at runtime — CP node as RR only (cp-1)

```
L2VPN EVPN Summary (BGP router identifier 10.0.0.1, local AS 64514):

Neighbor        V    AS   Up/Down  State/PfxRcd  Desc
*192.168.11.5   4  64514  00:25:56           5   <- dynamic client (worker-1, IPv4)
*192.168.11.6   4  64514  00:25:56           2   <- dynamic client (worker-2, IPv4)
*fd00:11::5     4  64514  00:25:56           5   <- dynamic client (worker-1, IPv6)
*fd00:11::6     4  64514  00:25:56           2   <- dynamic client (worker-2, IPv6)
192.168.10.4    4  64514  00:25:56           9   <- other RR cp-2 (explicit, IPv4)
fd00:10::4      4  64514  00:25:56           9   <- other RR cp-2 (explicit, IPv6)

* = dynamic neighbor accepted via bgp listen range
```

#### BGP sessions at runtime — client node (worker-1)

```
L2VPN EVPN Summary (BGP router identifier 10.0.0.2, local AS 64514):

Neighbor        V    AS   Up/Down  State/PfxRcd  Desc
192.168.11.2    4  64512  00:25:56           9   <- ToR (eBGP, IPv4)
fd00:11::2      4  64512  00:25:56           9   <- ToR (eBGP, IPv6)
192.168.10.3    4  64514  00:25:56           9   <- RR cp-1 (IPv4)
fd00:10::3      4  64514  00:25:56           9   <- RR cp-1 (IPv6)
192.168.10.4    4  64514  00:25:56           9   <- RR cp-2 (IPv4)
fd00:10::4      4  64514  00:25:56           9   <- RR cp-2 (IPv6)
```

### Controller Reconcile Logic

**Hostcontroller** (existing, extended with RR logic):

- Watches: Nodes, Underlays, L2VNIs, L3VNIs, Pods (router)
- When the Underlay CR contains a `routeReflector` section, the hostcontroller additionally:
  - Filters out neighbors matching its own IP (self-filtering for inter-RR peering)
  - Generates `bgp listen range` entries from the explicit `listenRanges`, `route-reflector-client`, `bgp cluster-id` — all through the existing FRR template and conversion pipeline
- Processes Underlay neighbors as before (including iBGP neighbors to other RR IPs and client RR IPs from the Underlay CR)

### Failure and Recovery

RR nodes are typically stable (e.g., control plane nodes) and do not get rescheduled. DaemonSets restart pods on the same node.

**RR node goes down** (e.g., cp-1 fails):

1. **Client nodes**: iBGP sessions to cp-1 time out (based on hold-time, or faster with BFD). Sessions to cp-2 (surviving RR) stay up — **no traffic disruption**.
2. **cp-2 RR**: inter-RR session to cp-1 drops. cp-2 continues reflecting routes from all clients.
3. **When cp-1 recovers**: DaemonSet recreates the router pod, FRR restarts, sessions re-establish.

**Router pod crash on RR node** (node is fine):

1. DaemonSet restarts the pod. The hostcontroller regenerates the RR configuration on startup.
2. Brief BGP session flap during restart.

Convergence is bounded by BGP hold-time (default 180s, or less with BFD) for session failover, and pod restart time for recovery.

### Systemd Mode

No special handling is needed. All RR logic lives in the Underlay spec, and the static configuration files use the same `spec` schema as the Kubernetes CRs. The hostcontroller processes the `routeReflector` section through the same conversion and template pipeline regardless of whether the Underlay comes from a static file or the API server.

### Testing

**Unit tests** (`internal/frr/`, `internal/conversion/`):

- FRR template rendering: RR stanzas (`bgp listen range`, `route-reflector-client`, `bgp cluster-id`, inter-RR peers) rendered correctly (golden file tests)
- iBGP `allowas-in` / `next-hop-self force` handling already covered by PR #260 tests

**E2e tests** (`e2etests/tests/`, Ginkgo suite against a 4-node clab cluster):

- **RR configuration lifecycle**: add `routeReflector` section to RR Underlay -> verify RR stanzas present in FRR config on matching nodes
- **Underlay iBGP neighbors**: verify that adding RR node IPs as `type: internal` neighbors in client Underlay CR produces correct FRR config
- **BGP sessions**: all iBGP sessions to RR nodes established, dynamic clients accepted via `bgp listen range` (`*` prefix in vtysh output)
- **Multi-subnet listen range**: verify that `bgp listen range` covers all configured `listenRanges`
- **Route reflection**: routes on client nodes show `*>i` (iBGP best path from RR), not the eBGP path from the ToR
- **East/West data plane**: ping between workloads on different client nodes, VXLAN packets go directly node-to-node (zero packets via ToR IP)
- **Failover**: delete one router pod on an RR node -> verify DaemonSet recreates it, surviving RR keeps sessions up, clients reconverge
- **RR-only nodes**: verify Underlay with `routeReflector` and no ToR produces RR-only FRR config, route reflection works

## Alternatives Considered

- **Standalone FRR RR Pods**: requires cluster-to-underlay routing, adds separate FRR processes, pod IPs unstable
- **Full-mesh iBGP**: N*(N-1)/2 sessions, does not scale
- **FRR-K8s as RR**: cross-system coordination, upgrade risk
- **Scheduler-placed RR Deployment**: 2-replica Deployment with anti-affinity landing on scheduler-selected nodes. Required auto-discovery mechanism (hostcontroller listing RR controller pods, reading IP annotations, extending the pod cache). Complex rescheduling logic when pods get evicted. Replaced by Underlay-driven RR configuration for simplicity.
- **Auto-discovery of RR IPs**: hostcontroller listing RR controller pods and reading annotations to discover RR node IPs dynamically. Replaced by explicit Underlay CR neighbor configuration for both client-to-RR and inter-RR peering — users list all RR IPs as `type: internal` neighbors, each hostcontroller filters out its own IP. Reduces controller complexity and eliminates cross-pod watches.

## References

- RFC 4456 — BGP Route Reflection
- FRR `bgp listen range` — https://docs.frrouting.org/en/latest/bgp.html
- PoC branch — https://github.com/qinqon/openperouter/tree/poc-router-reflector
