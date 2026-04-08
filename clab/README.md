# Test Environment

The `clab/` directory contains containerlab-based test topologies for the Open PE Router project. Each topology defines a spine-leaf fabric with FRR routers, Kind-based Kubernetes clusters, and simulated hosts.

## Directory Structure

```
clab/
├── singlecluster/           # Single Kind cluster topology
│   ├── kind.clab.yml        # Containerlab topology definition
│   ├── environment-config.yaml  # Declarative network configuration
│   ├── leafkind/            # Generated FRR config for the kind-facing leaf
│   ├── spine/               # Generated FRR config for the spine
│   └── ip_map.txt           # IP assignment reference
├── multicluster/            # Two Kind clusters topology
│   ├── kind.clab.yml        # Containerlab topology definition
│   ├── environment-config.yaml  # Declarative network configuration
│   ├── leafkind-a/          # Generated FRR config for leaf facing kind cluster A
│   ├── leafkind-b/          # Generated FRR config for leaf facing kind cluster B
│   ├── spine/               # Generated FRR config for the spine
│   └── ip_map.txt           # IP assignment reference
├── leafA/                   # Shared FRR config and setup for Leaf A
├── leafB/                   # Shared FRR config and setup for Leaf B
├── frrcommon/               # Shared FRR daemon configs (daemons, vtysh.conf)
├── host*/                   # Host setup scripts (hostA_red, hostA_blue, etc.)
├── spine/                   # Shared spine FRR config (used by shared leaves)
├── scripts/                 # Modular deployment scripts (00-environment.sh through 10-veth-monitoring.sh)
├── kind/                    # Kind cluster configuration
├── tools/                   # Utility tools
├── setup.sh                 # Main deployment entry point
├── clean.sh                 # Teardown script
└── common.sh                # Shared shell variables and helper functions
```

## Topology Variants

### Single Cluster

One Kind cluster (control plane + worker) connected to the fabric through a leaf switch. LeafA and LeafB each connect two hosts on red (VNI 100) and blue (VNI 200) overlay networks. See [singlecluster/README.md](singlecluster/README.md) for the full topology diagram and IP assignments.

### Multi Cluster

Two Kind clusters (A and B), each with its own leaf switch and Kind nodes. Both leaf switches connect to the same spine for inter-cluster communication. See [multicluster/README.md](multicluster/README.md) for the full topology diagram and IP assignments.

## How kind.clab.yml and environment-config.yaml Relate

Each topology variant has two key files:

- **`kind.clab.yml`** -- The containerlab topology file. It defines the nodes (FRR routers, hosts, Kind clusters, bridges), their container images, bind mounts, and the physical link wiring between them. This is the "what is connected to what" definition.

- **`environment-config.yaml`** -- The declarative network configuration. It defines IP ranges, BGP ASNs, node roles, VRF-to-VNI mappings, and peering relationships. The `clab-config apply` tool reads both files together and generates the FRR configurations, IP assignments, and setup scripts for each node.

In short: `kind.clab.yml` defines the physical topology; `environment-config.yaml` defines the logical network configuration applied on top of it.

## Quick Start

### Prerequisites

- Docker (or Podman, set `CONTAINER_ENGINE=podman`)
- Kind
- Containerlab (pulled automatically as a container image)
- `clab-config` tool (optional, for regenerating configs): `make build-clab-config`

### Deploy Single Cluster (default)

```bash
# From the project root:
make deploy-clab

# Or directly:
CLAB_TOPOLOGY=singlecluster/kind.clab.yml clab/setup.sh
```

### Deploy Multi Cluster

```bash
# From the project root:
make deploy-multi-cluster

# Or directly:
CLAB_TOPOLOGY=multicluster/kind.clab.yml clab/setup.sh pe-kind-a pe-kind-b
```

### Tear Down

```bash
make clean
```

### Regenerate Configs with clab-config

To regenerate FRR configs and IP assignments from the declarative configuration:

```bash
make build-clab-config

# Single cluster
bin/clab-config apply \
  --clab clab/singlecluster/kind.clab.yml \
  --config clab/singlecluster/environment-config.yaml

# Multi cluster
bin/clab-config apply \
  --clab clab/multicluster/kind.clab.yml \
  --config clab/multicluster/environment-config.yaml
```

### Inspect Topology State

After running `clab-config apply`, a `topology-state.json` file is generated. Use it to query the topology:

```bash
# Print a human-readable summary
bin/clab-config summary --state topology-state.json

# Query a specific node's VTEP IP
bin/clab-config query --state topology-state.json node-vtep --node leafA

# Query the IP on a link between two nodes
bin/clab-config query --state topology-state.json link-ip --node leafA --peer spine

# Find which node owns an IP
bin/clab-config query --state topology-state.json ip-owner --ip 192.168.1.1
```

## Interfaces and IPs

Each variant has different interfaces and IPs. See the per-variant READMEs:
- [singlecluster](singlecluster/README.md#interfaces-and-ips) interfaces and IPs
- [multicluster](multicluster/README.md#interfaces-and-ips) interfaces and IPs

## FRR-K8s

FRR-K8s is a Kubernetes controller that runs FRR inside the Kind cluster. It is deployed in the test environment to validate interaction between the Open PE Router and a BGP-speaking component on the host.
