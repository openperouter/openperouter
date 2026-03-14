# Quickstart: Configurable Development Environment

## Prerequisites

- Go 1.25+ installed
- Existing containerlab topology file (`.clab.yml`)

## Build

```bash
make build-clab-config
# or directly:
go build -o bin/clab-config ./cmd/clab-config/
```

## Basic Usage

### 1. Create an environment configuration

Create `environment-config.yaml` alongside your containerlab topology:

```yaml
ipRanges:
  pointToPoint:
    ipv4: "192.168.1.0/24"
    ipv6: "fd00::/48"
  broadcast:
    ipv4: "192.168.11.0/24"
    ipv6: "fd00:11::/48"
  vtep: "100.64.0.0/24"
  routerID: "10.0.0.0/24"

nodes:
  - pattern: "leaf[AB]"
    role: edge-leaf
    evpnEnabled: true
    vrfs:
      red:
        redistributeConnected: true
        interfaces: [ethred]
        vni: 100
      blue:
        redistributeConnected: true
        interfaces: [ethblue]
        vni: 200
    bgp:
      asn: 64520
      peers:
        - pattern: "spine"
          evpnEnabled: true
          bfdEnabled: true

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
```

### 2. Generate configuration

```bash
clab-config apply \
  --clab clab/singlecluster/kind.clab.yml \
  --config clab/singlecluster/environment-config.yaml \
  --output-dir clab/singlecluster/
```

This generates:
- FRR configuration files per router node
- Setup scripts per edge-leaf node
- A `topology-state.json` state file
- A human-readable summary printed to stdout

### 3. View summary later

```bash
clab-config summary --state clab/singlecluster/topology-state.json
```

### 4. Query topology (for scripts or debugging)

```bash
# Get VTEP IP for a node
clab-config query --state topology-state.json node-vtep leafA

# Get link IP between two nodes
clab-config query --state topology-state.json link-ip leafA spine

# Find which node owns an IP
clab-config query --state topology-state.json ip-owner 192.168.1.1

# JSON output for automation
clab-config summary --state topology-state.json -o json
```

### 5. Use in e2e tests (Go)

```go
import "github.com/openperouter/openperouter/internal/clabconfig"

topo, err := clabconfig.Load(
    "clab/singlecluster/kind.clab.yml",
    "clab/singlecluster/environment-config.yaml",
)

vtep, _ := topo.GetNodeVTEP("leafA")
linkIP, _ := topo.GetLinkIP("leafA", "spine", clabconfig.IPv4)
```

## Running Tests

```bash
# Unit tests for the clab-config tool
go test ./internal/clabconfig/...

# Full project tests
make test
```
