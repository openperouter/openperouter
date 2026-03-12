# Enhancement: Internal iBGP Route Reflector

## Summary

In environments where the external network does not support EVPN or can't be changed to support it or allow east/west, OpenPERouter cannot distribute EVPN routes between router pods via the fabric. Without an alternative the only option is a full-mesh iBGP topology, which does not scale.

This enhancement adds an internal iBGP Route Reflector for EVPN distribution between router pods:

- **East/West**: iBGP EVPN via an internal RR. VXLAN data plane goes directly between nodes.
- **North/South**: eBGP with the ToR for IPv4/IPv6 (unchanged).

No dedicated FRR pods are added. A lightweight **RR controller** Deployment (2 replicas) configures the existing router pod FRR process on two nodes as route reflectors. Kubernetes scheduling determines which nodes are RRs — no leader election, no new CRDs, no API changes.

## Motivation

- **Cloud environments**: managed routers typically do not support EVPN
- **Hybrid deployments**: depending on the on-prem ToR for route reflection adds latency and cost to every East/West flow
- **Full-mesh iBGP**: N*(N-1)/2 sessions, does not scale

### Goals

- Enable East/West EVPN without requiring EVPN support from the external fabric
- Keep East/West data plane traffic within the cluster (no hairpin through the ToR)
- HA without leader election
- No new API types — opt-in by deploying the RR controller

### Non-Goals

- Exposing RR peering to external routers
- Dedicated standalone FRR RR pods

## Design

### How It Works

Each hostcontroller reads the underlay NIC IPs (IPv4 and/or IPv6, with mask) from inside the router pod network namespace and writes them as a JSON array to the `openperouter.io/underlay-ips` annotation on the router pod (e.g. `["192.168.11.3/24","fd00::3/64"]`).

> **Note:** This annotation is a temporary discovery mechanism. It is expected to be replaced by a per-node Underlay status subresource.

The RR controller is a 2-replica Deployment. Each replica lands on a different node (anti-affinity) that runs a router pod (affinity). It reads the local router pod's `underlay-ips` annotation and creates a `RawFRRConfig` with `bgp listen range` (one per address family, derived by zeroing the host bits) and `route-reflector-client`.

To discover RR nodes, each hostcontroller lists RR controller pods (`app=openperouter-rr-controller`), finds the router pod on each RR controller's node via `spec.nodeName`, reads its `underlay-ips` annotation, and adds them as iBGP neighbors. On RR nodes it merges the `RawFRRConfig` into the FRR config. No node labels or node annotations are required — the RR controller pod presence is the signal.

Deploying the RR controller is the only opt-in. No changes to the Underlay CR are needed.

### Session Topology

```
            ToR (eBGP)

  RR node A ←—— iBGP ——→ RR node B       (explicit neighbors, full mesh)
       ↑  bgp listen range         ↑
       |  route-reflector-client    |
       └────────────┬───────────────┘
                    │
          client C          client D      (connect to both RRs)
```

- **Clients → RRs**: clients initiate, RRs accept passively via `bgp listen range` on the underlay NIC subnet
- **RR ↔ RR**: explicit iBGP peers, not via listen range (FRR explicit `neighbor` takes precedence over a matching `bgp listen range`)
- **All nodes → ToR**: eBGP unchanged, `allowas-in` applies only to eBGP neighbors
- **Path selection**: iBGP (local-preference 100) beats the longer eBGP AS path, so VXLAN goes directly between nodes rather than hairpinning through the ToR

### Example: 4-Node Dual-Stack Cluster

Setup: ASN 64514, ToR at 192.168.11.2 / fd00::2 (ASN 64512).

| Node | Role | Underlay NIC IPs | VTEP IP |
|------|------|-----------------|---------|
| control-plane | RR | 192.168.11.3/24, fd00::3/64 | 100.65.0.0 |
| worker | client | 192.168.11.4/24, fd00::4/64 | 100.65.0.1 |
| worker2 | client | 192.168.11.5/24, fd00::5/64 | 100.65.0.2 |
| worker3 | RR | 192.168.11.6/24, fd00::6/64 | 100.65.0.3 |

Router pod annotation on control-plane:
```
openperouter.io/underlay-ips: '["192.168.11.3/24","fd00::3/64"]'
```

#### RawFRRConfig (created by RR controller on control-plane)

```yaml
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: RawFRRConfig
metadata:
  name: rr-pe-kind-control-plane
  namespace: openperouter-system
spec:
  nodeSelector:
    matchLabels:
      kubernetes.io/hostname: pe-kind-control-plane
  priority: 100
  rawConfig: |
    router bgp 64514
      bgp cluster-id 10.0.0.1
      neighbor CLIENTS peer-group
      neighbor CLIENTS remote-as 64514
      bgp listen range 192.168.11.0/24 peer-group CLIENTS
      bgp listen range fd00::/64 peer-group CLIENTS
      address-family l2vpn evpn
        neighbor CLIENTS activate
        neighbor CLIENTS route-reflector-client
      exit-address-family
```

