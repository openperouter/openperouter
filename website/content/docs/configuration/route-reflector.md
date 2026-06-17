---
weight: 43
title: "Route Reflector"
description: "Running a node as a BGP route reflector for the underlay fabric"
icon: "article"
date: "2026-06-17T00:00:00+02:00"
lastmod: "2026-06-17T00:00:00+02:00"
toc: true
---

A BGP Route Reflector (RFC 4456) lets a router re-advertise iBGP routes learned from one client to the other clients. This removes the need for a full iBGP mesh between nodes: instead of every router peering with every other router, the clients peer only with the reflector, and the reflector distributes the routes among them.

In OpenPERouter this is useful to distribute EVPN (type-2/type-3) and VTEP reachability routes between nodes without relying on the fabric, for scenarios where the fabric does not support EVPN.

## Enabling Route Reflection

Route reflection is built from three pieces:

1. **`routeReflector`** on the Underlay marks the local FRR process as a route reflector and sets the BGP `cluster-id`.
2. **`listenRange`** on a neighbor accepts dynamic BGP sessions from any peer in the given CIDR (`bgp listen range`), so the clients do not have to be enumerated one by one.
3. **`routeReflectorClient`** as a per-address-family property marks the dynamic peers as route reflector clients, so their routes are reflected to the other clients.

A typical deployment uses two Underlays selected by node role: one reflector and one client configuration.

### Reflector node

The reflector does not need a tunnel endpoint of its own — it only reflects the control-plane routes. It accepts dynamic neighbors over the cluster subnet and reflects both `ipv4unicast` (so clients learn each other's VTEP addresses) and `evpn` (so clients learn each other's MAC/IP routes).

```yaml
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: Underlay
metadata:
  name: route-reflector
  namespace: openperouter-system
spec:
  asn: 64514
  interfaces:
    - type: NetworkDevice
      networkDevice:
        interfaceName: toswitch1
  nodeSelector:
    matchExpressions:
      - key: node-role.kubernetes.io/control-plane
        operator: Exists
  routeReflector:
    clusterID: 192.0.2.1
  neighbors:
    - type: internal
      listenRange: 192.168.11.0/24
      addressFamilies:
        - type: ipv4unicast
          properties:
            - type: routeReflectorClient
        - type: evpn
          properties:
            - type: routeReflectorClient
```

### Client nodes

The clients run a normal data-plane Underlay (with a `tunnelEndpoint`) whose only neighbor is the reflector, established as an internal (iBGP) session.

```yaml
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: Underlay
metadata:
  name: client
  namespace: openperouter-system
spec:
  asn: 64514
  interfaces:
    - type: NetworkDevice
      networkDevice:
        interfaceName: toswitch1
  nodeSelector:
    matchExpressions:
      - key: node-role.kubernetes.io/control-plane
        operator: DoesNotExist
  tunnelEndpoint:
    cidrs:
      - 100.65.0.0/24
  neighbors:
    - type: internal
      address: 192.168.11.3 # the reflector's address on the cluster subnet
```

## Configuration Fields

| Field | Type | Description | Default | Range |
|-------|------|-------------|---------|-------|
| `routeReflector` | object | Enables route reflection on matching nodes when present. Omit to run as a standard router. | _(disabled)_ | |
| `routeReflector.clusterID` | string | BGP cluster-id (RFC 4456 §7) shared by all reflectors serving the same clients. Must be a valid IPv4 address and lie **outside** `routeridcidr` so it never collides with an allocated router-id. | `192.0.2.1` | valid IPv4 |
| `neighbors[].listenRange` | string | CIDR for dynamic neighbor acceptance via `bgp listen range`. Mutually exclusive with `address` and `interface`. IPv6 link-local ranges are rejected. | | valid CIDR |
| `neighbors[].addressFamilies[].properties[].type` | string | Per-address-family feature. `routeReflectorClient` marks the neighbor as a route reflector client in that address family. Requires the neighbor `type` to be `internal`. | | `routeReflectorClient` |

> The reflector's listen-range neighbor must use `type: internal`, because route reflection is an iBGP-only concept. Setting `routeReflectorClient` on a neighbor that is not `internal` is rejected at admission.

## What Happens When Route Reflection Is Enabled

When the controller detects the `routeReflector` field on the Underlay, the generated `frr.conf` includes:

1. **`bgp cluster-id <clusterID>`**, so the reflector stamps reflected routes with the cluster-id and uses the CLUSTER_LIST for loop detection.
2. **`bgp listen limit <limit>`** whenever a neighbor uses `listenRange`, raising FRR's dynamic-neighbor cap (100 by default). The limit defaults to `65535` and can be tuned globally via the `bgpListenLimit` Helm value (`openperouter.bgpListenLimit`) or the `bgpListenLimit` field of the operator's `OpenPERouter` resource.
3. **A peer-group per `listenRange`** (`neighbor <cidr> peer-group`, `remote-as internal`, `bgp listen range <cidr> peer-group <cidr>`) that accepts sessions from peers in the range.
4. **`neighbor <peer> route-reflector-client`** in each address family that carries the `routeReflectorClient` property.

A reflector without a `tunnelEndpoint` still renders the `l2vpn evpn` address family for its clients, but does not advertise local VNIs (`advertise-all-vni`) — it only reflects what the clients originate. This is the standard pure-reflector / spine pattern.

## Notes

- All reflectors that serve the same set of clients must share the same `clusterID` so loop detection works across them.
- The default `clusterID` (`192.0.2.1`) is an RFC 5737 documentation address, outside the default `routeridcidr` pool. With a custom `routeridcidr` you must still pick a `clusterID` outside of it — a `clusterID` inside the range is rejected at admission.
- The reflector preserves the BGP next hop of reflected routes, so clients must be able to reach each other's next hops (VTEP addresses). Activating `ipv4unicast` with `routeReflectorClient` on the reflector lets it reflect the client VTEP `/32` routes for this purpose.
