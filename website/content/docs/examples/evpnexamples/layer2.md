---
weight: 30
title: "Layer 2 Integration"
description: "Integrate OpenPERouter with Multus for pod to pod layer 2 overlay via EVPN / VXLAN"
icon: "article"
date: "2025-06-15T15:03:22+02:00"
lastmod: "2025-06-15T15:03:22+02:00"
toc: true
---

This example demonstrates how to integrate OpenPERouter with Multus to have a pod's secondary interface connected to a layer 2 overlay.

## Overview

A layer 2 VNI is created, exposing a layer 2 domain on the host. On each node, a pod is created with a macvlan interface enslaved to that domain via a Linux bridge.

### Example Setup

The full example can be found in the [project repository](https://github.com/openperouter/openperouter/examples/evpn/layer2) and can be deployed by running:

```bash
make docker-build demo-l2
```

The example configures both an L2 VNI and an L3 VNI. The L2 VNI belongs to the L3 VNI's routing domain. Pods are running on two separate nodes and connected via the overlay. Additionally, the pods are able to reach the broader L3 domain.

### Pod-to-Pod Connectivity

The two pods are on the same L2 subnet. When a pod tries to reach the other, no routing is involved and the L2 traffic gets encapsulated in the corresponding L2 VNI.

![Layer 2 Pod to Pod](/images/openpel2podtopod.svg)

### Pod-to-Host Connectivity

When a pod needs to reach a host belonging to the L3 domain the L2 VNI belongs to, the traffic is routed in the VRF of the OpenPERouter the pod is connected to, and routed according to the Type 5 routes.

## Configuration

### OpenPERouter Configuration

One L3 VNI corresponding to the routing domain, one L2 VNI with the same VRF as the L3 VNI:

```yaml
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: L3VNI
metadata:
  name: red
  namespace: openperouter-system
spec:
  vrf: red
  vni: 100
  hostsession:
    asn: 64514
    hostasn: 64515
    localcidr:
      ipv4: 192.169.10.0/24
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
  l2gatewayips: ["192.170.1.1/24"]
```

**Configuration Notes:**

- **`l2gatewayip` field**: Allows each pod to have the same default gateway and be able to send traffic to the outer L3 domain
- **hostmaster.autocreate**: Instructs OpenPERouter to create a bridge local to the node that can be used to access the L2 domain

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
         "type": "static",
         "routes": [
              {
                "dst": "0.0.0.0/0",
                "gw": "192.170.1.1"
              }
            ]
      }
    }'
```

A network attachment definition creating a macvlan interface with:

- The bridge created by the OpenPERouter as master
- The default gateway IP assigned to each OpenPERouter instance on every node

### Workload Configuration

Two pods, each running on a different node, with a Multus secondary network corresponding to the network attachment definition we just created:

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
      command: ["/bin/sh", "-c", "ip r del default dev eth0 && /agnhost netexec --http-port=8090"]
      ports:
      - containerPort: 8090
        name: http
      securityContext:
        capabilities:
          add: ["NET_ADMIN"]
```

> **Note**: We remove the default gateway so the secondary interface default gateway is the only active one.

## Validation

### Pod L2-to-L2 Connectivity

We can check that the first pod is able to curl the second, and the client IP is the one corresponding to the secondary interface:

```bash
kubectl exec -it first -- curl 192.170.1.4:8090/clientip
```

Expected output:

```
192.170.1.3:44564
```

We can monitor traffic in the OpenPERouter's namespace to validate that the traffic is actually going through the overlay:

```bash
17:56:45.152547 pe-110 P   IP 192.170.1.3 > 192.170.1.4: ICMP echo request, id 12544, seq 0, length 64
17:56:45.152563 vni110 Out IP 192.170.1.3 > 192.170.1.4: ICMP echo request, id 12544, seq 0, length 64
17:56:45.152578 toswitch Out IP 100.65.0.0.50691 > 100.65.0.1.4789: VXLAN, flags [I] (0x08), vni 110
IP 192.170.1.3 > 192.170.1.4: ICMP echo request, id 12544, seq 0, length 64
17:56:45.152627 toswitch In  IP 100.65.0.1.50691 > 100.65.0.0.4789: VXLAN, flags [I] (0x08), vni 110
IP 192.170.1.4 > 192.170.1.3: ICMP echo reply, id 12544, seq 0, length 64
17:56:45.152634 vni110 P   IP 192.170.1.4 > 192.170.1.3: ICMP echo reply, id 12544, seq 0, length 64
17:56:45.152637 pe-110 Out IP 192.170.1.4 > 192.170.1.3: ICMP echo reply, id 12544, seq 0, length 64
```

**Traffic Flow Analysis:**
The traffic comes through the veth corresponding to the pod's secondary interface, then it's sent out directly via the VXLAN interface and encapsulated.

### Pod-to-External L3 Connectivity

With `192.168.20.2` being the IP of the host on the red network:

```bash
kubectl exec -it first -- curl 192.168.20.2:8090/clientip
```

Expected output:

```
192.170.1.3:37144
```

The pod is able to reach a host on the layer 3 network and the IP is preserved.

When monitoring traffic:

```bash
18:01:50.406263 pe-110 P   IP 192.170.1.3 > 192.168.20.2: ICMP echo request, id 21760, seq 0, length 64
18:01:50.406267 br-pe-110 In  IP 192.170.1.3 > 192.168.20.2: ICMP echo request, id 21760, seq 0, length 64
18:01:50.406284 br-pe-100 Out IP 192.170.1.3 > 192.168.20.2: ICMP echo request, id 21760, seq 0, length 64
18:01:50.406287 vni100 Out IP 192.170.1.3 > 192.168.20.2: ICMP echo request, id 21760, seq 0, length 64
18:01:50.406295 toswitch Out IP 100.65.0.0.39370 > 100.64.0.1.4789: VXLAN, flags [I] (0x08), vni 100
IP 192.170.1.3 > 192.168.20.2: ICMP echo request, id 21760, seq 0, length 64
```

**Traffic Flow Analysis:**
We see that the packet comes from the veth interface towards the bridge associated with VNI 110 (the default gateway), then is routed to the bridge corresponding to the layer 3 domain and finally encapsulated.
