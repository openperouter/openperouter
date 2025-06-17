---
weight: 51
title: "MetalLB Integration"
description: "Integrate OpenPERouter with MetalLB for LoadBalancer service advertisement"
icon: "article"
date: "2025-06-15T15:03:22+02:00"
lastmod: "2025-06-15T15:03:22+02:00"
toc: true
---

This example demonstrates how to integrate OpenPERouter with MetalLB to advertise LoadBalancer services across the EVPN fabric, enabling external access to Kubernetes services.

## Overview

MetalLB provides load balancing for Kubernetes services by advertising service IPs via BGP. When integrated with OpenPERouter, these BGP routes are automatically converted to EVPN Type 5 routes, making the services reachable across the entire fabric.

## Prerequisites

Before proceeding with this integration:

1. **OpenPERouter Configuration**: Ensure OpenPERouter is properly configured with underlay and VNI settings (see [base configuration](../))
2. **MetalLB Installation**: Install MetalLB in your cluster
3. **Network Planning**: Plan your service IP ranges for each VNI

## Integration Architecture

### BGP Peering Setup

MetalLB establishes BGP sessions with OpenPERouter through the veth interfaces created for each VNI. Since the router-side IP is consistent across all nodes, you only need one BGPPeer configuration per VNI.

### Route Flow

1. **Service Creation**: Kubernetes LoadBalancer service is created
2. **MetalLB Advertisement**: MetalLB advertises the service IP via BGP to OpenPERouter
3. **EVPN Conversion**: OpenPERouter converts the BGP route to EVPN Type 5 route
4. **Fabric Distribution**: EVPN route is distributed across the fabric
5. **External Access**: External hosts can reach the service through the fabric

## Configuration Steps

### Step 1: Configure BGP Peers

Create BGPPeer resources for each VNI. Since the router IP is consistent across nodes, one peer configuration per VNI is sufficient.

#### Red VNI (VNI 100) BGP Peer

```yaml
apiVersion: metallb.io/v1beta2
kind: BGPPeer
metadata:
  name: red-vni-peer
  namespace: metallb-system
spec:
  myASN: 64515
  peerASN: 64514
  peerAddress: 192.169.10.0
```

#### Blue VNI (VNI 200) BGP Peer

```yaml
apiVersion: metallb.io/v1beta2
kind: BGPPeer
metadata:
  name: blue-vni-peer
  namespace: metallb-system
spec:
  myASN: 64515
  peerASN: 64514
  peerAddress: 192.169.11.0
```

**Configuration Details:**

- **myASN**: 64515 (MetalLB's ASN, matches the host ASN in OpenPERouter VNI config)
- **peerASN**: 64514 (OpenPERouter's ASN)
- **peerAddress**: Router-side veth IP for each VNI

### Step 2: Configure IP Address Pools

Create IP address pools for your LoadBalancer services. You can create separate pools for different VNIs or use a single pool.

#### Single Pool Configuration

```yaml
apiVersion: metallb.io/v1beta1
kind: IPAddressPool
metadata:
  name: service-pool
  namespace: metallb-system
spec:
  addresses:
  - 192.168.10.0/24
```

#### Multiple Pools for Different VNIs

```yaml
# Red VNI Pool
apiVersion: metallb.io/v1beta1
kind: IPAddressPool
metadata:
  name: red-vni-pool
  namespace: metallb-system
spec:
  addresses:
  - 192.168.10.0/24
---
# Blue VNI Pool
apiVersion: metallb.io/v1beta1
kind: IPAddressPool
metadata:
  name: blue-vni-pool
  namespace: metallb-system
spec:
  addresses:
  - 192.168.20.0/24
```

### Step 3: Configure BGP Advertisement

Create BGPAdvertisement resources to enable route advertisement.

#### Basic Advertisement

```yaml
apiVersion: metallb.io/v1beta1
kind: BGPAdvertisement
metadata:
  name: service-advertisement
  namespace: metallb-system
spec:
  ipAddressPools:
  - service-pool
```

#### VNI-Specific Advertisement

```yaml
apiVersion: metallb.io/v1beta1
kind: BGPAdvertisement
metadata:
  name: red-vni-advertisement
  namespace: metallb-system
spec:
  ipAddressPools:
  - red-vni-pool
  communities:
  - 64514:100  # VNI-specific community
```

## Route Advertisement Flow

Once configured, the integration works as follows:

![](/images/openpeemetallbroutes.svg)

1. **Service Creation**: Kubernetes LoadBalancer service is created with an IP from the pool
2. **MetalLB Processing**: MetalLB assigns an IP and starts advertising it via BGP
3. **BGP Session**: MetalLB advertises the route to OpenPERouter through the veth interface
4. **EVPN Conversion**: OpenPERouter converts the BGP route to EVPN Type 5 route
5. **Fabric Distribution**: The EVPN route is distributed to all fabric routers
6. **External Reachability**: External hosts can now reach the service through the fabric

## Traffic Flow

When external traffic reaches the service:

![](/images/openpeemetallbtraffic.svg)

1. **External Request**: External host sends traffic to the service IP
2. **Fabric Routing**: Fabric routes the traffic to the appropriate VTEP
3. **VXLAN Encapsulation**: Traffic is encapsulated in VXLAN with the appropriate VNI
4. **OpenPERouter Processing**: OpenPERouter receives and decapsulates the traffic
5. **Service Delivery**: Traffic is forwarded to the Kubernetes service endpoint
