---
weight: 40
title: "Route Reflector"
description: "Distribute EVPN routes between nodes through an internal BGP route reflector, without relying on the ToR fabric"
icon: "article"
date: "2026-07-09T00:00:00+02:00"
lastmod: "2026-07-09T00:00:00+02:00"
toc: true
---

This example demonstrates how to run one node as a pure BGP route reflector (RFC 4456) that distributes EVPN routes between the other nodes, removing the need for a full iBGP mesh or for the Top-of-Rack fabric to reflect the routes.

## Overview

The control-plane node runs a router with no tunnel endpoint: it does not participate in the data plane and only reflects control-plane routes. The worker nodes run a regular data-plane underlay whose only BGP session is the internal (iBGP) one to the route reflector.

A layer 2 VNI is stretched between the worker nodes. Since the clients peer only with the reflector, the EVPN type-2/type-3 routes can only be learned through the reflection path, while the VXLAN traffic flows directly between the workers.

### Example Setup

The full example can be found in the [project repository](https://github.com/openperouter/openperouter/tree/main/examples/evpn/route-reflector) and can be deployed by running:

```bash
make docker-build demo-route-reflector
```

The example configures:

- An `Underlay` named `route-reflector` on the control-plane node, accepting dynamic iBGP sessions over a listen range and reflecting both `ipv4unicast` (VTEP reachability) and `evpn` routes
- An `Underlay` named `client` on the worker nodes, peering only with the route reflector
- A disconnected `L2VNI` on the worker nodes, plus one pod per worker attached to it via a macvlan interface

For details about the configuration fields, see the [Route Reflector configuration guide]({{< ref "../../configuration/route-reflector.md" >}}).

## Configuration

### OpenPERouter Configuration

The route reflector underlay is selected on the control-plane node. It has no `tunnelEndpoint`, and accepts dynamic neighbors from the cluster subnet via `listenRange` instead of enumerating each node:

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

The client underlay is selected on the worker nodes. It is a normal data-plane underlay whose only neighbor is the route reflector, peered as an internal (iBGP) session:

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
      address: 192.168.11.3  # the route reflector on the leafkind1 switch subnet
```

Finally, a disconnected L2VNI (no VRF) is stretched between the client nodes. The node selector keeps it off the route reflector node, which has no tunnel endpoint:

```yaml
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: L2VNI
metadata:
  name: reflected
  namespace: openperouter-system
spec:
  vni: 300
  nodeSelector:
    matchExpressions:
      - key: node-role.kubernetes.io/control-plane
        operator: DoesNotExist
  hostmaster:
    type: linux-bridge
    linuxBridge:
      autoCreate: true
```

**Configuration Notes:**

- **`routeReflector`**: marks the control-plane router as a route reflector and sets the BGP `cluster-id`
- **`listenRange`**: accepts dynamic iBGP sessions from any node in the cluster subnet, so nodes can be added without touching the reflector configuration
- **`routeReflectorClient`**: reflects the routes received in that address family to the other clients; `ipv4unicast` carries the VTEP `/32` reachability, `evpn` carries the type-2/type-3 routes
- **`hostmaster.autoCreate`**: instructs OpenPERouter to create a bridge local to the node that can be used to access the L2 domain

### Network Attachment Definition

```yaml
apiVersion: "k8s.cni.cncf.io/v1"
kind: NetworkAttachmentDefinition
metadata:
  name: macvlan-conf
spec:
  config: '{
      "cniVersion": "0.3.0",
      "type": "macvlan",
      "master": "br-hs-300",
      "mode": "bridge",
      "ipam": {
         "type": "static"
      }
    }'
```

A network attachment definition creating a macvlan interface on top of the `br-hs-300` bridge created by OpenPERouter for VNI 300. Both pods are on the same subnet, so no gateway is needed.

### Workload Configuration

Two pods, one per worker node, with a Multus secondary network attached to the L2VNI bridge:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: first
  annotations:
    k8s.v1.cni.cncf.io/networks: '[{
      "name": "macvlan-conf",
      "namespace": "default",
      "ips": ["192.171.31.2/24"]
      }]'
spec:
  nodeSelector:
    kubernetes.io/hostname: pe-kind-worker
  securityContext:
    runAsNonRoot: true
    runAsUser: 1000
    seccompProfile:
      type: RuntimeDefault
  containers:
    - name: agnhost
      image: registry.k8s.io/e2e-test-images/agnhost:2.45
      command: ["/agnhost", "netexec", "--http-port=8090"]
      securityContext:
        allowPrivilegeEscalation: false
        readOnlyRootFilesystem: true
        capabilities:
          drop: ["ALL"]
      ports:
      - containerPort: 8090
        name: http
```

