---
weight: 20
title: "KubeVirt L2 Integration"
description: "Integrate OpenPERouter with KubeVirt for VM-to-VM traffic via L2 EVPN/VXLAN overlay"
icon: "article"
date: "2025-06-15T15:03:22+02:00"
lastmod: "2025-06-15T15:03:22+02:00"
toc: true
---

This example demonstrates how to connect KubeVirt virtual machines to an L2 EVPN/VXLAN overlay using OpenPERouter, extending the [Layer 2 integration example](layer2.md).

## Overview

The setup creates both Layer 2 and Layer 3 VNIs, with OpenPERouter automatically creating a Linux bridge on the host. Two KubeVirt virtual machines are connected to this bridge via Multus secondary interfaces of type `bridge`.

### Architecture

- **L3VNI (VNI 100)**: Provides routing capabilities and connects to external networks
- **L2VNI (VNI 110)**: Creates a Layer 2 domain for VM-to-VM communication
- **Linux Bridge**: Automatically created by OpenPERouter for VM connectivity
- **VM Connectivity**: VMs connect to the bridge using Multus network attachments

![KubeVirt L2 Integration](/images/openpel2vmtovm.svg)

## Prerequisites

- A Kubernetes cluster with KubeVirt installed
- OpenPERouter deployed and configured
- Multus CNI installed and configured
- Access to a real cluster (this example cannot run on kind)

## Configuration

### 1. OpenPERouter Resources

Create the L3VNI and L2VNI resources:

```yaml
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: L3VNI
metadata:
  name: red
  namespace: openperouter-system
spec:
  asn: 64514
  vni: 100
  localcidr: 
    ipv4: 192.169.10.0/24
  hostasn: 64515
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
  vxlanport: 4789
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
  namespace: default
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

Create two virtual machines with network connectivity:

```yaml
apiVersion: kubevirt.io/v1
kind: VirtualMachine
metadata:
  name: vm-cirros
spec:
  running: false
  template:
    spec:
      networks:
      - name: evpn
        multus:
          networkName: evpn
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
            memory: 1024M
      terminationGracePeriodSeconds: 0
      volumes:
      - containerDisk:
          image: quay.io/kubevirt/cirros-container-disk-demo:devel
        name: containerdisk
      - cloudInitNoCloud:
          userData: |
            #!/bin/sh
            sudo ip address add 192.170.1.3/24 dev eth0
            sudo ip r add default via 192.170.1.1
            echo 'printed from cloud-init userdata'
        name: cloudinitdisk
```

**VM Configuration Details:**

- Uses the `evpn` network attachment for bridge connectivity
- Cloud-init configures the VM's IP address and default gateway
- Second VM should use IP `192.170.1.4/24` for testing

## Validation

### VM-to-VM Connectivity

Test connectivity between the two VMs:

```bash
# Connect to VM console
virtctl console vm-cirros

# Test ping to the other VM
ping 192.170.1.4
```

Expected output:
```
PING 192.170.1.4 (192.170.1.4): 56 data bytes
64 bytes from 192.170.1.4: seq=0 ttl=64 time=0.470 ms
64 bytes from 192.170.1.4: seq=1 ttl=64 time=0.321 ms
```

### Packet Flow Verification

Monitor packet flow on the router to verify traffic:

```bash
# Check packets flowing through the bridge
tcpdump -i br-hs-110 -n
```

Expected packet flow:

```bash
13:56:16.151606 pe-110 P   IP 192.170.1.3 > 192.170.1.4: ICMP echo request
13:56:16.151610 vni110 Out IP 192.170.1.3 > 192.170.1.4: ICMP echo request
13:56:16.152073 vni110 P   IP 192.170.1.4 > 192.170.1.3: ICMP echo reply
13:56:16.152075 pe-110 Out IP 192.170.1.4 > 192.170.1.3: ICMP echo reply
```

### Layer 3 Connectivity

Test connectivity to hosts in the L3VNI domain:

```bash
# From VM console, ping a host in the L3VNI
ping 192.168.10.3
```

Expected output:

```bash
PING 192.168.10.3 (192.168.10.3): 56 data bytes
64 bytes from 192.168.10.3: seq=0 ttl=62 time=1.207 ms
64 bytes from 192.168.10.3: seq=1 ttl=62 time=0.998 ms
```

### Live Migration Testing

Verify that connectivity persists during live migration:

```bash
# Start continuous ping from one VM to another
ping 192.170.1.4

# In another terminal, initiate live migration
virtctl migrate vm-cirros1
```

The ping should continue working throughout the migration process.

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
virtctl console vm-cirros
ip addr show
```
