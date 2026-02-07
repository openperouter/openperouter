# Configurable Development Environment

## Summary

This enhancement proposes a declarative, flexible, and maintainable configuration system for the containerlab topology used in end-to-end tests and examples. The system will automatically allocate network resources (IPs, VTEPs, MACs) and generate FRR configurations based on a simple, high-level topology definition.

## Motivation

### Current State

The topology infrastructure has evolved from a single fixed configuration to multiple variations serving different purposes:

**Existing use cases:**
- Showcasing the integration with existing projects, such as Calico, MetalLB and Kubevirt
- Running as the base for end-to-end tests
- Demoing Kubevirt migration across multiple clusters
- Different layout for the multicluster example

**Upcoming use cases:**
- SRv6 transport instead of EVPN

### Problems

This evolution has resulted in configuration management challenges:

1. **Fragmented tooling**: the configuration is spread across multiple mechanisms:
   - Templates (in e2e tests and examples)
   - Bash scripts (e.g., Calico deployment)
   - Custom tools (IP assignment)

2. **Tight coupling**: the end-to-end tests rely on hardcoded topology assumptions using fixed node names and network configurations

3. **Maintainability**: Adding new topology variations requires duplicating configuration logic across multiple files

4. **Lack of introspection**: No unified way to query the deployed topology configuration (e.g., "what is leafA's VTEP IP?")

## Goals

The solution should provide:

1. **Declarative configuration**: Define the topology behavior at a high level without specifying low-level details (IPs, MACs, etc.)
2. **Automatic resource allocation**: System assigns IPs, VTEPs, and MAC addresses automatically
3. **Configuration introspection**: Query API/CLI to retrieve allocated resources and topology details
4. **Single source of truth**: Replace fragmented configuration mechanisms with one unified system
5. **Pattern-based configuration**: Apply configurations to node groups using patterns (e.g., `leaf.*`)
6. **A discoverable output on how the configuration is applied:** Have an easy to read summary of what was applied to the environment
7. **Separation of concerns**: Decouple e2e tests from specific topology implementation details

## Proposal

### High-Level Architecture

An environment consists of two complementary files:

1. **Containerlab topology file** (`.clab.yml`): Defines the physical topology - nodes, links, and container images
2. **Environment configuration file** (`environment-config.yaml`): Defines the logical network configuration - routing, VRFs, and behavior
3. **A clab-config cli tool** to apply / query the current configuration

The system reads both files and generates all necessary configurations. This separation allows:
- Multiple logical configurations to share the same physical topology
- Easy variation of network behavior without recreating the containerlab topology
- Clear distinction between infrastructure (what exists) and configuration (how it behaves)

For each node of the clab topology, the system will:

1. Allocate IP addresses for all point-to-point links
2. Configure host-level requirements (e.g., VXLAN interfaces)
3. Generate and apply FRR configuration
4. Generate and apply node setup scripts

### ContainerLab Node Classification

The system recognizes two primary router types:

1. **Edge nodes**: Host tunnel endpoints (VTEPs), manage VRFs, and connect to hosts
2. **Transit nodes**: Passthrough routers (spines) that forward traffic without VXLAN termination. To be noted that the leaves connected
to the kind cluster are transit nodes too, as the VXLan termination is performed on the nodes.

Configuration is applied using pattern matching (e.g., `leaf.*`), eliminating the need for per-node specification of common parameters.

### Configuration Examples

#### Edge Node Configuration

An edge node configuration declares high-level intent without specifying implementation details:

```yaml
nodes:
  - pattern: "leaf[AB]"  # Matches leafA, leafB
    role: edge-leaf
    evpnEnabled: true
    vrfs:
      red:
        redistributeConnected: true
        interfaces:
          - ethred  # Interface names from containerlab topology
        vni: 100
      blue:
        redistributeConnected: true
        interfaces:
          - ethblue
        vni: 101
    bgp:
      asn: 65001  # Can be auto-assigned if omitted
      peers:
        - pattern: "spine"
          evpnEnabled: true
          bfdEnabled: true
```

The interface names are defined in the `links` section of the containerlab file.

**Key features:**
- VTEP IP, MAC addresses, and router IDs assigned automatically
- Point-to-point link IPs allocated from configured ranges
- FRR configuration generated based on declared VRFs and BGP settings

#### Transit Node Configuration

Transit nodes (spines) have simpler configuration focused on routing:

```yaml
nodes:
  - pattern: "spine"
    role: transit
    bgp:
      asn: 65000
      peers:
        - pattern: "leaf.*"
          evpnEnabled: true
          bfdEnabled: true
        - pattern: "leafkind"
          evpnEnabled: true
```