#### Resulting frr.conf — RR node (control-plane)

The hostcontroller merges the RawFRRConfig into the main config. FRR combines both `router bgp` stanzas:

```
router bgp 64514
  bgp router-id 10.0.0.1
  no bgp ebgp-requires-policy
  no bgp default ipv4-unicast
  bgp cluster-id 10.0.0.1
  no bgp network import-check

  neighbor CLIENTS peer-group
  neighbor CLIENTS remote-as 64514
  neighbor 192.168.11.2 remote-as 64512          ! eBGP to ToR (IPv4)
  neighbor fd00::2 remote-as 64512               ! eBGP to ToR (IPv6)
  neighbor 192.168.11.6 remote-as 64514          ! explicit iBGP to other RR (IPv4)
  neighbor fd00::6 remote-as 64514               ! explicit iBGP to other RR (IPv6)
  bgp listen range 192.168.11.0/24 peer-group CLIENTS
  bgp listen range fd00::/64 peer-group CLIENTS

  address-family ipv4 unicast
    network 100.65.0.0/32
    neighbor 192.168.11.2 activate
    neighbor 192.168.11.2 allowas-in             ! eBGP only
  exit-address-family

  address-family l2vpn evpn
    neighbor CLIENTS activate
    neighbor CLIENTS route-reflector-client
    neighbor 192.168.11.2 activate
    neighbor 192.168.11.2 allowas-in             ! eBGP only
    neighbor fd00::2 activate
    neighbor fd00::2 allowas-in                  ! eBGP only
    neighbor 192.168.11.6 activate               ! other RR, no allowas-in
    neighbor fd00::6 activate                    ! other RR, no allowas-in
    advertise-all-vni
    advertise-svi-ip
  exit-address-family
```

#### Resulting frr.conf — client node (worker)

```
router bgp 64514
  bgp router-id 10.0.0.2
  no bgp ebgp-requires-policy
  no bgp default ipv4-unicast
  no bgp network import-check

  neighbor 192.168.11.2 remote-as 64512          ! eBGP to ToR (IPv4)
  neighbor fd00::2 remote-as 64512               ! eBGP to ToR (IPv6)
  neighbor 192.168.11.3 remote-as 64514          ! iBGP to RR (IPv4)
  neighbor fd00::3 remote-as 64514               ! iBGP to RR (IPv6)
  neighbor 192.168.11.6 remote-as 64514          ! iBGP to RR (IPv4)
  neighbor fd00::6 remote-as 64514               ! iBGP to RR (IPv6)

  address-family ipv4 unicast
    network 100.65.0.1/32
    neighbor 192.168.11.2 activate
    neighbor 192.168.11.2 allowas-in             ! eBGP only
  exit-address-family

  address-family l2vpn evpn
    neighbor 192.168.11.2 activate
    neighbor 192.168.11.2 allowas-in             ! eBGP only
    neighbor fd00::2 activate
    neighbor fd00::2 allowas-in                  ! eBGP only
    neighbor 192.168.11.3 activate               ! RR, no allowas-in
    neighbor fd00::3 activate
    neighbor 192.168.11.6 activate               ! RR, no allowas-in
    neighbor fd00::6 activate
    advertise-all-vni
    advertise-svi-ip
  exit-address-family
```

#### BGP sessions at runtime — RR node (control-plane)

```
L2VPN EVPN Summary (BGP router identifier 10.0.0.1, local AS 64514):

Neighbor        V    AS   Up/Down  State/PfxRcd  Desc
*192.168.11.4   4  64514  00:25:56           5   ← dynamic client (worker, IPv4)
*192.168.11.5   4  64514  00:25:56           2   ← dynamic client (worker2, IPv4)
*fd00::4        4  64514  00:25:56           5   ← dynamic client (worker, IPv6)
*fd00::5        4  64514  00:25:56           2   ← dynamic client (worker2, IPv6)
192.168.11.2    4  64512  00:25:56           9   ← ToR (eBGP, IPv4)
fd00::2         4  64512  00:25:56           9   ← ToR (eBGP, IPv6)
192.168.11.6    4  64514  00:25:56           9   ← other RR (explicit, IPv4)
fd00::6         4  64514  00:25:56           9   ← other RR (explicit, IPv6)

* = dynamic neighbor accepted via bgp listen range
```

#### BGP sessions at runtime — client node (worker)

