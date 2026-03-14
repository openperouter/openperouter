# Data Model: Configurable Development Environment

**Branch**: `001-configurable-dev-env` | **Date**: 2026-02-24

## Input Entities

### EnvironmentConfig (from `environment-config.yaml`)

The top-level configuration declaring logical network behavior.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| ipRanges | IPRanges | Yes | CIDR ranges for automatic IP allocation |
| nodes | []NodeConfig | Yes | List of node configuration entries with patterns |

### IPRanges

Configurable address pools for automatic allocation.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| pointToPoint.ipv4 | CIDR string | Yes | Base range for P2P link IPv4 allocation (e.g., `192.168.0.0/16`) |
| pointToPoint.ipv6 | CIDR string | Yes | Base range for P2P link IPv6 allocation (e.g., `fd00::/48`) |
| broadcast.ipv4 | CIDR string | Yes | Base range for broadcast network IPv4 allocation |
| broadcast.ipv6 | CIDR string | Yes | Base range for broadcast network IPv6 allocation |
| vtep | CIDR string | Yes | Range for VTEP IP allocation (e.g., `100.64.0.0/24`) |
| routerID | CIDR string | Yes | Range for BGP router ID allocation (e.g., `10.0.0.0/24`) |

### NodeConfig

A pattern-based node configuration entry.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| pattern | regex string | Yes | Regex pattern matching containerlab node names |
| role | enum: `edge-leaf`, `transit` | Yes | Node classification |
| evpnEnabled | bool | No | Whether EVPN is enabled on this node (default: false) |
| vrfs | map[string]VRFConfig | No | VRF declarations (edge-leaf only) |
| bgp | BGPConfig | Yes | BGP configuration for the node |

### VRFConfig

VRF declaration on an edge-leaf node.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| redistributeConnected | bool | No | Whether to redistribute connected routes (default: false) |
| interfaces | []string | Yes | Interface names from containerlab topology assigned to this VRF |
| vni | int | Yes | VXLAN Network Identifier for this VRF |

### BGPConfig

BGP configuration for a node.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| asn | uint32 | Yes | Autonomous System Number |
| peers | []PeerConfig | Yes | BGP peer definitions |

### PeerConfig

BGP peer definition using pattern matching.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| pattern | regex string | Yes | Regex pattern matching peer node names |
| evpnEnabled | bool | No | Whether EVPN address family is enabled (default: false) |
| bfdEnabled | bool | No | Whether BFD is enabled for this peer (default: false) |

### ClabTopology (from `.clab.yml`, read-only)

Subset of containerlab topology file consumed by the tool.

| Field | Type | Description |
|-------|------|-------------|
| name | string | Topology name |
| topology.nodes | map[string]ClabNode | Node definitions keyed by name |
| topology.links | []ClabLink | Physical link definitions |

### ClabNode

| Field | Type | Description |
|-------|------|-------------|
| kind | string | Node type: `linux`, `bridge`, `k8s-kind`, `ext-container` |
| image | string | Container image (for linux nodes) |
| binds | []string | Volume bind mounts |

### ClabLink

| Field | Type | Description |
|-------|------|-------------|
| endpoints | [2]string | Pair of `"nodeName:interfaceName"` strings |

## Output / State Entities

### TopologyState (persisted to state file)

The complete allocated state of a topology, enabling introspection and idempotent re-generation.

| Field | Type | Description |
|-------|------|-------------|
| inputHash | string | SHA-256 hash of combined input files for change detection |
| topologyName | string | From containerlab topology `name` field |
| nodes | map[string]NodeState | Per-node allocated state, keyed by node name |
| links | []LinkState | All links with allocated IPs |
| broadcastNetworks | []BroadcastNetwork | Switch/broadcast domain allocations |

### NodeState

Per-node allocated resources and resolved configuration.

| Field | Type | Description |
|-------|------|-------------|
| name | string | Node name from containerlab topology |
| matchedPattern | string | The pattern from environment config that matched this node |
| role | enum | Resolved role: `edge-leaf`, `transit`, `unmatched` |
| routerID | string | Allocated BGP router ID (IPv4 address) |
| vtepIP | string | Allocated VTEP IP (edge-leaf only, empty for transit) |
| interfaces | map[string]InterfaceState | Per-interface state keyed by interface name |
| vrfs | map[string]VRFState | Resolved VRF state (edge-leaf only) |
| bgp | BGPState | Resolved BGP state |

