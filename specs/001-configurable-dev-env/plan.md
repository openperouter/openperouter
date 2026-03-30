# Implementation Plan: Configurable Development Environment

**Branch**: `001-configurable-dev-env` | **Date**: 2026-02-24 | **Spec**: [spec.md](spec.md)
**Input**: Feature specification from `/specs/001-configurable-dev-env/spec.md`

## Summary

Build a Go CLI tool (`clab-config`) and companion library that reads containerlab topology files and a declarative environment configuration file, then automatically allocates network resources (IPs, VTEPs, MACs, router IDs), generates FRR configurations and setup scripts, and provides introspection via CLI queries and a Go API. This replaces the current fragmented configuration approach where IPs, FRR configs, and setup scripts are manually maintained and hardcoded in e2e tests.

## Technical Context

**Language/Version**: Go 1.25
**Primary Dependencies**: `github.com/apparentlymart/go-cidr` (existing), `github.com/spf13/cobra` (available via controller-runtime), `gopkg.in/yaml.v3`, `text/template` (stdlib)
**Storage**: JSON state file on local filesystem
**Testing**: `go test` with table-driven tests; Ginkgo/Gomega for integration with existing e2e test suite
**Target Platform**: Linux (development workstations, CI)
**Project Type**: CLI tool + Go library
**Performance Goals**: Generate configuration for 10+ node topologies in under 1 second
**Constraints**: Must be idempotent; standalone binary with no runtime dependencies beyond generated files
**Scale/Scope**: Topologies with 2-20 nodes, 5-40 links

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principle | Status | Notes |
|-----------|--------|-------|
| I. Go Idiomatic Code Quality | PASS | New Go code follows line-of-sight, proper package naming, error wrapping |
| II. Kubernetes-Native Design | N/A | This tool operates outside the Kubernetes cluster — it's a dev/test utility for containerlab. No CRDs or controllers involved |
| III. Network Namespace Isolation | N/A | Tool generates configs; does not manipulate namespaces at runtime |
| IV. FRR Integration Integrity | PASS | Generates FRR configuration via templates; does not manipulate FRR state directly |
| V. Testing Strategy | PASS | Unit tests for allocation logic, template generation, config parsing. E2e tests migrate to use the Go API |
| VI. Documentation Alignment | PASS | Quickstart and contracts documented; will update contributing docs |
| VII. Configuration as Code | PASS | Environment config is versioned YAML; generated state is reproducible |
| VIII. Simplicity and YAGNI | PASS | Minimal viable CLI with 3 subcommands. No speculative features. Library API covers only documented query needs |

**Post-Phase 1 re-check**: All gates still pass. The `internal/clabconfig/` package is internal to the module, consistent with existing package organization. No new external dependencies beyond what's already available.

## Project Structure

### Documentation (this feature)

```text
specs/001-configurable-dev-env/
├── plan.md              # This file
├── spec.md              # Feature specification
├── research.md          # Phase 0 research decisions
├── data-model.md        # Phase 1 data model
├── quickstart.md        # Phase 1 quickstart guide
├── contracts/           # Phase 1 interface contracts
│   ├── cli.md           # CLI subcommands and flags
│   └── config-schema.md # Environment config YAML schema
└── tasks.md             # Phase 2 output (created by /speckit.tasks)
```

### Source Code (repository root)

```text
cmd/clab-config/
├── main.go              # CLI entry point, cobra root command
├── apply.go             # apply subcommand
├── summary.go           # summary subcommand
└── query.go             # query subcommand

internal/clabconfig/
├── config.go            # Environment config types and YAML parsing
├── clab.go              # Containerlab topology types and YAML parsing
├── allocator.go         # Deterministic IP, MAC, router ID allocation
├── generator.go         # FRR config and setup script generation
├── state.go             # TopologyState type, persistence, queries
├── validate.go          # Input validation (patterns, interfaces, ranges)
├── templates/
│   ├── edge-leaf.frr.tmpl    # FRR template for edge-leaf nodes
│   ├── transit.frr.tmpl      # FRR template for transit nodes
│   └── setup.sh.tmpl         # Setup script template for edge-leaf nodes
└── testdata/
    ├── basic.clab.yml         # Test fixture: minimal topology
    ├── basic-config.yaml      # Test fixture: minimal environment config
    └── expected/              # Expected outputs for golden-file tests

e2etests/pkg/infra/
├── topology.go          # New: loads topology via clabconfig, replaces hardcoded values
├── leaf.go              # Modified: remove hardcoded IPs, use topology queries
├── routers.go           # Modified: remove hardcoded links, use topology queries
└── nodes.go             # Modified: remove hardcoded node names if needed
```

**Structure Decision**: New code lives in `cmd/clab-config/` (CLI binary) and `internal/clabconfig/` (library), following the existing project convention where binaries are in `cmd/` and internal packages in `internal/`. The `internal/` placement ensures the library is only consumed within this module (by the CLI and e2e tests).

## Complexity Tracking

> No constitution violations to justify.
