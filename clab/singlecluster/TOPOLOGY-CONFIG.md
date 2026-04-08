# Topology Configuration with clab-config

The `clab-config` tool generates deterministic network configurations (IPs, ASNs, router IDs, VTEP addresses) from two declarative input files:

- **`kind.clab.yml`** — Containerlab topology defining nodes and links
- **`environment-config.yaml`** — IP ranges, node roles, VRFs, and BGP configuration

Both the containerlab deployment and the e2e tests derive their values from the same generated state, ensuring consistency.

## CLI Usage

### Generating Configuration

```bash
# Generate configs and topology state
go run ./cmd/clab-config/ apply \
  --clab clab/singlecluster/kind.clab.yml \
  --config clab/singlecluster/environment-config.yaml \
  --output-dir clab/singlecluster

# Or using a built binary
clab-config apply --clab kind.clab.yml --config environment-config.yaml --output-dir .
```

This produces:
- **Per-node directories** (`leafA/`, `leafB/`, etc.) containing `frr.conf` and optional `setup.sh`
- **`topology-state.json`** — The complete allocated state, consumed by tests and query commands

### Viewing the Summary

```bash
# Human-readable summary
clab-config summary --state clab/singlecluster/topology-state.json

# Full state as JSON
clab-config summary --state clab/singlecluster/topology-state.json -o json
```

### Querying Individual Values

All query commands output single values suitable for shell scripts.

```bash
STATE=clab/singlecluster/topology-state.json

# Get a node's VTEP IP
clab-config query --state $STATE node-vtep --node leafA
# Output: 100.64.0.1

# Get a node's link IP (its side of a point-to-point link)
clab-config query --state $STATE link-ip --node leafA --peer spine
# Output: 192.168.1.10/31

# Get IPv6 instead
clab-config query --state $STATE link-ip --node leafA --peer spine --family ipv6
# Output: fd00:1::a/127

# Reverse-lookup: find which node owns an IP
clab-config query --state $STATE ip-owner --ip 192.168.1.11
# Output: spine eth1

# List nodes matching a pattern
clab-config query --state $STATE nodes --pattern "leaf.*"
# Output:
# leafA
# leafB
# leafkind
```

### Scripting Example

```bash
STATE=clab/singlecluster/topology-state.json

# Configure all leaf nodes
for node in $(clab-config query --state $STATE nodes --pattern "^leaf[AB]$"); do
  vtep=$(clab-config query --state $STATE node-vtep --node "$node")
  echo "Node $node has VTEP $vtep"
done
```

## E2E Test Integration

The e2e test infrastructure (`e2etests/pkg/infra/`) loads `topology-state.json` at init time and derives all network values from it. This means **`clab-config apply` must be run before the e2e tests** to generate the state file.

### How It Works

The file `e2etests/pkg/infra/topology.go` provides a singleton `Topology()` function that:

1. Loads `clab/singlecluster/topology-state.json` (path resolved relative to the source file)
2. Unmarshals it into a local `TopologyState` struct
3. Caches the result via `sync.Once` — subsequent calls return the cached state

All other infra files (`leaf.go`, `routers.go`, `underlay.go`) call `Topology()` in their `init()` functions to populate their variables.

### What Gets Derived from Topology

| Variable / Field | Source |
|---|---|
| `HostARedIPv4`, `HostBBlueIPv6`, etc. | Peer IP of leaf interfaces facing host nodes |
| `LeafAConfig.VTEPIP` | `topo.Nodes["leafA"].VTEPIP` |
| `LeafAConfig.SpineAddress` | Spine's IP on the link facing leafA |
| `LeafAConfig.ASN` | `topo.Nodes["leafA"].BGP.ASN` |
| `LeafAConfig.SpineASN` | `topo.Nodes["leafA"].BGP.Peers[0].ASN` |
| Router links (`NeighborIP`) | Iterated from `topo.Links` and `topo.BroadcastNetworks` |
| `Underlay.Neighbors[0].Address` | Leafkind's broadcast member IP |
| `Underlay.Neighbors[0].ASN` | `topo.Nodes["leafkind"].BGP.ASN` |
| FRR template ASNs | Passed via `LeafConfiguration` and `LeafKindConfiguration` structs |

### What Stays Hardcoded

| Value | Reason |
|---|---|
| `ASN: 64514` (KIND node) | Kubernetes CRD config, not part of clab topology |
| `VTEPCIDR: "100.65.0.0/24"` | Kubernetes CRD config |
| `"toswitch"` NIC name | Kubernetes CRD config |
| `"clab-kind-"` container prefix | Stable — derived from topology name "kind" |

### Using Topology in Test Code

```go
package infra

// Topology() is available to any code importing this package.
// Example: getting a leaf's VTEP IP
topo := Topology()
vtep := topo.Nodes["leafA"].VTEPIP

// Get the IP of a node's interface facing a peer
ip, err := topo.GetLinkIP("spine", "leafA", IPv4)

// Get the peer's IP (works for host nodes not in topo.Nodes)
hostIP, err := topo.GetPeerIP("leafA", "hostA_red", IPv4)

// Get a broadcast network member's IP
memberIP, err := topo.GetBroadcastMemberIP("leafkind-switch", "leafkind", IPv4)

// Map clab node name to Docker container name
container := ContainerName("leafA") // "clab-kind-leafA"
```

## Regenerating After Topology Changes

If you modify `kind.clab.yml` or `environment-config.yaml`:

```bash
# Regenerate the state (and FRR configs)
go run ./cmd/clab-config/ apply \
  --clab clab/singlecluster/kind.clab.yml \
  --config clab/singlecluster/environment-config.yaml \
  --output-dir clab/singlecluster

# Verify the e2e tests still compile
cd e2etests && go build ./...
```

The IP addresses will change if you add/remove links (the allocator sorts links alphabetically), but both the deployment configs and the e2e tests will use the same new values automatically.
