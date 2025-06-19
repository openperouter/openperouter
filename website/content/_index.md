+++
title = "Home"
+++

## OpenPERouter

OpenPERouter is an open implementation of a Provider Edge (PE) router, designed to terminate multiple VPN protocols on Kubernetes nodes and expose a BGP interface to the host network.

## Enable EVPN in your cluster

OpenPERouter will enable EVPN tunneling to any BGP enabled Kubernetes component,
such as Calico, MetalLB, KubeVip, Cilium, FRR-K8s and many others, behaving as an external router.

Behaving as an external router, the integration is seamless and BGP based, exactly as if a physical
Provider Edge Router was moved inside the node.

This project is in the early stage of prototyping. Use carefully!

## Description

Where we normally have a node interacting with the TOR switch, which is configured to map the VLans to a given VPN tunnel,
OpenPERouter runs directly in the node, exposing one Veth interface per VPN tunnel.

After OpenPERouter is configured and deployed on a cluster, it can interact with any BGP-speaking component of the cluster, including FRR-K8s, MetalLB, Calico and others. The abstraction is as if a physical Provider Edge Router was moved inside the node.

Here it's a high level overview of the abstraction, on the left side a classic Kubernetes deployment connected via
vlan interfaces, on the right side a deployment of OpenPERouter on a Kubernetes node.

![](/images/openpedescription.svg)

## A separate network namespace

The router runs in a separate network namespace, and interacts with the host using veth pair serving as entry points
for the L3 domain.

![](/images/openpeinside.svg)

## Why Run the Router on the Host?

Running the router directly on the host provides greater flexibility and simplifies configuration compared to manually setting up each VPN tunnel and mapping it to a VLAN on a traditional router. With OpenPERouter, the configuration is managed using Kubernetes Custom Resource Definitions (CRDs), allowing you to declaratively define VPN tunnels and their properties.

## Integration Benefits

### Seamless BGP Integration

OpenPERouter behaves exactly like a physical PE router, enabling seamless integration with
MetalLB, Calico, Cilium, FRR-K8s and any other BGP speaking component.

### Operational Advantages

- **Centralized Configuration**: Kubernetes CRDs for declarative configuration
- **Version Control**: Configuration changes tracked in Git
- **Automation**: Integration with CI/CD pipelines
- **Consistency**: Repeatable deployments across environments
- **Reduced Complexity**: No external router reconfiguration required

## Use Cases

### Multi-Tenant Environments

- Separate VNIs for different tenants or applications
- Isolated routing domains with controlled inter-VNI communication

### Hybrid Cloud

- Extend on-premises networks to Kubernetes clusters
- Maintain consistent routing policies across environments

### Network Segmentation

- Production, development, and management networks
- Secure isolation between different network segments

### Load Balancer Integration

- Advertise LoadBalancer services across the fabric
- Enable external access to Kubernetes services
