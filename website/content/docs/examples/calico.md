---
weight: 20
title: "Calico Integration"
description: "Integrate OpenPERouter with Calico for pod to pod traffic via EVPN / VXLAN overlay"
icon: "article"
date: "2025-06-15T15:03:22+02:00"
lastmod: "2025-06-15T15:03:22+02:00"
toc: true
---

This example demonstrates how to integrate OpenPERouter with Calico to advertise the pod network via an L3 EVPN and to allow pod-to-pod traffic via a VXLAN overlay.

## Overview

Calico allows each node to establish a BGP session with a router to advertise the pod network of each node and to allow pod-to-pod traffic to flow. Here we leverage OpenPERouter to provide a seamless integration with the EVPN fabric.

### Example Setup

The full example can be found in the [project repository](https://github.com/openperouter/openperouter/examples/calico) and can be deployed by running deployed by running:

```bash
make docker-build demo-calico
```

This example configures Calico to advertise the pod network to the OpenPERouter and runs a pod on each node. It then shows how the pods are connected via the overlay and reachable from the fabric.

### Control Plane

{{< figure src="/images/openpecalicoroutes.svg" alt="Calico Routes" class="text-center" >}}

The Bird instance of Calico running on each node peers with the OpenPERouter, learning routes to the other node's pods and advertising the routes related to the node it's running on. The routes are then translated to EVPN Type 5 routes to the fabric, including the other OpenPERouter instance.

### Data Path

When a pod tries to reach a pod running on another node, the traffic lands on the host, then in the OpenPERouter via the veth interface, and then gets encapsulated as a VXLAN packet toward the other node, where it's decapsulated and sent to the node.

![Calico Traffic Flow](/images/openpecalicotraffic.svg)

## Configuration

### OpenPERouter Configuration

The OpenPERouter configuration is simple: one underlay and one L3VNI configured to peer with the host:

```yaml
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: L3VNI
metadata:
  name: red
  namespace: openperouter-system
spec:
  vni: 100
  hostsession:
    asn: 64514
    hostasn: 64515
    localcidr:
      ipv4: 192.169.10.0/24
---
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: Underlay
metadata:
  name: underlay
  namespace: openperouter-system
spec:
  asn: 64514
  vtepcidr: 100.65.0.0/24
  nics:
    - toswitch
  neighbors:
    - asn: 64512
      address: 192.168.11.2
```

### Calico Configuration

Calico is configured to establish a session with the same router IP on every node:

```yaml
apiVersion: projectcalico.org/v3
kind: BGPPeer
metadata:
  name: openpe
spec:
  peerIP: 192.169.10.1
  asNumber: 64514
  numAllowedLocalASNumbers: 5
---
apiVersion: projectcalico.org/v3
kind: BGPConfiguration
metadata:
  name: default
spec:
  nodeToNodeMeshEnabled: false
  logSeverityScreen: Info
  asNumber: 64515
  serviceClusterIPs:
    - cidr: 10.96.0.0/12
  serviceExternalIPs:
    - cidr: 104.244.42.129/32
    - cidr: 172.217.3.0/24
  listenPort: 179
  bindMode: NodeIP
```

Additionally, a node status is created to validate the status of the BGP session:

```yaml
apiVersion: projectcalico.org/v3
kind: CalicoNodeStatus
metadata:
  name: status
spec:
  classes:
    - BGP
  node: pe-kind-worker
  updatePeriodSeconds: 10
```

## Verification

### Check BGP Session Status

Verify the status of the BGP session between Calico and OpenPERouter:

```bash
kubectl get caliconodestatuses.projectcalico.org status -o yaml
```

Expected output:

```yaml
apiVersion: projectcalico.org/v3
kind: CalicoNodeStatus
metadata:
  name: status
spec:
  classes:
  - BGP
  node: pe-kind-worker
  updatePeriodSeconds: 10
status:
  agent:
    birdV4: {}
    birdV6: {}
  bgp:
    numberEstablishedV4: 1
    numberEstablishedV6: 0
    numberNotEstablishedV4: 0
    numberNotEstablishedV6: 0
    peersV4:
    - peerIP: 192.169.10.1
      since: "15:55:08"
      state: Established
      type: GlobalPeer
```

### Test Pod-to-Pod Connectivity

First, check the two pods running on different nodes:

```bash
kubectl get pods -o wide
```

Expected output:
```
NAME                      READY   STATUS    RESTARTS   AGE   IP             NODE                    NOMINATED NODE   READINESS GATES
agnhost-daemonset-97jjq   1/1     Running   0          33m   10.244.97.8    pe-kind-control-plane   <none>           <none>
agnhost-daemonset-zj6sg   1/1     Running   0          33m   10.244.1.195   pe-kind-worker          <none>           <none>
```

Test connectivity from one pod to the other:

```bash
kubectl exec agnhost-daemonset-97jjq -- curl --no-progress-meter 10.244.1.195:8090
```

Expected output:
```
NOW: 2025-07-01 16:29:16.788070872 +0000 UTC m=+2044.193991270
```

> **Success**: The pods can communicate with each other!

### Test External Access

Test accessing a pod from a host on the red overlay. This demonstrates that the pod IP is routable and exposed on the EVPN overlay:

```bash
docker exec clab-kind-hostA_red curl --no-progress-meter 10.244.1.195:8090
```

Expected output:
```
NOW: 2025-07-01 16:30:55.312723398 +0000 UTC m=+2142.718643795
```

### Verify VXLAN Encapsulation

To verify that traffic is actually running over VXLAN, we can monitor the traffic while pinging between pods:

```bash
kubectl exec agnhost-daemonset-97jjq -- ping 10.244.1.195
```

Expected output:
```
PING 10.244.1.195 (10.244.1.195): 56 data bytes
64 bytes from 10.244.1.195: seq=0 ttl=60 time=0.161 ms
64 bytes from 10.244.1.195: seq=1 ttl=60 time=0.135 ms
64 bytes from 10.244.1.195: seq=2 ttl=60 time=0.098 ms
64 bytes from 10.244.1.195: seq=3 ttl=60 time=0.114 ms
```

The traffic flow observed in the router shows VXLAN encapsulation:

```bash
6:33:38.392401 pe-100 In  IP 10.244.97.8 > 10.244.1.195: ICMP echo request, id 10752, seq 0, length 64
16:33:38.392411 br-pe-100 Out IP 10.244.97.8 > 10.244.1.195: ICMP echo request, id 10752, seq 0, length 64
16:33:38.392413 vni100 Out IP 10.244.97.8 > 10.244.1.195: ICMP echo request, id 10752, seq 0, length 64
16:33:38.392419 toswitch Out IP 100.65.0.0.47720 > 100.65.0.1.4789: VXLAN, flags [I] (0x08), vni 100
IP 10.244.97.8 > 10.244.1.195: ICMP echo request, id 10752, seq 0, length 64
16:33:38.392488 toswitch In  IP 100.65.0.1.47720 > 100.65.0.0.4789: VXLAN, flags [I] (0x08), vni 100
IP 10.244.1.195 > 10.244.97.8: ICMP echo reply, id 10752, seq 0, length 64
16:33:38.392494 vni100 In  IP 10.244.1.195 > 10.244.97.8: ICMP echo reply, id 10752, seq 0, length 64
16:33:38.392495 br-pe-100 In  IP 10.244.1.195 > 10.244.97.8: ICMP echo reply, id 10752, seq 0, length 64
16:33:38.392499 pe-100 Out IP 10.244.1.195 > 10.244.97.8: ICMP echo reply, id 10752, seq 0, length 64
```

**Traffic Flow Analysis:**

1. **Incoming**: The ICMP packet comes from the host through the veth interface
2. **Encapsulation**: It leaves through the VXLAN interface and gets encapsulated as a VXLAN packet toward the VTEP of the other node
3. **Return**: The reply follows the same path in reverse
4. **VXLAN Header**: Notice the VXLAN encapsulation with VNI 100 and the outer IP addresses (100.65.0.0/100.65.0.1)
