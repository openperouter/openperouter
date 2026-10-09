# Reconciliation benchmark fixtures

Each direct subfolder is a complete static router configuration set. Only its `openpe_*.yaml` files are loaded; other folders and README files are ignored. Files use the existing static `PERouterConfig` block format (`underlays`, `l3vnis`, `l2vnis`, `l3vpns`), rather than Kubernetes resource manifests. Loading, merging and defaults happen outside measurement.

| Folder | Underlays | L3VNIs | L2VNIs | L3VPNs |
|---|---:|---:|---:|---:|
| vxlan | 1 | 3 | 3 | 0 |
| vxlan-srv6 | 1 | 3 | 3 | 3 |

Both configurations use a dummy `bench-underlay` device, a host session for every L3 domain, and a managed Linux host bridge for every L2VNI. The harness creates the underlay in an isolated source namespace, and reconciliation moves it into a separate router namespace. The SRv6 set adds an ISIS/SRv6 underlay and three L3VPN host sessions with distinct identifiers and address ranges. No OVS service, CNI plugin, live FRR or cluster is required.

Each folder runs as `BenchmarkReconcile/<folder>/FirstApplication` and `BenchmarkReconcile/<folder>/RepeatedReconcile`. First application starts from fresh network namespaces and prerequisite interfaces. Repeated reconciliation starts after untimed successful convergence. FRR conversion/template rendering and kernel datapath operations are measured; the FRR updater is mocked. Fixture loads, per-iteration snapshots, namespace setup, initial convergence and teardown are excluded. Production bridge refresh workers remain enabled; their asynchronous allocations can add noise to B/op and allocs/op.

## Add a scenario

1. Add a direct subfolder with valid `openpe_*.yaml` blocks. Use one NetworkDevice underlay, distinct routing identifiers and valid routing-domain references.
2. Add an entry to `benchmarkScenarios` in `reconcile_benchmark_test.go`, specifying a stable name and directory. Names and directories need not be identical.
3. Build the package test binary and select the new scenario with `-test.bench='^BenchmarkReconcile$/^<name>$' -test.benchmem -test.benchtime=3x` using namespace privileges. Both starting-state cases use the existing harness; do not copy setup logic.

Required kernel features include dummy, VRF, bridge, veth, VXLAN and namespace-scoped forwarding sysctls. The combined set also requires SRv6 sysctls and strict VRF support. Missing capabilities or mandatory kernel features fail requested benchmarks, with no unisolated fallback.
