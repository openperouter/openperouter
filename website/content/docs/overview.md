---
weight: 5
title: "Overview"
description: "An overview of OpenPERouter"
icon: "article"
toc: true
---

## OpenPERouter

OpenPERouter is an open implementation of a Provider Edge (PE) router, designed
to terminate multiple VPN protocols on Kubernetes nodes and expose a BGP
interface to the host network.

## Enable VPN tunnels in your cluster

OpenPERouter supports L3 and L2 overlays, and it integrates exactly like an
external router.


### L3 Overlays

OpenPERouter can extend L3 VPN tunnels to any BGP enabled Kubernetes component,
such as Calico, MetalLB, KubeVip, Cilium, FRR-K8s and many others, behaving as
an external router.

## L2 Overlays

OpenPERouter supports L2 overlays too, allowing seamless communication between
nodes using a stretched layer 2 domain. L2 Overlays can be used directly for
nodes or they can integrate with Multus for pod secondary networks.

Additionally, L2 overlays can be mapped to a L3 routing domain for more complex
scenarios.

## Overview

Where we normally have a node interacting with the TOR switch, which is
configured to map the vlans to a given VPN tunnel, OpenPERouter runs directly in
the node, exposing one Veth interface per VPN tunnel.

After OpenPERouter is configured and deployed on a cluster, it can interact with
any BGP-speaking component of the cluster, including FRR-K8s, MetalLB, Calico
and others. The abstraction is as if a physical Provider Edge Router was moved
inside the node.

Here is a high level overview of the abstraction: on the left side, a classic
Kubernetes deployment connected via vlan interfaces; on the right side, a
deployment of OpenPERouter on a Kubernetes node:

L3:

![Description of OpenPERouter, showcasing the l3 integration](/images/openpedescription.svg)

L2:

![Description of OpenPERouter, showcasing the l2 integration](/images/openpedescriptionl2.svg)

## Why Run the Router on the Host?

Running the router directly on the host provides greater flexibility and
simplifies configuration compared to manually setting up each VPN tunnel and
mapping it to a VLAN on a traditional router. With OpenPERouter, the
configuration is managed using Kubernetes Custom Resource Definitions (CRDs),
allowing you to declaratively define VPN tunnels and their properties.

## A separate network namespace

The router runs in a separate network namespace, and interacts with the host
using a veth pair serving as entry points for the overlays. This allows it to
be seen as a completely external component from a networking point of view, making
it independent and integrable with the CNI running on the cluster.

![](/images/openpeinside.svg)

## Integration Benefits

A key operational advantage is that no changes are required to your existing
external router or network fabric. You can deploy the solution without
reconfiguring your current network infrastructure.

OpenPERouter is a pluggable, CNI independent component that can run with your
CNI of choice.

### Seamless BGP Integration

OpenPERouter behaves exactly like a physical PE router, enabling seamless
integration with MetalLB, Calico, Cilium, FRR-K8s and any other BGP speaking
component.

### L2 integration with Multus

With L2 overlays, the same configuration achievable with Vlans and Multus
secondary interfaces can be achieved using OpenPERouter.


