# Validation Guide: Existing-API VLAN-Aware VNI Reimplementation

This guide is for implementation validation. No deployment or tests were executed during planning. Run from the repository root after implementation. See [compatibility contract](contracts/compatibility.md) and [data model](data-model.md) for expected behavior.

## Prerequisites

- Go 1.26.4 and the repository's normal build/test tools.
- Docker/container engine, Kind, ContainerLab, privileged namespace/OVS testing support, and the existing development environment requirements in `website/content/docs/contributing/`.
- Target host kernel with VLAN filtering, VRF, macvlan, external VXLAN and VNI-filter support; upstream 5.18 contains the required VNI-filter functionality. Verify actual operations, not only `uname -r`.
- The existing FRR 10.6.0 image and iproute2 diagnostic commands (`bridge vlan`, `bridge vni`). Containers use the host kernel.

Deploy using the `deploy` skill and execute suites using `e2e-tests`. Detect deployment mode with that skill before testing; do not infer a mode from a connectivity or authorization error. The CI filters below are the acceptance baseline. The skill's current hostmode prose has broader exclusions; retain the CI filters explicitly rather than reducing coverage.

## Local checks

```sh
make test
make lint
```

`make test` includes the privileged `runasroot` hostnetwork/bridge-refresh suites in an OVS-enabled container. Plain `go test ./internal/hostnetwork` omits the necessary topology tests. Expected: ordinary tests and privileged race-enabled tests pass, including new topology and fault-repair cases.

Inspect the final diff for zero public API/static/schema changes and no modifications to e2e assertions, manifests, deadlines or filters. Generated schema/chart/operator copies must remain compatible with the existing baseline.

## Early US1 validation

Compile and run the host-network suite through the same OVS-enabled harness used by `make test`:

```sh
go test -tags=runasroot -c -race -o bin/hostnetwork.test ./internal/hostnetwork
docker run --rm --privileged -v "$PWD":/src -w /src \
  --entrypoint /src/hack/integration_tests.sh \
  quay.io/openperouter/kind-node-openperouter:v1.32.2 /src/bin/hostnetwork.test
```

For a quick bridge/VXLAN-only check on a host with passwordless sudo (OVS is not needed for this focus):

```sh
sudo -n bin/hostnetwork.test \
  -ginkgo.focus='VLAN-aware VNI endpoints|Filtered external VXLAN'
```

After `make docker-build` and `make deploy`, detect kernel pod mode and run the unchanged disconnected scenarios:

```sh
KUBECONFIG=bin/kubeconfig make e2etests \
  GINKGO_ARGS="--label-filter='!systemdmode && !grout-only' --focus='Disconnected L2VNI east/west traffic'"
```

Use fresh router namespaces for this early checkpoint. Legacy conversion is validated separately by the lifecycle regressions. These focused runs do not replace the full compatibility matrix below.

## Kernel pod deployment and full suite

Through `deploy`, build then deploy:

```sh
make docker-build
make deploy
```

After the mode check, run through `e2e-tests` with the existing pod filter:

```sh
KUBECONFIG=bin/kubeconfig make e2etests \
  GINKGO_ARGS="--label-filter='!systemdmode && !grout-only'"
```

## Hostmode deployment and full suite

Build and deploy through `deploy` for hostmode, sequentially:

```sh
make docker-build
make deploy-hostmode
```

After detecting hostmode, preserve the current `.github/workflows/ci.yaml` filters:

```sh
KUBECONFIG=bin/kubeconfig make e2etests \
  TEST_ARGS="--systemdmode" \
  GINKGO_ARGS="--label-filter='!grout-only && !podmode-only' --skip='North/south traffic after FRR container restart' --skip='Alpha: Named netns' --skip='Beta: Named netns'"
```

Validate the existing static-before-API lane on its separately provisioned boot environment, following the CI setup:

```sh
make deploy-hostmode-boot
KUBECONFIG=bin/kubeconfig make e2etests-hostmode-boot
```

Do not run deployment-mode transitions concurrently. The boot lane establishes that unchanged static configuration works before the API controller is installed. Existing CI packaging variants remain required; these local runs are not substitutes for their CI results.

## Grout regression

Provision Grout separately through `deploy`:

```sh
make grout-docker-build
make grout-deploy
```

After mode detection:

```sh
KUBECONFIG=bin/kubeconfig make e2etests \
  TEST_ARGS="--groutmode" \
  GINKGO_ARGS="--label-filter='grout-support || grout-only'"
```

Expected: shared naming, host attachments and refresher behavior do not regress. The new Linux VLAN implementation must not be applied to Grout.

## Targeted runtime observations

Use the existing router executor for the selected node/mode, as used by e2e helpers. The following commands run **inside that router's network namespace**, not the host namespace:

```sh
ip -d link show br-vl-110
ip -d link show vni110
bridge vlan show
bridge vlan tunnelshow dev vni110
bridge vni show dev vni110
ip -d link show br-pe-110
ip addr show dev br-pe-110
ip neigh show dev br-pe-110
bridge fdb show dev vni110
bridge fdb show br br-vl-110
vtysh -c 'show evpn vni'
vtysh -c 'show vrf vni'
```

Expected: VLAN filtering with default PVID 0; VLAN 1 memberships; exactly one 1→110 tunnel mapping and VNI 110 receive-filter entry; external+vnifilter VXLAN; unchanged gateway addresses and deterministic gateway MAC; expected VRF binding and EVPN L2/L3 classification. The gateway endpoint owns workload neighbor entries. L3 endpoints have no configured IP addresses or generated IPv6 link-local addresses.

Capture traffic at the host-facing veth and on the underlay while existing connectivity/maximum-MTU scenarios run. Verify the router introduces no host-facing VLAN header, no additional inner VLAN header in VXLAN payloads, and no reduction in existing maximum usable payload. Use privileged regression tests to prove missing membership/filter/mapping/FDB repair, deletion/recreation, legacy conversion, same-port L2/L3 dispatch, and isolation between dedicated bridges reusing VLAN 1.

## Acceptance evidence

Record commit/build identity, kernel/FRR versions, deployment mode, exact filter, test counts/pass/failure/skip totals, and JSON reports/log artifacts produced by `make e2etests`. Compare counts/skips against the existing mode baseline. Focused runs help investigate a failure but do not satisfy the full-suite gate.

Required existing scenarios include disconnected EVPN, Linux/OVS/omitted host-master attachments, shared-L3VNI symmetric IRB, mixed L3VPN/L3VNI, RT/RD policy, IPv6 underlays, maximum MTU, silent-workload route persistence, workload movement, FRR/Zebra restarts and systemd recovery. Use `e2etriage` to investigate failures. Completion requires the unchanged suites to pass; planning has not established that result.
