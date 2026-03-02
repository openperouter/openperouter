---
weight: 3
title: "The clab-config tool"
description: "Containerlab topology configuration tool"
icon: "article"
date: "2025-06-15T15:03:22+02:00"
lastmod: "2025-06-15T15:03:22+02:00"
toc: true
---

The `clab-config` tool reads a [containerlab](https://containerlab.dev/) topology file and an environment configuration, deterministically allocates network resources (IP addresses, ASNs, VNIs), and generates FRR configurations and setup scripts for each node. It is used to produce all the per-node configuration that the development environment needs.

## Installation

Build the binary from the repository root:

```bash
make build-clab-config
```

The resulting binary is placed in the `bin/` directory.

## Subcommands

`clab-config` has three subcommands: `apply`, `summary`, and `query`.

### apply

`apply` is the primary subcommand. It reads the containerlab topology and an environment configuration file, allocates network resources, and writes per-node configuration files to an output directory.

#### Flags

| Flag | Required | Default | Description |
|------|----------|---------|-------------|
| `--clab` | Yes | | Path to the containerlab topology file (`.clab.yml`) |
| `--config` | Yes | | Path to the environment configuration file |
| `--output-dir` | No | `.` | Directory where generated outputs are written |

#### Example

```bash
clab-config apply \
  --clab clab/singlecluster/kind.clab.yml \
  --config clab/singlecluster/environment-config.yaml \
  --output-dir /tmp/singlecluster-output
```

After running `apply`, the tool prints a human-readable summary of all allocations to stdout. Any warnings (for example, nodes in the topology that are not covered by the environment config) are printed to stderr.

### summary

`summary` loads a previously generated `topology-state.json` file and displays the full allocation summary, including nodes, links, IP assignments, and VTEP addresses.

#### Flags

| Flag | Required | Default | Description |
|------|----------|---------|-------------|
| `--state` | Yes | | Path to `topology-state.json` |
| `-o`, `--output` | No | `text` | Output format: `text` or `json` |

#### Examples

```bash
# Human-readable summary
clab-config summary --state /tmp/singlecluster-output/topology-state.json

# Full state as JSON (useful for scripting)
clab-config summary --state topology-state.json -o json
```

### query

`query` extracts specific pieces of information from a `topology-state.json` file. It is designed for scripting and automation where you need individual values rather than a full summary.

The `--state` flag is required for all query sub-subcommands.

#### node-vtep

Returns the VXLAN Tunnel Endpoint (VTEP) IP address assigned to a node.

```bash
clab-config query --state topology-state.json node-vtep --node leaf0
```

| Flag | Required | Description |
|------|----------|-------------|
| `--node` | Yes | Node name |

#### link-ip

Returns the IP address (in CIDR notation) assigned to a node's side of a point-to-point link with a peer node.

```bash
# IPv4 (default)
clab-config query --state topology-state.json link-ip --node leaf0 --peer spine0

# IPv6
clab-config query --state topology-state.json link-ip --node leaf0 --peer spine0 --family ipv6
```

| Flag | Required | Default | Description |
|------|----------|---------|-------------|
| `--node` | Yes | | Node name |
| `--peer` | Yes | | Peer node name |
| `--family` | No | `ipv4` | IP address family: `ipv4` or `ipv6` |

#### ip-owner

Performs a reverse lookup to find which node and interface own a given IP address. Outputs the node name and interface name separated by a space.

```bash
clab-config query --state topology-state.json ip-owner --ip 10.0.0.1
```

| Flag | Required | Description |
|------|----------|-------------|
| `--ip` | Yes | IP address to look up |

#### nodes

Lists all node names matching a regular expression pattern, one per line. Useful for iterating over groups of nodes in shell scripts.

```bash
# List all leaf nodes
clab-config query --state topology-state.json nodes --pattern "leaf.*"

# Iterate in a script
for node in $(clab-config query --state topology-state.json nodes --pattern "spine.*"); do
  echo "Processing $node"
done
```

| Flag | Required | Description |
|------|----------|-------------|
| `--pattern` | Yes | Regex pattern to match node names |

## Environment configuration schema

The environment configuration file (`environment-config.yaml`) defines the IP ranges used for allocation and the per-node BGP/VRF settings. Below is an annotated example:

```yaml
# IP ranges used for deterministic address allocation.
ipRanges:
  pointToPoint:
    ipv4: "192.168.1.0/24"   # Subnet carved into /31s for point-to-point links
    ipv6: "fd00:1::/112"     # IPv6 equivalent
  broadcast:
    ipv4: "192.168.11.0/16"  # Subnet for broadcast segments (e.g. kind node networks)
    ipv6: "fd00:11::/48"
  vtep: "100.64.0.0/24"      # Pool for VTEP loopback addresses
  routerID: "10.255.0.0/24"  # Pool for BGP router IDs

# Node definitions. Each entry matches nodes in the clab topology by regex.
nodes:
  - pattern: "leaf[AB]"        # Regex matched against containerlab node names
    role: edge-leaf             # Node role (edge-leaf or transit)
    vrfs:
      red:                      # VRF name
        vni: 100                # EVPN VNI for this VRF
        interfaces:
          - ethred              # Interfaces placed in this VRF
        redistributeConnected: true
      blue:
        vni: 200
        interfaces:
          - ethblue
        redistributeConnected: true
    bgp:
      asn: 64520                # BGP autonomous system number
      peers:
        - pattern: "spine"      # Regex to match peer node names from the topology
          evpnEnabled: true     # Enable EVPN address family with this peer
          bfdEnabled: true      # Enable BFD for this peer

  - pattern: "spine"
    role: transit
    bgp:
      asn: 64612
      peers:
        - pattern: "leaf.*"
          evpnEnabled: true
          bfdEnabled: true

  - pattern: "leafkind"
    role: transit
    bgp:
      asn: 64512
      peers:
        - pattern: "spine"
          evpnEnabled: true
          bfdEnabled: true
```

## Generated file structure

After running `apply`, the output directory contains the following structure:

```
<output-dir>/
  topology-state.json    # Full allocation state, used by summary and query
  <node-name>/
    frr.conf             # FRR configuration for this node
    setup.sh             # Optional setup script (created only when needed)
```

- **topology-state.json** -- A JSON file that captures all allocated resources (IPs, links, ASNs, VTEPs). This file is consumed by the `summary` and `query` subcommands.
- **frr.conf** -- A complete FRR configuration file for the node, including BGP sessions, VRF definitions, EVPN settings, and interface addresses.
- **setup.sh** -- An optional shell script for additional node setup (for example, creating VXLAN interfaces or assigning IPs to loopbacks). Only generated when the node requires it.

Each node that appears in both the containerlab topology and the environment configuration gets its own subdirectory.
