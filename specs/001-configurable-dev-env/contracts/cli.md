# CLI Contract: clab-config

**Binary**: `clab-config`

## Subcommands

### `clab-config apply`

Reads containerlab topology and environment config, allocates resources, generates FRR configs and setup scripts, persists state.

```
clab-config apply --clab <topology.clab.yml> --config <environment-config.yaml> [--output-dir <dir>]
```

| Flag | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| `--clab` | string | Yes | - | Path to containerlab topology file |
| `--config` | string | Yes | - | Path to environment configuration file |
| `--output-dir` | string | No | `.` | Directory for generated outputs (FRR configs, setup scripts, state file) |

**Output**: Human-readable configuration summary to stdout. Generated files written to output directory.

**Exit codes**:
- `0`: Success
- `1`: Validation error (overlapping patterns, missing interfaces, exhausted IP range)
- `2`: File I/O error

**Generated file structure**:
```
<output-dir>/
├── topology-state.json          # Persisted state for introspection
├── <node-name>/
│   ├── frr.conf                 # Generated FRR configuration
│   └── setup.sh                 # Generated setup script (edge-leaf only)
```

### `clab-config summary`

Displays the configuration summary from a persisted state file.

```
clab-config summary --state <topology-state.json> [-o json|text]
```

| Flag | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| `--state` | string | Yes | - | Path to topology state file |
| `-o` | string | No | `text` | Output format: `text` (human-readable) or `json` (machine-readable) |

**Output**: Configuration summary in the requested format.

**Exit codes**:
- `0`: Success
- `2`: File I/O error (state file not found or corrupt)

### `clab-config query`

Queries the topology state for specific information.

```
clab-config query --state <topology-state.json> <query-type> [args...]
```

**Query types**:

| Query | Args | Output |
|-------|------|--------|
| `node-vtep <name>` | Node name | VTEP IP address |
| `link-ip <nodeA> <nodeB> [--family ipv4\|ipv6]` | Two node names | Link IP for nodeA's side |
| `ip-owner <ip>` | IP address | Node name and interface |
| `nodes <pattern>` | Regex pattern | List of matching node names |

**Exit codes**:
- `0`: Success, result printed to stdout
- `1`: Query error (node not found, no match)
- `2`: File I/O error

## Go Package API

Package: `github.com/openperouter/openperouter/internal/clabconfig`

```go
// Load reads and validates clab topology + environment config, allocates
// resources, and returns the resolved topology state.
func Load(clabPath, configPath string) (*TopologyState, error)

// LoadState reads a persisted topology state from a JSON file.
func LoadState(statePath string) (*TopologyState, error)

// TopologyState provides query methods over the resolved topology.
type TopologyState struct { ... }

// GetNodeVTEP returns the VTEP IP for the named node.
func (t *TopologyState) GetNodeVTEP(nodeName string) (string, error)

// GetLinkIP returns the IP address for nodeName's side of the link to peerName.
func (t *TopologyState) GetLinkIP(nodeName, peerName string, family IPFamily) (string, error)

// FindIPOwner returns the node name and interface that owns the given IP.
func (t *TopologyState) FindIPOwner(ip string) (nodeName, iface string, err error)

// GetNodesByPattern returns all node names matching the regex pattern.
func (t *TopologyState) GetNodesByPattern(pattern string) ([]string, error)

// Summary returns the human-readable configuration summary.
func (t *TopologyState) Summary() string

// MarshalJSON returns the JSON representation of the topology state.
func (t *TopologyState) MarshalJSON() ([]byte, error)
```