### InterfaceState

Per-interface allocated addresses and peer information.

| Field | Type | Description |
|-------|------|-------------|
| name | string | Interface name (e.g., `eth1`, `ethred`) |
| peerNode | string | Name of the node on the other end of the link |
| peerInterface | string | Interface name on the peer node |
| ipv4 | string | Allocated IPv4 address with prefix (e.g., `192.168.0.1/31`) |
| ipv6 | string | Allocated IPv6 address with prefix (e.g., `fd00::1/127`) |
| linkType | enum | `point-to-point` or `broadcast` |

### VRFState

Resolved VRF with all generated parameters.

| Field | Type | Description |
|-------|------|-------------|
| name | string | VRF name (e.g., `red`, `blue`) |
| vni | int | VXLAN Network Identifier |
| interfaces | []string | Assigned interface names |
| redistributeConnected | bool | Whether to redistribute connected routes |
| macAddress | string | Generated MAC address for the VXLAN interface (e.g., `02:ab:cd:00:00:01`) |
| bridgeID | int | Generated bridge interface number |

### LinkState

A resolved link with allocated addresses on both sides.

| Field | Type | Description |
|-------|------|-------------|
| nodeA | string | First node name (alphabetically lower) |
| interfaceA | string | Interface on nodeA |
| nodeB | string | Second node name |
| interfaceB | string | Interface on nodeB |
| ipv4Subnet | string | Allocated /31 IPv4 subnet |
| ipv6Subnet | string | Allocated /127 IPv6 subnet |
| type | enum | `point-to-point` or `broadcast` |

### BroadcastNetwork

A broadcast domain (switch) with all connected nodes.

| Field | Type | Description |
|-------|------|-------------|
| switchName | string | Bridge/switch node name |
| ipv4Subnet | string | Allocated /24 IPv4 subnet |
| ipv6Subnet | string | Allocated /64 IPv6 subnet |
| members | []BroadcastMember | All nodes connected to this switch |

### BroadcastMember

| Field | Type | Description |
|-------|------|-------------|
| nodeName | string | Connected node name |
| interfaceName | string | Interface on the connected node |
| ipv4 | string | Allocated IPv4 address within the broadcast subnet |
| ipv6 | string | Allocated IPv6 address within the broadcast subnet |

### BGPState

Resolved BGP configuration with concrete peer addresses.

| Field | Type | Description |
|-------|------|-------------|
| asn | uint32 | Autonomous System Number |
| peers | []BGPPeerState | Resolved peer list with concrete addresses |

### BGPPeerState

A resolved BGP peer with allocated addresses.

| Field | Type | Description |
|-------|------|-------------|
| nodeName | string | Peer node name |
| asn | uint32 | Peer's ASN |
| ipv4Address | string | Peer's IPv4 address on the shared link |
| ipv6Address | string | Peer's IPv6 address on the shared link |
| evpnEnabled | bool | Whether EVPN address family is active |
| bfdEnabled | bool | Whether BFD is enabled |

## Validation Rules

1. **Pattern uniqueness**: No node may match more than one pattern (FR-022). Validated before any allocation.
2. **Interface existence**: All interface names referenced in VRF configs must exist as link endpoints in the containerlab topology (FR-021).
3. **IP range sufficiency**: The configured CIDR ranges must have enough addresses for all allocations. Error if exhausted (FR-021).
4. **VNI uniqueness**: Each VNI must be unique across all VRFs in the topology.
5. **ASN consistency**: Peers referencing each other via patterns must have consistent ASN expectations.
6. **Role constraints**: VRF declarations on `transit` nodes are invalid. VTEP IPs are only allocated for `edge-leaf` nodes.

## State Transitions

This tool has no runtime state transitions — it is a batch configuration generator. The conceptual flow is:

1. **Load** → Read clab topology + environment config
2. **Validate** → Check patterns, interfaces, ranges (error if invalid)
3. **Allocate** → Deterministically assign IPs, MACs, router IDs
4. **Generate** → Produce FRR configs, setup scripts, summary
5. **Persist** → Write state file, generated configs to disk
