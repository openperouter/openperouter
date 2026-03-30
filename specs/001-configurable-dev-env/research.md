# Research: Configurable Development Environment

**Branch**: `001-configurable-dev-env` | **Date**: 2026-02-24

## R-001: Containerlab Topology File Parsing

**Decision**: Parse the containerlab `.clab.yml` file using standard YAML unmarshalling into Go structs. Only the `topology.nodes` and `topology.links` sections are needed.

**Rationale**: The containerlab YAML format is well-defined and stable. The project already uses YAML parsing for Kubernetes manifests. We need only a subset of the schema (nodes with their `kind`, and links with `endpoints`). No need to import the containerlab library itself — a minimal struct covering the fields we consume keeps dependencies light.

**Alternatives considered**:
- Importing containerlab as a Go library: rejected due to heavy transitive dependency chain and coupling to containerlab internals.
- Using a generic YAML map: rejected because typed structs provide compile-time safety and better documentation.

## R-002: Pattern Matching Strategy

**Decision**: Use Go's `regexp` standard library for pattern matching of node names against configuration patterns.

**Rationale**: The enhancement document uses regex-style patterns throughout (`leaf[AB]`, `leaf.*`, `spine`). Go's `regexp` package provides POSIX-compatible regex without external dependencies. Patterns are compiled once at configuration load time and matched against each node name.

**Alternatives considered**:
- Glob matching (filepath.Match): rejected because it doesn't support `.*` or character classes like `[AB]` — the exact patterns used in the enhancement examples.
- Custom matching: rejected — standard regex covers all documented use cases.

## R-003: IP Address Allocation Strategy

**Decision**: Use deterministic allocation based on sorted node names and sorted link pairs. Allocate from configurable CIDR ranges using the existing `go-cidr` library already in the project (`github.com/apparentlymart/go-cidr`).

**Rationale**: Deterministic ordering (alphabetical by node name, then by peer name for links) ensures idempotency (FR-018) without requiring state from previous runs. The `go-cidr` library is already a project dependency used in `internal/ipam/`. Point-to-point links use /31 (IPv4) and /127 (IPv6) subnets; broadcast networks use /24 and /64.

**Alternatives considered**:
- Random allocation with state persistence: rejected because it adds complexity and breaks idempotency when state is lost.
- Declaration-order allocation: rejected because reordering config entries would change allocations, violating the principle of least surprise.

## R-004: FRR Configuration Generation

**Decision**: Use Go `text/template` for FRR configuration generation, with separate templates for edge-leaf and transit node roles.

**Rationale**: The project already uses Go templates for FRR config generation in e2e tests (`e2etests/pkg/infra/testdata/leaf.tmpl`, `leafkind.tmpl`). Using the same approach provides consistency. Templates will be embedded in the binary using `embed.FS` for portability.

**Alternatives considered**:
- Programmatic string building: rejected — templates are more readable and maintainable for FRR config syntax.
- External template files on disk: rejected for the CLI tool — embedded templates ensure the binary is self-contained.

## R-005: State Persistence Format

**Decision**: Use JSON for the state file format. The state file records all allocated resources (IPs, MACs, router IDs) and the input configuration hash.

**Rationale**: JSON is human-readable for debugging, supported natively in Go, and straightforward to version. The state file enables the `summary --state` command and introspection queries without re-running allocation. An input hash allows detecting when re-generation is needed.

**Alternatives considered**:
- YAML: would work but adds inconsistency — the tool already outputs JSON for `--json` flag.
- Binary/gob: rejected — not human-readable, harder to debug.

## R-006: CLI Tool Architecture

**Decision**: Build `clab-config` as a standalone CLI tool in `cmd/clab-config/` with subcommands: `apply`, `summary`, and `query`. Use the Go `cobra` library for CLI structure (already an indirect dependency through controller-runtime).

**Rationale**: A standalone binary keeps the tool independent of the Kubernetes operator. Cobra is the standard CLI framework in the Go ecosystem and is already available as a transitive dependency. The tool can also expose its core logic as a Go package (`internal/clabconfig/`) for programmatic access from e2e tests.

**Alternatives considered**:
- Using `flag` package directly: rejected — subcommand routing is cumbersome without a framework.
- Integrating into an existing binary: rejected — the tool operates on containerlab topologies independently of the Kubernetes cluster.

## R-007: Setup Script Generation

**Decision**: Use Go `text/template` for setup script generation, with templates covering VXLAN interface creation, VRF setup, and IP address assignment.

**Rationale**: The existing setup scripts (`clab/leafA/setup.sh`, etc.) follow a consistent pattern: sysctl settings, VTEP IP on loopback, VRF creation, bridge/VXLAN interface setup. These patterns are parameterizable via templates with node-specific values (VTEP IP, VRF names, VNI IDs, MAC addresses).

**Alternatives considered**:
- Generating scripts line-by-line in Go code: rejected — less readable and harder to maintain than templates.

## R-008: MAC Address Generation

**Decision**: Generate MAC addresses deterministically from a hash of the node name and VNI, using the locally administered range (`02:xx:xx:xx:xx:xx`).

**Rationale**: Deterministic generation ensures idempotency without state tracking. Using node name + VNI as input guarantees uniqueness across the topology (since FR-022 prevents overlapping patterns, each node-VNI pair is unique). The `02` prefix marks them as locally administered per IEEE 802.

**Alternatives considered**:
- Random generation with collision detection: rejected — breaks idempotency.
- Sequential assignment: rejected — depends on allocation order, fragile across topology changes.

## R-009: Integration with E2E Tests

**Decision**: Expose the topology query functionality as a Go package (`internal/clabconfig/`) that e2e tests import directly. The package provides functions like `GetNodeVTEP()`, `GetLinkIP()`, `FindIPOwner()`, and `GetNodesByPattern()`.

**Rationale**: The e2e tests already live in the same Go module (`github.com/openperouter/openperouter`). Direct package import avoids the overhead of shelling out to the CLI tool and provides type-safe access. The existing hardcoded values in `e2etests/pkg/infra/` (leaf.go, routers.go, nodes.go) will be replaced with calls to this package.

**Alternatives considered**:
- CLI-only access via exec: rejected — adds process overhead and requires JSON parsing in tests.
- Generating Go source code: rejected — adds a build step and compilation dependency.
