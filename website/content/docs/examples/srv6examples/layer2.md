---
weight: 30
title: "Layer 2 Integration"
description: "Integrate OpenPERouter with Multus for pod to pod layer 2 overlay via SRv6 L3VPN"
icon: "article"
date: "2025-06-15T15:03:22+02:00"
lastmod: "2025-06-15T15:03:22+02:00"
toc: true
---

This example demonstrates how to integrate OpenPERouter with Multus to
have a pod's secondary interface connected to a layer 2 overlay, using
SRv6 as the underlay.

## Overview

A layer 2 VNI is created, exposing a layer 2 domain on the host. On each
node, a pod is created with a macvlan interface enslaved to that domain
via a Linux bridge.

### Example Setup

The full example can be found in the
[project repository](https://github.com/openperouter/openperouter/examples/l3vpn/layer2)
and can be deployed by running:

```bash
make docker-build demo-metallb-l3vpn-l2vni
```

The example configures both an L2 VNI and an L3VPN. The L2 VNI belongs to
the L3VPN's routing domain. Pods are running on two separate nodes and
connected via the overlay. Additionally, the pods are able to reach the
broader L3 domain via the L3VPN path.

> **Note**: `l2GatewayIPs` is not supported when using L3VPN (SRv6).
> Layer 3 connectivity to external hosts goes through the pod's primary
> interface (eth0) and the L3VPN path, not through the L2VNI's macvlan
> interface.

### Pod-to-Pod Connectivity

The two pods are on the same L2 subnet. When a pod tries to reach the
other, no routing is involved and the L2 traffic gets encapsulated in the
corresponding L2 VNI.

### Pod-to-External L3 Connectivity

When a pod needs to reach an external host on the L3 domain, the traffic
leaves via the pod's primary interface (eth0), hits the host namespace
where FRR-K8s has injected a route via the L3VPN's veth interface
(host-s-100), and is then SNATed and routed through the L3VPN's VRF
using SRv6 encapsulation.

## Configuration

### OpenPERouter Configuration

One L3VPN corresponding to the routing domain, one L2 VNI with the same
VRF as the L3VPN:

```yaml
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: L3VPN
metadata:
  name: red
  namespace: openperouter-system
spec:
  hostsession:
    asn: 64514
    hostasn: 64515
    localcidr:
      ipv4: 192.169.10.0/24
  vrf: red
  rdAssignedNumber: 100
  exportRTs:
  - "64514:100"
  importRTs:
  - "64520:100"
---
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: L2VNI
metadata:
  name: layer2
  namespace: openperouter-system
spec:
  vni: 110
  vrf: red
  hostmaster:
    type: linux-bridge
    linuxBridge:
      autoCreate: true
```

**Configuration Notes:**

- **hostmaster.autocreate**: Instructs OpenPERouter to create a bridge
  local to the node that can be used to access the L2 domain
- **`l2GatewayIPs`** is not supported with L3VPN (SRv6). This field is
  only available when using L3VNI (EVPN/VXLAN)

### Underlay Configuration

The underlay for this example includes both VXLAN and SRv6 tunnel
endpoints, along with IS-IS and SRv6 configuration:

```yaml
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: Underlay
metadata:
  name: underlay
  namespace: openperouter-system
spec:
  tunnelEndpoint:
    cidrs:
    - 100.65.0.0/24
    - 2001:db8:1234:5678::/64
  asn: 64514
  neighbors:
  - address: 2001:db8:1234::1
    asn: 64520
    ebgpMultiHop: true
  - address: 2001:db8:1234::2
    asn: 64520
    ebgpMultiHop: true
  - asn: 64512
    address: 192.168.11.2
  interfaces:
    - type: NetworkDevice
      networkDevice:
        interfaceName: toswitch1
  routeridcidr: 10.0.0.0/24
  isis:
    baseNet: "49.0001.0002.0003.0004.00"
    level: 1
  srv6:
    locator:
      basePrefix: "fd00:0:32::/48"
      format: "usid-f3216"
```

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
      "master": "br-hs-110",
      "mode": "bridge",
      "ipam": {
         "type": "static"
      }
    }'
```

A network attachment definition creating a macvlan interface with the
bridge created by the OpenPERouter as master.

### Workload Configuration

Two pods, each running on a different node, with a Multus secondary
network corresponding to the network attachment definition we just created:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: second
  annotations:
    k8s.v1.cni.cncf.io/networks: '[{
      "name": "macvlan-conf",
      "namespace": "default",
      "ips": ["192.170.1.4/24"]
      }]'
spec:
  nodeSelector:
    kubernetes.io/hostname: pe-kind-worker
  containers:
    - name: agnhost
      image: k8s.gcr.io/e2e-test-images/agnhost:2.45
      command: ["/agnhost", "netexec", "--http-port=8090"]
      ports:
      - containerPort: 8090
        name: http
```

## Validation

### Pod L2-to-L2 Connectivity

We can check that the first pod is able to curl the second, and the client
IP is the one corresponding to the secondary interface:

```bash
kubectl exec -it first -- curl 192.170.1.4:8090/clientip
```

Expected output:

```
192.170.1.3:44564
```

The traffic comes through the veth corresponding to the pod's secondary
interface and is sent out directly via the VXLAN interface and
encapsulated.

### Pod-to-External L3 Connectivity

With `192.168.20.2` being the IP of the host on the red network:

```bash
kubectl exec -it first -- curl 192.170.20.2:8090/clientip
```

Expected output:
```
192.169.10.2:35722
```

The traffic leaves via the pod's eth0 interface, is routed through the
host namespace to the L3VPN's veth interface (host-s-100), SNATed, and
then encapsulated using SRv6 towards the destination.

