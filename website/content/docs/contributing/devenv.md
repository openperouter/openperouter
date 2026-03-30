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

## Declarative topology configuration

The FRR configurations and setup scripts for each node in the development environment are generated
from two source files using the `clab-config` tool:

- **`kind.clab.yml`** -- the containerlab topology file that defines nodes, links, and interface names.
- **`environment-config.yaml`** -- a declarative file that describes IP ranges, BGP parameters, VRFs, and node roles.

Both files live side-by-side under a topology directory (e.g. `clab/singlecluster/`).

### Regenerating configs with `clab-config apply`

After editing `environment-config.yaml` or `kind.clab.yml`, regenerate the per-node configuration by running:

```bash
make build-clab-config

bin/clab-config apply \
  --clab clab/singlecluster/kind.clab.yml \
  --config clab/singlecluster/environment-config.yaml \
  --output-dir clab/singlecluster
```

This produces a directory per node (containing `frr.conf` and optional `setup.sh`) plus a
`topology-state.json` that can be inspected with `clab-config summary` or `clab-config query`.

### Creating a new topology variation

To create a new topology, add a directory under `clab/` with its own `kind.clab.yml` and
`environment-config.yaml`, then run `clab-config apply` pointing at those files. The
`clab/multicluster/` directory is an existing example of a second topology.

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

