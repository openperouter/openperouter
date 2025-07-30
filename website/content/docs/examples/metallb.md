---
weight: 10
title: "MetalLB Integration"
description: "Integrate Open PE Router with MetalLB for LoadBalancer service advertisement"
icon: "article"
date: "2025-06-15T15:03:22+02:00"
lastmod: "2025-06-15T15:03:22+02:00"
toc: true
---

This example demonstrates how to integrate OpenPERouter with MetalLB to advertise LoadBalancer services across the EVPN fabric, enabling external access to Kubernetes services.

## Overview

MetalLB provides load balancing for Kubernetes services by advertising service IPs via BGP. When integrated with OpenPERouter, these BGP routes are automatically converted to EVPN Type 5 routes, making the services reachable across the entire fabric.

### Example Setup

The full example can be found in the [project repository](https://github.com/openperouter/openperouter/examples/metallb) and can be deployed by running

```bash
make docker-build demo-metallb
```

This example exposes two different services over two different overlays by configuring two L3 VNIs on the OpenPERouter and peering MetalLB on the sessions corresponding to each VNI.

{{< figure src="/images/openpemetallbtwosessions.svg" alt="MetalLB Two Sessions" class="text-center" >}}

## Route Advertisement Flow

Once configured, the integration works as follows:

![MetalLB Route Flow](/images/openpeemetallbroutes.svg)

1. **Service Creation**: Kubernetes LoadBalancer service is created with an IP from the pool
2. **MetalLB Processing**: MetalLB assigns an IP and starts advertising it via BGP
3. **BGP Session**: MetalLB advertises the route to OpenPERouter through the veth interface
4. **EVPN Conversion**: OpenPERouter converts the BGP route to EVPN Type 5 route
5. **Fabric Distribution**: The EVPN route is distributed to all fabric routers
6. **External Reachability**: External hosts can now reach the service through the fabric

## Traffic Flow

When external traffic reaches the service:

![MetalLB Traffic Flow](/images/openpeemetallbtraffic.svg)

1. **External Request**: External host sends traffic to the service IP
2. **Fabric Routing**: Fabric routes the traffic to an appropriate VTEP
3. **VXLAN Encapsulation**: Traffic is encapsulated in VXLAN with the appropriate VNI
4. **OpenPERouter Processing**: OpenPERouter receives and decapsulates the traffic
5. **Service Delivery**: Traffic is forwarded to the Kubernetes service endpoint
6. **Service Reply**: The service reply finds the route learned by FRR-K8s and is routed to the host that sent the request

## Configuration

### L3 VNI Configuration

Configure one L3VNI for each overlay:

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
kind: L3VNI
metadata:
  name: blue
  namespace: openperouter-system
spec:
  vni: 200
  hostsession:
    asn: 64514
    hostasn: 64515
    localcidr:
      ipv4: 192.169.11.0/24
```

### MetalLB BGP Peer Configuration

On the MetalLB side, configure the corresponding `BGPPeer` resources:

```yaml
apiVersion: metallb.io/v1beta2
kind: BGPPeer
metadata:
  name: peerred
  namespace: metallb-system
spec:
  myASN: 64515
  peerASN: 64514
  peerAddress: 192.169.10.1
---
apiVersion: metallb.io/v1beta2
kind: BGPPeer
metadata:
  name: peerblue
  namespace: metallb-system
spec:
  myASN: 64515
  peerASN: 64514
  peerAddress: 192.169.11.1
```

> **Note**: The `peerAddress` field specifies the router-side IP address. MetalLB establishes BGP sessions with OpenPERouter through the veth interfaces created for each VNI. Since the router-side IP is consistent across all nodes, you only need one BGPPeer configuration per VNI.

### BGP Advertisement Configuration

MetalLB is configured with two address pools associated with two different namespaces (omitted here for brevity) and configured to advertise each pool to the corresponding `BGPPeer`:

![MetalLB VNI Routes](/images/openpemetallbvniroutes.svg)

```yaml
apiVersion: metallb.io/v1beta1
kind: BGPAdvertisement
metadata:
  name: bgp-advertisement-red
  namespace: metallb-system
spec:
  ipAddressPools:
    - address-pool-red
  peers:
    - peerred
---
apiVersion: metallb.io/v1beta1
kind: BGPAdvertisement
metadata:
  name: bgp-advertisement-blue
  namespace: metallb-system
spec:
  ipAddressPools:
    - address-pool-blue
  peers:
    - peerblue
```

## Return Traffic Configuration

Once the pod behind the service replies, the traffic must find its way back to the host that sent the request.

![MetalLB Reply Flow](/images/openpemetallbreply.svg)

To enable this, the host must learn the routes advertised by the two leaves (leafA and leafB) via BGP. We leverage the integration with [frr-k8s](https://github.com/metallb/frr-k8s/) to add a configuration that allows incoming routes to be added to the host:

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
      - address: 192.169.11.1
        asn: 64514
        toReceive:
          allowed:
            mode: all
```

## Verification

### Check BGP Session Status

Verify that the BGP sessions between MetalLB/FRR-K8s and the OpenPERouter are established:

```bash
kubectl get bgpsessionstates.frrk8s.metallb.io -A
```

Expected output:
```
NAMESPACE        NAME                          NODE                    PEER           VRF   BGP           BFD
frr-k8s-system   pe-kind-control-plane-94ct2   pe-kind-control-plane   192.169.11.1         Established   N/1
frr-k8s-system   pe-kind-control-plane-bc9zh   pe-kind-control-plane   192.169.10.1         Established   N/A
frr-k8s-system   pe-kind-worker-496lk          pe-kind-worker          192.169.11.1         Established   N/A
frr-k8s-system   pe-kind-worker-s74kn          pe-kind-worker          192.169.10.1         Established   N/A
```

### Test Service Connectivity

#### Test from Red Overlay

First, check the service details:

```bash
kubectl get svc -n red
```

Expected output:
```
NAME                TYPE           CLUSTER-IP      EXTERNAL-IP   PORT(S)        AGE
nginx-service-red   LoadBalancer   10.96.161.254   172.30.0.10   80:32732/TCP   2m3s
```

Test connectivity from a host connected to the red overlay:

```bash
docker exec clab-kind-hostA_red curl 172.30.0.10
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

#### Test from Blue Overlay

Try to access the same service from a host connected to the blue overlay:

```bash
docker exec clab-kind-hostA_blue curl --max-time 2 --no-progress-meter 172.30.0.10
```

Expected output:
```
curl: (28) Connection timed out after 2001 milliseconds
```

> **Expected Behavior**: The connection times out because the service is advertised only to the red network, demonstrating the network isolation between overlays.