**Key features:**
- Relays the BGP routes (for VTEPS) and EVPN routes to its peers
- No VXLAN or VRF configuration needed
- Automatic IP allocation for all connected links

## Configuration Summary Output

After generating and applying configurations, the system provides a human-readable summary of what was configured. This summary serves as immediate documentation and verification of the deployed topology.

### Output Format

The summary is displayed after running `clab-config apply` and includes:

1. **Topology Overview**: Number of nodes, links, and configuration patterns matched
2. **Per-Node Summary**: For each node, show:
   - Matched pattern and assigned role
   - Allocated IPs (loopback, VTEP, router ID)
   - Interface assignments with peer information
   - VRF configurations (if applicable)
   - BGP configuration summary (ASN, peer count)
3. **Resource Allocation Summary**: Total IPs allocated, subnet ranges used
4. **Warnings/Errors**: Any configuration issues or edge cases encountered

**Note:** A graph based output (mermaid, ascii-art) will be implemented as a follow up of
this proposal.

### Example Output

```
Configuration Summary
=====================

Topology: kind.clab.yml + topology-config.yaml
Nodes: 4 (2 edge-leaves, 1 transit, 1 kind-node)
Links: 6 point-to-point, 1 broadcast network

Edge Leaves
-----------
leafA (matched pattern: leaf[AB])
  Role: edge-leaf
  Router ID: 10.0.1.1
  VTEP IP: 10.0.1.1
  Interfaces:
    eth1 -> spine (192.168.0.1/31, fd00::1/127)
    ethred -> host-red (VRF: red)
    ethblue -> host-blue (VRF: blue)
  VRFs:
    red (VNI: 100, interfaces: [ethred])
    blue (VNI: 101, interfaces: [ethblue])
  BGP: AS 65001, 1 peer:
    - spine (AS 65000, 192.168.0.0, fd00::0) EVPN enabled, BFD enabled

leafB (matched pattern: leaf[AB])
  Role: edge-leaf
  Router ID: 10.0.1.2
  VTEP IP: 10.0.1.2
  Interfaces:
    eth1 -> spine (192.168.0.3/31, fd00::3/127)
    ethred -> host-red (VRF: red)
    ethblue -> host-blue (VRF: blue)
  VRFs:
    red (VNI: 100, interfaces: [ethred])
    blue (VNI: 101, interfaces: [ethblue])
  BGP: AS 65001, 1 peer:
    - spine (AS 65000, 192.168.0.2, fd00::2) EVPN enabled, BFD enabled

Transit Nodes
-------------
spine (matched pattern: spine)
  Role: transit
  Router ID: 10.0.0.1
  Interfaces:
    eth1 -> leafA (192.168.0.0/31, fd00::0/127)
    eth2 -> leafB (192.168.0.2/31, fd00::2/127)
    eth3 -> leafkind (192.168.1.1/24, fd00:1::1/64)
  BGP: AS 65000, 3 peers:
    - leafA (AS 65001, 192.168.0.1, fd00::1) EVPN enabled, BFD enabled
    - leafB (AS 65001, 192.168.0.3, fd00::3) EVPN enabled, BFD enabled
    - leafkind (AS 64512, 192.168.1.2, fd00:1::2) EVPN enabled

leaf-kind (matched pattern: leaf-kind)
  Role: transit
  Router ID: 10.0.0.2
  Interfaces:
    eth1 -> spine (192.168.1.2/24, fd00:1::2/64)
    eth2 -> kind-switch (192.168.2.1/24, fd00:2::1/64)
  BGP: AS 64512, 2 peers:
    - spine (AS 65000, 192.168.1.1, fd00:1::1) EVPN enabled
    - kind-control-plane (AS 64512, 192.168.2.10, fd00:2::10) EVPN enabled
    - kind-worker (AS 64512, 192.168.2.11, fd00:2::11) EVPN enabled

Configuration applied successfully.
```

### Persistence

The summary can be regenerated at any time using:

```bash
clab-config summary --state 
```

This allows users to review the configuration without re-running the full generation process.

## IP Assignment Strategy

### Point-to-Point Links

The system automatically allocates IP addresses for all point-to-point links between routers:

- **IPv4**: `/31` subnets (2 usable addresses per link)
- **IPv6**: `/127` subnets (2 usable addresses per link)

Allocation occurs from configurable base ranges (e.g., `192.168.0.0/16` for IPv4, `fd00::/48` for IPv6).

### Switch/Broadcast Networks

Switches (bridge nodes in containerlab) represent broadcast domains and require different handling:

- **IPv4**: `/24` subnet (254 usable addresses)
- **IPv6**: `/64` subnet

