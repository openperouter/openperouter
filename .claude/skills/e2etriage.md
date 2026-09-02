---
name: e2etriage
description: Triage e2e test failures by analyzing cluster dumps, logs, CRDs, and FRR state from the reporterpath directory
trigger: triage e2e, triage test failure, analyze test logs, why did the test fail, debug e2e
---

# Triage e2e test failures

When e2e tests fail, each failed test dumps the full cluster state into a subdirectory under the reporterpath. When running via `make e2etests`, the reporterpath is `/tmp/kind_logs`.

## Step 1: Find the failed test directories

List the subdirectories under `/tmp/kind_logs` (or the reporterpath the user provides). Each subdirectory corresponds to a failed test — the directory name is the sanitized test name (non-alphanumeric characters replaced with `_`).

## Step 2: Understand the directory structure

Each failed test directory contains these files:

### Kubernetes state (from k8sreporter)

| File | Content |
|---|---|
| `nodes.log` | JSON dump of all Kubernetes Node objects |
| `events.log` | JSON dump of Kubernetes events from `openperouter-system` and `frr-k8s-system` namespaces |
| `<namespace>_<podname>_pods_specs.log` | JSON dump of full Pod spec for each pod in the watched namespaces |
| `<namespace>_<podname>_pods_logs.log` | Container logs (current + previous, last 10 minutes) for each pod |
| `UnderlayList.log` | JSON dump of all Underlay CRD instances |
| `L3VNIList.log` | JSON dump of all L3VNI CRD instances |
| `L2VNIList.log` | JSON dump of all L2VNI CRD instances |
| `FRRConfigurationList.log` | JSON dump of all FRRConfiguration CRD instances |

### FRR state (from dumpFRRInfo)

Files named `frrdump-<name>.log` where `<name>` is a router pod, frr-k8s pod, or clab leaf container. Each contains:
- FRR version, running config
- BGP summary, neighbor state, advertised routes per neighbor
- IPv4/IPv6/EVPN routing tables
- Interface info, ip link/address/neigh, bridge FDB
- VRF info, all routing tables
- Optionally frr.log (from clab leaf containers)

### Workload pod network info (from dumpWorkloadInfo)

| File | Content |
|---|---|
| `pod-list-namespace-<ns>.log` | YAML list of all pods in the namespace |
| `pod-container-dump-<ns>-<podname>.log` | Network diagnostics from inside the pod: ip link, address, neigh, routes |

### Hostmode only (from dumpPodmanInfo)

| File | Content |
|---|---|
| `podmandump.log` | Podman pods, containers, and last 10 minutes of container logs from each node |

## Step 3: Triage approach

1. **Start with the test name** — the directory name tells you which test failed. Map it back to a test in `e2etests/tests/`.
2. **Check events.log** — look for warnings, errors, failed scheduling, crashloops, or image pull failures.
3. **Check pod logs** — look at `*_pods_logs.log` files for the controller and router pods for errors or panics.
4. **Check pod specs** — look at `*_pods_specs.log` for pods in CrashLoopBackOff, not Ready, or missing containers.
5. **Check CRD state** — compare `UnderlayList.log`, `L3VNIList.log`, `L2VNIList.log` against what the test expected to configure.
6. **Check FRR state** — the `frrdump-*.log` files are critical. Look for:
   - BGP sessions not established (check `show bgp vrf all summary` section)
   - Missing routes in the routing tables
   - Wrong or missing FRR running config
   - EVPN routes not propagated (`show bgp l2vpn evpn` section)
   - Missing bridge FDB entries
7. **Check workload pods** — `pod-container-dump-*.log` files show the network state inside test workloads. Look for missing routes or addresses.
8. **In hostmode** — also check `podmandump.log` for podman container crashes or missing containers.

## Example

For a failed test `L3VNI - VRF Traffic - should allow traffic between pods in the same VRF`:

```
/tmp/kind_logs/
  L3VNI___VRF_Traffic___should_allow_traffic_between_pods_in_the_same_VRF/
    nodes.log
    events.log
    openperouter-system_router-abcde_pods_specs.log
    openperouter-system_router-abcde_pods_logs.log
    openperouter-system_controller-manager-xyz_pods_specs.log
    openperouter-system_controller-manager-xyz_pods_logs.log
    frr-k8s-system_frr-k8s-daemon-f88qn_pods_specs.log
    frr-k8s-system_frr-k8s-daemon-f88qn_pods_logs.log
    UnderlayList.log
    L3VNIList.log
    L2VNIList.log
    FRRConfigurationList.log
    frrdump-pe-kind-worker.log
    frrdump-pe-kind-control-plane.log
    frrdump-clab-kind-leafA.log
    frrdump-frr-k8s-daemon-f88qn.log
    pod-list-namespace-l3vni-test.log
    pod-container-dump-l3vni-test-pod1.log
    pod-container-dump-l3vni-test-pod2.log
```

A typical triage flow for this test:
1. Read `events.log` — are all pods running? Any scheduling or image issues?
2. Read `openperouter-system_router-*_pods_logs.log` — did the controller configure the VRF correctly?
3. Read `L3VNIList.log` — is the L3VNI CRD created with the right VNI and VRF?
4. Read `FRRConfigurationList.log` — did frr-k8s get the expected FRRConfiguration?
5. Read `frrdump-pe-kind-worker.log` — is the BGP session up? Are VRF routes present? Check `show bgp vrf all summary` and `show ip route`.
6. Read `frrdump-clab-kind-leafA.log` — is the leaf seeing EVPN routes from the router? Check `show bgp l2vpn evpn`.
7. Read `pod-container-dump-l3vni-test-pod1.log` — does the pod have the right IP and routes inside its netns?
