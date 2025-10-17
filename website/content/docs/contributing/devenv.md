---
weight: 1
title: "The development environment"
description: "The development environment"
icon: "article"
date: "2025-06-15T15:03:22+02:00"
lastmod: "2025-06-15T15:03:22+02:00"
toc: true
---

In order to test and experiment with OpenPERouter, a [containerlab](https://containerlab.dev/) and [kind](https://kind.sigs.k8s.io/) based environment is available.

To start it, run 

```bash
make deploy
```

The topology of the environment is as follows:

![](/images/openpedevenv.svg)

With:

- Two kind nodes connected to a leaf, running OpenPERouter
- A spine container
- Two EVPN enabled leaves, leafA and leafB
- For each leaf, two hosts connected to two different VRFs (red and blue)
- One host connected to the default VRF of leafA

By default, the two VRFs are exposed as type 5 EVPN (VNI 100 and 200) from leafA and leafB
to the rest of the fabric.

The kubeconfig file required to interact with the cluster is created under `bin/kubeconfig`.

The leaf the kind cluster is connected to is configured with the following parameters:

IP: 192.168.11.2
ASN: 64512

and it is configured to accept BGP session from any peer coming from the network `192.168.11.0/24`
with ASN `64514`.  

More details, including the IP addresses of all the nodes involved,
can be found on the project [readme](https://github.com/openperouter/openperouter/tree/main/clab).

## Veth recreation

The development environment faces a significant issue:

- the nodes of the topology are containers
- the interfaces that connect the various node are veth pairs
- if the network namespace wrapping a veth (or any virtual interface) gets deleted, the veth gets deleted too
- OpenPERouter works by moving one interface from the host (the kind node) to the pod running inside of it

Because of this, when the router pod wrapping the interface gets deleted (instead of returning to the host as it would happen with a
real interface).

To emulate the behavior of a real system, there is a [background script](https://github.com/openperouter/openperouter/blob/main/clab/check_veths.sh)
which checks for the deletion of the veths and recreates them.

