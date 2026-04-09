# Status: 006-multi-underlay-neighbors

**Last updated**: 2026-04-09 (session 3)

---

## What Was Done This Session (session 3)

### Bug Found and Fixed: L3 Connectivity (the long-standing failure)

**Tests affected**: `Single Session Baseline > verifies L3 connectivity from external host to pod`

**Root cause**: `leafA` and `leafB` do not redistribute their connected VRF routes into BGP by default. Because of this, they never advertise their local subnets (e.g. `192.168.20.0/24` for `hostA_red`) as EVPN type-5. Consequently:
- The router namespace VRF red had no return path to external hosts.
- `frr-k8s` received no routes for external subnets via the host session.
- The host network had no route to reach `hostA_red`, so the pod's TCP response could not be forwarded back.

**How the correct fix works** (existing test infrastructure already knew how to do this):
- `clab/tools/generate_leaf_config/` has a Go generator with `--redistribute-connected-from-vrfs` flag.
- `e2etests/pkg/infra/testdata/leaf.tmpl` already supports `RedistributeConnected` in the `Addresses` struct.
- The helper `redistributeConnectedForLeaf(leaf)` in `e2etests/tests/leaf.go` dynamically reloads the leaf FRR config with `redistribute connected` enabled.
- Tests that need L3 connectivity (`bridgerefresh`, `evpn_l2`, `evpn_routes`, `frr_restart`, `passthrough_routes`) already call this helper.

**Fix applied**: Added `redistributeConnectedForLeaf(infra.LeafAConfig)` and `redistributeConnectedForLeaf(infra.LeafBConfig)` to `singlesession.go` `BeforeAll`, with cleanup in `AfterAll`.

**File changed**: `e2etests/tests/singlesession.go`

### Tests Run This Session

| Test | Result |
|------|--------|
| `Single Session Baseline > establishes BGP session with TOR` | PASS |
| `Single Session Baseline > verifies L3 connectivity from external host to pod` | PASS |
| `Multi-Session Multi-Underlay` (4 tests) | PASS |
| Full suite (91 specs) | INTERRUPTED — partial results below |

### Full Suite Partial Results (before interrupt)

~33 specs ran, then interrupted. Two failures were visible:

1. **Webhook test — "invalid vtep CIDR"** (`webhooks.go:604`):
   - Expected: `"invalid vtep CIDR"`
   - Got: `"underlay must have at least one neighbor configured"`
   - Cause: webhook validates neighbors before VTEP CIDR, so the neighbor error fires first.
   - This is a pre-existing validation ordering bug in the webhook.

2. **Webhook BeforeEach — "second different underlay should fail"** (`webhooks.go:696`):
   - Tries to create `underlay1` with zero neighbors, which fails with neighbor error instead of the expected "different underlay" error.
   - Same root cause as above.

3. **`hostconfiguration > works while editing the underlay parameters`** (`hostconfiguration.go:419`):
   - Edits underlay NIC to `toswitch-nonexistent`, then waits 60s for router pod rollout.
   - Times out: old pod `router-fmcxd` not deleted.
   - Likely a test environment slowness or controller hang when the NIC doesn't exist.

These failures need investigation before T071 (full e2e suite) can be declared complete.

---

## What Was Done in Previous Sessions (sessions 1 & 2)

### Session 2: `lound` interface fix, cluster redeploy

**File**: `internal/hostnetwork/underlay.go` — `ensureLoopback()` now calls `netlink.LinkSetUp(loopback)` to bring `lound` UP after creation.

**Cluster redeploy workarounds**:
- `kind load docker-image` fails with `--all-platforms` → fixed `clab/common.sh` to use `ctr import --snapshotter=overlayfs` directly.
- `setup.sh` exits early if kind cluster exists → run scripts 6-10 manually.

### Session 1: Multi-underlay bugs fixed

1. `internal/hostnetwork/underlay.go` — `moveUnderlayInterface` rewritten for multi-underlay.
2. `clab/check_veths.sh` — 5th field (bridge name) now extracted and passed to `ensure_veth`.
3. `clab/scripts/10-veth-monitoring.sh` — toswitch2 entries added with `leafkind2-sw` bridge.
4. `clab/singlecluster/ip_map.txt` — leafkind2 and pe-kind toswitch2 IP entries added.

---

## Current E2E Test Status

### Confirmed Passing (isolated runs)
- `Single Session Baseline` (both tests) — PASS
- `Multi-Session Multi-Underlay` (4 tests) — PASS
- `Router Host configuration > peers with both TOR switches` — PASS (isolated)
- `evpn_routes`, `passthrough_routes` — PASS (confirmed in prior sessions)

### Failures to Investigate
- Webhook validation ordering: neighbor error fires before VTEP CIDR error
- `hostconfiguration > works while editing the underlay parameters` rollout timeout (60s)

### Not Yet Run
- T071: Full `make e2e-test` pass
- T072: Backward compatibility
- T073: Performance test

---

## What Still Needs Doing

- **T043**: ✅ Multi-session tests pass — done.
- **T044**: CI verification
- **T045**: quickstart.md documentation
- **T071**: Full `make e2e-test` run (webhook ordering bug and hostconfig rollout timeout need fixing first)
- **T072**: Backward compatibility verification
- **T073**: Performance test
- **T076**: Code review — webhook validation order fix
- **T077**: Final quickstart.md walkthrough
- **Phase 7 (US4: hot-apply)** — not started, lower priority

---

## Infrastructure State (as of session 3 end)

- Containerlab: deployed, both `clab-kind-leafkind` and `clab-kind-leafkind2` running
- Kind cluster: `pe-kind` with control-plane and worker nodes
- Veth monitoring: running with 4 entries
- Controller image: rebuilt with `lound` fix loaded into kind
- `leafA/frr.conf` and `leafB/frr.conf`: regenerated without `redistribute connected` (correct default; tests call `redistributeConnectedForLeaf` at runtime)
- BGP sessions: NOT established (no underlay applied after full suite run)

---

## How to Resume

```bash
export KUBECONFIG=/home/fpaoline/openperouter1/bin/kubeconfig

# Run singlesession:
KUBECONFIG=/home/fpaoline/openperouter1/bin/kubeconfig \
  ./bin/ginkgo --focus "Single Session Baseline" --timeout 10m ./e2etests/suite/... \
  -- --kubectl=/home/fpaoline/openperouter1/bin/kubectl \
     --hostvalidator=/home/fpaoline/openperouter1/bin/validatehost

# Run multi-session:
KUBECONFIG=/home/fpaoline/openperouter1/bin/kubeconfig \
  ./bin/ginkgo --focus "Multi-Session Multi-Underlay" --timeout 15m ./e2etests/suite/... \
  -- --kubectl=/home/fpaoline/openperouter1/bin/kubectl \
     --hostvalidator=/home/fpaoline/openperouter1/bin/validatehost

# Run full suite:
KUBECONFIG=/home/fpaoline/openperouter1/bin/kubeconfig \
  ./bin/ginkgo --timeout 60m ./e2etests/suite/... \
  -- --kubectl=/home/fpaoline/openperouter1/bin/kubectl \
     --hostvalidator=/home/fpaoline/openperouter1/bin/validatehost
```

---

## Key Files Changed This Session (session 3)

| File | Change |
|------|--------|
| `e2etests/tests/singlesession.go` | Added `redistributeConnectedForLeaf` calls in BeforeAll/AfterAll for L3 connectivity |
