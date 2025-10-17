---
weight: 20
title: "Stretching a layer 2 overlay across multiple KubeVirt clusters"
description: "Integrate OpenPERouter with KubeVirt for VM-to-VM (running in different clusters) traffic via L2 EVPN/VXLAN overlay"
icon: "article"
date: "2025-10-14T15:45:48+01:00"
lastmod: "2025-10-14T15:45:48+01:00"
toc: true
---

This example demonstrates how to connect KubeVirt virtual machines running in
different Kubernetes clusters to an L2 EVPN/VXLAN overlay using OpenPERouter,
extending the [KubeVirt single cluster example](kubevirt.md).

## Overview
The setup creates both Layer 2 and Layer 3 VNIs, with OpenPERouter
automatically creating a Linux bridge on the host. Two KubeVirt virtual
machines are connected to this bridge via Multus secondary interfaces of type
`bridge`.

### Architecture

- **L3VNI (VNI 100)**: Provides routing capabilities and connects to external networks
- **L2VNI (VNI 110)**: Creates a Layer 2 domain for VM-to-VM communication
- **Linux Bridge**: Automatically created by OpenPERouter for VM connectivity
- **VM Connectivity**: VMs connect to the bridge using Multus network attachments

![KubeVirt Multi Cluster L2 Integration](/images/evpn-openperouter-multicluster.svg)

## Example Setup

> **Note:** Some operating systems have their `inotify.max_user_instances`
> set too low to support larger kind clusters. This leads to:
>
> * virt-handler failing with CrashLoopBackOff, logging `panic: Failed to create an inotify watcher`
> * nodemarker failing with CrashLoopBackOff, logging `too many open files`
>
> If that happens in your setup, you may want to increase the limit with:
>
> ```bash
> sysctl -w fs.inotify.max_user_instances=1024
> ```

