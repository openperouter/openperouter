# Quickstart: Validate RD Assigned Numbers

This is a validation guide for the implementation described in [plan.md](plan.md). Run it after the VNI API, conversion, validation, generated manifests, and focused e2e scenario are implemented. The resource shape and expected outcomes are in [contracts/overlay-rd-api.md](contracts/overlay-rd-api.md).

## Prerequisites

- Linux development host with the repository's Go, container, kind, kubectl, and Make prerequisites.
- A Kubernetes-mode development cluster with two OpenPERouter router nodes and the existing fabric leaf containers.
- `bin/kubeconfig` from the deployment target.

## 1. Generate and verify the public contract

From the repository root:

```sh
make generate-all-ci
make test
make lint
```

Expected: generation succeeds; the L2VNI and L3VNI CRD schemas and API reference show optional `rdAssignedNumber` with 1–65535 bounds; L3VPN retains its required field; unit tests and lint pass. Review generated manifest changes before proceeding.

## 2. Deploy Kubernetes mode

```sh
make docker-build
make deploy
KUBECONFIG=bin/kubeconfig kubectl get nodes
KUBECONFIG=bin/kubeconfig kubectl -n openperouter-system get pods -o wide
```

Expected: at least two nodes and their router pods are ready. The default development mode is Kubernetes mode; hostmode or Grout require their own deployment and test flags.

## 3. Run the focused end-to-end cases

The L2VNI and L3VNI leaf-route cases have `RD assigned number` in their names. Run them with the Kubernetes-mode filter:

```sh
make e2etests TEST_ARGS="" GINKGO_ARGS="--label-filter='!systemdmode && !grout-only' --focus='RD assigned number'"
```

The L2VNI case verifies that two originating nodes advertise their pods' Type-2 MAC/IP routes to a fabric leaf under their respective `<originating-router-ID>:<assigned-number>` RDs. The L3VNI case checks a Type-5 prefix from each originating node under the configured RD on LeafA and LeafB. Both cases match the route identity, originating VTEP, and route targets within the RD entry. They do not test connectivity or RD updates.

Conversion, FRR golden, and admission tests cover L3VPN behavior, omitted-field fallback, update behavior, route targets, numeric bounds, and collisions.

The EVPN e2e assertions can reuse `frr.EVPNInfo` and the existing leaf executors. Add a focused matcher that checks a route inside the expected `RdEntry`: the current Type-2 and Type-5 helpers flatten entries and cannot prove which RD carried the route. Use `Eventually` around leaf table reads.

## 4. Inspect route evidence

Inspect the receiving fabric leaves' BGP tables while the test fixtures are active, or use the captured output on failure. The container names are defined in [e2etests/pkg/infra/routers.go](../../e2etests/pkg/infra/routers.go):

```sh
docker exec clab-kind-leafkind1 vtysh -c "show bgp l2vpn evpn json"
docker exec clab-kind-leafA vtysh -c "show bgp l2vpn evpn json"
docker exec clab-kind-leafB vtysh -c "show bgp l2vpn evpn json"
```

Expected: the receiving leaves have the Type-2 and Type-5 routes under their originating nodes' configured RDs, with the expected VTEPs and route targets.