```
L2VPN EVPN Summary (BGP router identifier 10.0.0.2, local AS 64514):

Neighbor        V    AS   Up/Down  State/PfxRcd  Desc
192.168.11.2    4  64512  00:25:56           9   ← ToR (eBGP, IPv4)
fd00::2         4  64512  00:25:56           9   ← ToR (eBGP, IPv6)
192.168.11.3    4  64514  00:25:56           9   ← RR (control-plane, IPv4)
fd00::3         4  64514  00:25:56           9   ← RR (control-plane, IPv6)
192.168.11.6    4  64514  00:25:56           9   ← RR (worker3, IPv4)
fd00::6         4  64514  00:25:56           9   ← RR (worker3, IPv6)
```

### Controller Reconcile Logic

**RR controller** (new, `cmd/rrcontroller`):

- Watches: local router Pod, Underlay objects
- Reconcile: waits for `underlay-ips` annotation, creates/updates `RawFRRConfig`
- Shutdown: deletes the `RawFRRConfig`

**Hostcontroller** (existing, updated):

- Watches: Nodes, Underlays, RawFRRConfigs, Pods (router + RR controller)
- Reconcile: writes `underlay-ips` annotation on local router pod; discovers RR nodes via RR controller pods; adds RR IPs as iBGP neighbors; merges RawFRRConfigs
- Triggers: RR controller pod or router pod add/remove/update
- Cache: must be extended to include RR controller pods (`app=openperouter-rr-controller`) across all nodes — the current cache only includes local `app=router` pods

### Failure and Rescheduling

When RR controller pod on Node A is evicted (Node A was one of two RRs):

1. **Graceful shutdown**: deletes `RawFRRConfig rr-nodeA`
2. **Node A hostcontroller**: regenerates FRR config without the RR snippet → Node A becomes a client
3. **Client nodes**: detect the pod is gone, drop iBGP neighbor for Node A. Session to Node B (surviving RR) stays up — **no traffic disruption**
4. **Kubernetes reschedules** on Node E → RR controller creates `RawFRRConfig` → Node E becomes the new RR
5. **Client nodes**: detect new pod on Node E, add iBGP neighbor → converge to RR pair (B + E)

Convergence is bounded by pod scheduling + BGP session establishment (typically seconds).

### Installation

**Kustomize**: `config/rr-controller/` overlay (Deployment + RBAC). Convenience overlay: `config/with-rr/`.

**Helm** (`values.yaml`):

```yaml
routeReflector:
  enabled: false
  replicas: 2
```

**Operator** (`OpenPERouter` CR):

```yaml
spec:
  routeReflector:
    enabled: true
    replicas: 2
```

### Testing

**Unit tests** (`internal/frr/`, `internal/conversion/`):

- FRR template rendering: `allowas-in` only on eBGP neighbors, RR client neighbor blocks rendered correctly (golden file tests)
- Conversion: `RRNodeUnderlayIPs` populated into `RRClients` NeighborConfig entries

**E2e tests** (`e2etests/tests/`, Ginkgo suite against a 4-node clab cluster):

- **RR controller lifecycle**: deploy RR controller → verify `RawFRRConfig` created for each RR node, `underlay-ips` annotation present on router pods
- **BGP sessions**: all iBGP sessions to RR nodes established, dynamic clients accepted via `bgp listen range` (`*` prefix in vtysh output)
- **EVPN route reflection**: type-3 IMET routes on client nodes show `*>i` (iBGP best path from RR), not the eBGP path from the ToR
- **East/West data plane**: ping between workloads on different client nodes, VXLAN packets go directly node-to-node (zero packets via ToR IP)
- **Failover**: evict one RR controller pod → verify surviving RR keeps sessions up, evicted node reverts to client config, rescheduled pod configures new RR, clients reconverge
- **No RR deployed**: without RR controller, system behaves identically to baseline (no iBGP neighbors, no RawFRRConfigs)
- **ToR VXLAN block**: drop VXLAN (UDP 4789) forwarding on the ToR (`iptables -I FORWARD -i toswitch -p udp --dport 4789 -j DROP`) to prove East/West traffic never hairpins through the ToR — pings between workloads must still succeed via direct node-to-node VXLAN

**CI lane**: run the full existing e2e suite with `routeReflector.enabled=true` on a 4-node cluster. The RR should be transparent — all existing EVPN/L2VNI/L3VNI tests must pass without modification.

## Alternatives Considered

- **Standalone FRR RR Pods**: requires cluster-to-underlay routing, adds separate FRR processes, pod IPs unstable
- **Full-mesh iBGP**: N*(N-1)/2 sessions, does not scale
- **FRR-K8s as RR**: `raw.config` is unsupported/experimental, cross-system coordination, upgrade risk

## References

- RFC 4456 — BGP Route Reflection
- FRR `bgp listen range` — https://docs.frrouting.org/en/latest/bgp.html
- PoC branch — https://github.com/qinqon/openperouter/tree/poc-router-reflector