The full example can be found in the
[project repository](https://github.com/openperouter/openperouter/examples/evpn/multi-cluster)
and can be deployed by running:

```bash
make docker-build demo-multi-cluster
```

The example configures both an L2 VNI and an L3 VNI. The L2 VNI belongs to the
L3 VNI's routing domain. VMs are connected into a single L2 overlay.
Additionally, the VMs are able to reach the broader L3 domain, and are
reachable from the broader L3 domain.

## Configuration

### 1. OpenPERouter Resources

Create the L3VNI and L2VNI resources (in **each** cluster):

```yaml
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: L3VNI
metadata:
  name: red
  namespace: openperouter-system
spec:
  vni: 100
  vrf: red
---
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: L2VNI
metadata:
  name: layer2
  namespace: openperouter-system
spec:
  hostmaster:
    autocreate: true
    type: bridge
  l2gatewayip: 192.170.1.1/24
  vni: 110
  vrf: red
```

**Key Configuration Points:**

- The L2VNI references the L3VNI via the `vrf: red` field, enabling routed traffic
- `hostmaster.autocreate: true` creates a `br-hs-110` bridge on the host
- `l2gatewayip` defines the gateway IP for the VM subnet

### 2. Network Attachment Definition

Create a Multus network attachment definition for the bridge:

```yaml
apiVersion: k8s.cni.cncf.io/v1
kind: NetworkAttachmentDefinition
metadata:
  name: evpn
spec:
  config: |
    {
      "cniVersion": "0.3.1",
      "name": "evpn",
      "type": "bridge",
      "bridge": "br-hs-110",
      "macspoofchk": false,
      "disableContainerInterface": true
    }
```

### 3. Virtual Machine Configuration

Create two virtual machines with network connectivity (one VM in each cluster):

This VM should be created in cluster **A**:
```yaml
apiVersion: kubevirt.io/v1
kind: VirtualMachine
metadata:
  name: vm-1
spec:
  runStrategy: Always
  template:
    metadata:
      labels:
        kubevirt.io/vm: vm-1
    spec:
      domain:
        devices:
          interfaces:
          - bridge: {}
            name: evpn
          disks:
          - disk:
              bus: virtio
            name: containerdisk
          - disk:
              bus: virtio
            name: cloudinitdisk
        resources:
          requests:
            memory: 2048M
        machine:
          type: ""
      networks:
      - name: evpn
        multus:
          networkName: evpn
      terminationGracePeriodSeconds: 0
      volumes:
      - containerDisk:
          image: quay.io/kubevirt/fedora-with-test-tooling-container-disk:v1.5.2
        name: containerdisk
      - cloudInitNoCloud:
          networkData: |
            version: 2
            ethernets:
              eth0:
                addresses:
                - 192.170.1.3/24
                gateway4: 192.170.1.1
        name: cloudinitdisk
```

This VM should be created in cluster **B**:
```yaml
apiVersion: kubevirt.io/v1
kind: VirtualMachine
metadata:
  name: vm-2
spec:
  runStrategy: Always
  template:
    metadata:
      labels:
        kubevirt.io/vm: vm-2
    spec:
      domain:
        devices:
          interfaces:
          - bridge: {}
            name: evpn
          disks:
          - disk:
              bus: virtio
            name: containerdisk
          - disk:
              bus: virtio
            name: cloudinitdisk
        resources:
          requests:
            memory: 2048M
        machine:
          type: ""
      networks:
      - name: evpn
        multus:
          networkName: evpn
      terminationGracePeriodSeconds: 0
      volumes:
      - containerDisk:
          image: quay.io/kubevirt/fedora-with-test-tooling-container-disk:v1.5.2
        name: containerdisk
      - cloudInitNoCloud:
          networkData: |
            version: 2
            ethernets:
              eth0:
                addresses:
                - 192.170.1.30/24
                gateway4: 192.170.1.1
        name: cloudinitdisk
```

**VM Configuration Details:**

- Uses the `evpn` network attachment for bridge connectivity
- Cloud-init configures the VM's IP address and default gateway

## Validation

### VM-to-VM Connectivity

Test connectivity between the two VMs:

```bash
# Connect to VM console
virtctl console vm-1

# It may take about 2 minutes to start up and get to the login.
# Once it does, login with username "fedora" an password "fedora".

# Test ping to the other VM
ping 192.170.1.30
```

Expected output:
```
PING 192.170.1.30 (192.170.1.30): 56 data bytes
64 bytes from 192.170.1.30: seq=0 ttl=64 time=0.470 ms
64 bytes from 192.170.1.30: seq=1 ttl=64 time=0.321 ms
```

### Packet Flow Verification

Monitor packet flow on the router to verify traffic:

```bash
# Check packets flowing through the bridge
tcpdump -i br-hs-110 -n
```

Expected packet flow:

```bash
13:56:16.151606 pe-110 P   IP 192.170.1.3 > 192.170.1.30: ICMP echo request
13:56:16.151610 vni110 Out IP 192.170.1.3 > 192.170.1.30: ICMP echo request
13:56:16.152073 vni110 P   IP 192.170.1.30 > 192.170.1.3: ICMP echo reply
13:56:16.152075 pe-110 Out IP 192.170.1.30 > 192.170.1.3: ICMP echo reply
```

### VM-to-External L3 Connectivity

With `192.168.20.2` being the IP of the host on the red network:

```bash
# Run this from the VM console (vm-1)
curl 192.168.20.2:8090/clientip
```

Expected output (vm-1):

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

## Troubleshooting

### Common Issues

1. **Bridge not created**: Verify the L2VNI has `hostmaster.autocreate: true`
2. **VM cannot reach gateway**: Check that the VM's IP is in the same subnet as `l2gatewayip`
3. **No VM-to-VM connectivity**: Ensure both VMs are connected to the same bridge (`br-hs-110`)

### Debug Commands

```bash
# Check bridge creation
ip link show br-hs-110

# Verify network attachment
kubectl get network-attachment-definitions

# Check VM network interfaces
virtctl console vm-1
ip addr show
```
