---
weight: 10
title: "MetalLB Integration"
description: "Integrate Open PE Router with MetalLB for LoadBalancer service advertisement in passthrough mode"
icon: "article"
date: "2025-06-15T15:03:22+02:00"
lastmod: "2025-06-15T15:03:22+02:00"
toc: true
---

This example demonstrates how to integrate OpenPERouter with MetalLB in passthrough mode to advertise LoadBalancer services across the fabric, enabling external access to Kubernetes services.

## Overview

MetalLB provides load balancing for Kubernetes services by advertising service IPs via BGP. When integrated with OpenPERouter in passthrough mode, these BGP routes are directly advertised to the fabric without EVPN encapsulation, providing a simpler networking model for basic connectivity scenarios.

### Example Setup

The full example can be found in the [project repository](https://github.com/openperouter/openperouter/examples/passthrough/metallb) and can be deployed by running

```bash
make demo-metallb-passthrough
```

This example exposes a LoadBalancer service through a single passthrough configuration, where OpenPERouter acts as a BGP speaker that directly advertises routes to the fabric.

## Route Advertisement Flow

Once configured, the integration works as follows:

![MetalLB Passthrough Route Flow](/images/openpemetallbbgproutespt.svg)


1. **Service Creation**: Kubernetes LoadBalancer service is created with an IP from the pool
2. **MetalLB Processing**: MetalLB assigns an IP and starts advertising it via BGP
3. **BGP Session**: MetalLB advertises the route to OpenPERouter through the veth interface
4. **Direct Advertisement**: OpenPERouter directly advertises the BGP route to the fabric
5. **Fabric Distribution**: The BGP route is distributed to all fabric routers
6. **External Reachability**: External hosts can now reach the service through the fabric

## Traffic Flow

When external traffic reaches the service:

1. **External Request**: External host sends traffic to the service IP
2. **Fabric Routing**: Fabric routes the traffic to the OpenPERouter node
3. **Direct Forwarding**: Traffic is forwarded directly to the Kubernetes service endpoint
4. **Service Reply**: The service reply is routed back through the fabric to the requesting host

## Configuration

### L3Passthrough Configuration

Configure a single L3Passthrough resource for direct BGP advertisement:

```yaml
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: L3Passthrough
metadata:
  name: passthrough
  namespace: openperouter-system
spec:
  hostsession:
    asn: 64514
    hostasn: 64515
    localcidr:
      ipv4: 192.169.10.0/24
```

**Configuration Details:**

- **ASN**: 64514 (OpenPERouter's ASN)
- **Host ASN**: 64515 (for BGP sessions with MetalLB)
- **Local CIDR**: 192.169.10.0/24 (BGP session network)

### MetalLB BGP Peer Configuration

Configure MetalLB to peer with OpenPERouter:

```yaml
apiVersion: metallb.io/v1beta2
kind: BGPPeer
metadata:
  name: pepassthrough
  namespace: metallb-system
spec:
  myASN: 64515
  peerASN: 64514
  peerAddress: 192.169.10.1
```

### IP Address Pool Configuration

Configure MetalLB with an address pool for LoadBalancer services:

```yaml
apiVersion: metallb.io/v1beta1
kind: IPAddressPool
metadata:
  name: address-pool
  namespace: metallb-system
spec:
  addresses:
    - 172.30.0.10-172.30.0.15
  autoAssign: true
  serviceAllocation:
    namespaces:
      - passthrough
```

### BGP Advertisement Configuration

Configure MetalLB to advertise the address pool to OpenPERouter:

```yaml
apiVersion: metallb.io/v1beta1
kind: BGPAdvertisement
metadata:
  name: bgp-advertisement-passthrough
  namespace: metallb-system
spec:
  ipAddressPools:
    - address-pool
  peers:
    - pepassthrough
```

## Return Traffic Configuration

To enable return traffic from services back to external hosts, configure FRR-K8s to receive routes from OpenPERouter:

```yaml
apiVersion: frrk8s.metallb.io/v1beta1
kind: FRRConfiguration
metadata:
  name: receive-all 
  namespace: frr-k8s-system
spec:
  bgp:
    routers:
    - asn: 64515
      neighbors:
      - address: 192.169.10.1
        asn: 64514
        toReceive:
          allowed:
            mode: all
```

## Verification

### Check BGP Session Status

Verify that the BGP session between MetalLB/FRR-K8s and OpenPERouter is established:

```bash
kubectl get bgpsessionstates.frrk8s.metallb.io -A
```

Expected output:
```
NAMESPACE        NAME                          NODE                    PEER           VRF   BGP           BFD
frr-k8s-system   pe-kind-control-plane-94ct2   pe-kind-control-plane   192.169.10.1         Established   N/1
frr-k8s-system   pe-kind-worker-496lk          pe-kind-worker          192.169.10.1         Established   N/A
```

### Test Service Connectivity

Check the service details:

```bash
kubectl get svc -n passthrough
```

Expected output:
```
NAME                        TYPE           CLUSTER-IP      EXTERNAL-IP   PORT(S)        AGE
nginx-service-passthrough   LoadBalancer   10.96.161.254   172.30.0.10   80:32732/TCP   2m3s
```

Test connectivity from a host connected to the fabric:

```bash
docker exec clab-kind-hostA_default curl 172.30.0.10
```

Expected output:
```html
<html>
<head>
<title>Welcome to nginx!</title>
</head>
<body>
<h1>Welcome to nginx!</h1>
<p>If you see this page, the nginx web server is successfully installed and
working. Further configuration is required.</p>

<p>For online documentation and support please refer to
<a href="http://nginx.org/">nginx.org</a>.<br/>
Commercial support is available at
<a href="http://nginx.com/">nginx.com</a>.</p>

<p><em>Thank you for using nginx.</em></p>
</body>
</html>
```