All interfaces connected to the same switch receive IPs from the same subnet. This supports scenarios like the `leafkind-switch` which connects multiple nodes (leafkind, pe-kind-control-plane, pe-kind-worker).

### Special Allocations

Additional IP ranges may be reserved for:

- **VTEP addresses**: Loopback IPs for VXLAN tunnel endpoints
- **Router IDs**: BGP router identifiers (typically IPv4 addresses)

## Automatic Property Assignment

The system automatically assigns variable properties that would otherwise require manual specification for each node:

### VTEP IPs (for edge leaves)

The VTEP IPs are allocated from a dedicated range (e.g., `10.0.1.0/24`), and the system assignes one unique IP per edge leaf node

### MAC Addresses
- Generated deterministically or randomly for VXLAN interfaces
- Ensures uniqueness across the topology
- Format: Locally administered MAC addresses (e.g., `02:xx:xx:xx:xx:xx`)

### BGP Router IDs
- Typically derived from VTEP IPs or allocated from a separate range
- One per BGP-enabled router
- Must be unique across all BGP speakers

## Configuration Introspection API

Since most configuration is now automatically generated, a query interface is essential for both operational use and testing.

### CLI Interface

A json variant of the summary command allows users and automations to check the current state of the fabric:

```bash
clab-config summary --state -o json
```

This would allow to perform query operations such as:

- get the interface ip between two nodes
- find which interface a given ip belongs to
- get the vtep ip of a specific leaf

### Go API

For programmatic access (especially in e2e tests):

```go
// Load topology configuration
topo, err := config.Load("topology-config.yaml")
if err != nil {
    log.Fatal(err)
}

// Query node properties
vtepIP := topo.GetNodeVTEP("leafA")
linkIP := topo.GetLinkIP("leafA", "spine", config.IPv4)

// Get all nodes matching a pattern
leaves := topo.GetNodesByPattern("leaf.*")

// Reverse lookup
link, iface := topo.FindIPOwner("192.168.0.1")
```

### Use Cases

This introspection capability enables:

1. **E2E tests**: Dynamically discover topology parameters without hardcoding
2. **Debugging**: Quickly identify which node/interface owns an IP address
3. **Documentation**: Generate topology diagrams and documentation automatically
4. **Validation**: Verify configuration correctness before deployment

## Implementation Details

### Architecture

A single Go-based tool (`clab-config`) serves as the central configuration manager with the following responsibilities:

#### Core Functions

1. **Resource Allocation**
   - Allocate IP addresses for all links (point-to-point and broadcast)
   - Assign VTEP IPs for edge leaves
   - Generate unique MAC addresses for VXLAN interfaces
   - Assign BGP router IDs

2. **Configuration Generation**
   - Generate FRR configuration files for each router node
   - Generate node-specific `setup.sh` scripts for host configuration
   - Create containerlab bind mounts configuration

3. **State Management**
   - Persist allocated resources to a state file (e.g., `topology-state.json`)
   - Support idempotent operations (re-running produces same allocations)
   - Enable state versioning for reproducibility

4. **Query Interface**
   - Implement CLI commands for querying topology state
   - Provide Go library for programmatic access
   - Support both individual queries and bulk exports

### Environment Composition

Each deployment environment is composed of two files working together:

1. **Containerlab topology file** (e.g., `kind.clab.yml`)
   - Defines nodes, their types, and container images
   - Specifies physical links between nodes
   - Sets up volumes and bind mounts
   - Contains infrastructure-level configuration

2. **Topology configuration file** (e.g., `topology-config.yaml`)
   - Defines logical network behavior
   - Specifies routing protocols and parameters
   - Declares VRFs and VNIs
   - Sets IP allocation ranges

**Example pairing:**
```
clab/basic
├── kind.clab.yml              # Physical topology
└── topology-config.yaml       # Logical configuration
```

The tool reads both files to produce a complete, deployable environment. This allows:
- The same physical topology to support multiple configurations (e.g., EVPN vs SRv6)
- Reusing common topologies across different test scenarios
- Version controlling infrastructure and configuration separately


### Integration with E2E Tests

The e2e tests will migrate from the current hardcoded "links" logic to using the Go API:

**Before:**
```go
// Hardcoded topology assumptions
leafASpineIP := "192.168.1.1"
vtepIP := "10.0.1.1"
```

**After:**
```go
// Dynamic topology queries
topo := config.MustLoad("clab/topology-config.yaml")
leafASpineIP := topo.GetLinkIP("leafA", "spine", "eth1", config.IPv4)
vtepIP := topo.GetNodeVTEP("leafA")
```

This decouples tests from specific topology implementations, allowing topology changes without test modifications.