The second pod is identical, pinned to `pe-kind-worker2` with IP `192.171.31.3/24`.

## Validation

### Route Reflector Sessions

The router pods can be listed with their nodes to identify the reflector (control-plane) and the clients (workers):

```bash
kubectl get pods -n openperouter-system -l app=router -o wide
```

On the route reflector router pod, the BGP summary shows the sessions with the clients. Dynamic neighbors accepted via the listen range are prefixed with `*`:

```bash
kubectl exec -n openperouter-system <router-pod-on-control-plane> -c frr -- vtysh -c "show bgp summary"
```

Expected output:

```text
L2VPN EVPN Summary:
BGP router identifier 10.0.0.0, local AS number 64514 VRF default
...
Neighbor        V         AS   MsgRcvd   MsgSent   TblVer  InQ OutQ  Up/Down State/PfxRcd   PfxSnt Desc
*192.168.11.4   4      64514        29        27        3    0    0 00:00:59            2        4 N/A
*192.168.11.5   4      64514        29        28        3    0    0 00:00:59            2        4 N/A

Total number of neighbors 2
* - dynamic neighbor
2 dynamic neighbor(s), limit 65535
```

The reflector's generated FRR configuration can be inspected as well:

```bash
kubectl exec -n openperouter-system <router-pod-on-control-plane> -c frr -- vtysh -c "show running-config"
```

It contains the route reflection bits described in the [configuration guide]({{< ref "../../configuration/route-reflector.md" >}}):

```text
router bgp 64514
 bgp cluster-id 192.0.2.1
 bgp listen limit 65535
 neighbor 192.168.11.0/24 peer-group
 neighbor 192.168.11.0/24 remote-as internal
 bgp listen range 192.168.11.0/24 peer-group 192.168.11.0/24
 !
 address-family ipv4 unicast
  neighbor 192.168.11.0/24 activate
  neighbor 192.168.11.0/24 route-reflector-client
 exit-address-family
 !
 address-family l2vpn evpn
  neighbor 192.168.11.0/24 activate
  neighbor 192.168.11.0/24 route-reflector-client
 exit-address-family
```

### Reflected EVPN Routes

On a worker's router pod, the type-2 routes of the pod running on the other worker must have been learned through the reflector. Reflected routes carry the `Originator` and `Cluster list` attributes:

```bash
kubectl exec -n openperouter-system <router-pod-on-worker> -c frr -- vtysh -c "show bgp l2vpn evpn route type macip"
```

Expected output (trimmed):

```text
Route Distinguisher: 10.0.0.2:2
*>i[2]:[0]:[48]:[aa:bb:cc:dd:ee:ff]:[32]:[192.171.31.3]
                    100.65.0.2                             100      0 i
                    RT:64514:300 ET:8
```

The route detail shows it was reflected, with the next hop preserved (the originating worker's VTEP) and the reflector's cluster-id in the cluster list:

```bash
kubectl exec -n openperouter-system <router-pod-on-worker> -c frr -- vtysh -c "show bgp l2vpn evpn route rd 10.0.0.2:2"
```

```text
  Route [2]:[0]:[48]:[aa:bb:cc:dd:ee:ff]:[32]:[192.171.31.3] VNI 300
  ...
      Origin IGP, localpref 100, valid, internal, best (First path received)
      Originator: 10.0.0.2, Cluster list: 192.0.2.1
```

### Pod-to-Pod Connectivity

We can check that the first pod is able to curl the second, and the client IP is the one corresponding to the secondary interface:

```bash
kubectl exec -it first -- curl 192.171.31.3:8090/clientip
```

Expected output:

```text
192.171.31.2:47420
```

Since the pods' only common network is the reflected L2VNI, the traffic proves the type-2/type-3 routes were distributed by the route reflector: removing the `routeReflector` underlay (or the `routeReflectorClient` properties) breaks the connectivity between the pods.

Traffic can be monitored in the OpenPERouter's namespace on one of the workers to validate that it goes directly worker-to-worker over the VXLAN overlay, without transiting the reflector:

```bash
21:12:33.201432 toswitch1 Out IP 100.65.0.1.40875 > 100.65.0.2.4789: VXLAN, flags [I] (0x08), vni 300
IP 192.171.31.2 > 192.171.31.3: ICMP echo request, id 4386, seq 0, length 64
```
