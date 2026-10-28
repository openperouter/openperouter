# Compatibility Contract

## Public configuration and status

This change introduces no new API. The contract is the current definitions in `api/v1alpha1/`, `api/static/config.go`, generated CRDs under `config/crd/bases/`, and existing chart/operator copies. All resource kinds, fields, defaults, required/optional values, validation and immutability remain identical. No trunk, VLAN tag, internal allocation or device-layout field is added.

Existing Kubernetes and static manifests remain usable without edits. Current conflict/dependency handling, node selection and routing policy semantics remain unchanged. `RouterNodeConfigurationStatus`, conditions, failed-resource kinds and reasons remain unchanged; provisioning failures continue through the existing failure path. Error messages may identify the failed internal operation without introducing new structured fields.

## Host networking

Existing host-side veth names, managed bridge names, Linux/OVS host-master attachment, and external bridge ownership remain unchanged. Omitting `hostMaster` still leaves an untagged dedicated attachment available to an external actor. No workload or host VLAN setup is required.

The router's internal VLAN remains private. Gateway CIDRs and deterministic MACs, route targets/distinguishers, VRF relationships, effective VXLAN ports, underlay families and usable packet sizes retain their existing behavior.

## Observations relied on by existing tests

| Observation | Required result |
| --- | --- |
| `ip neigh show <workload-IP> dev br-pe-<VNI>` | Actual gateway neighbor state remains observable on this device |
| Refresher bound to `br-pe-<VNI>` | Existing silent-workload refresh and movement behavior continues |
| `bridge fdb show dev vni<VNI>` | Existing remote forwarding entries remain observable |
| Workloads attached to current host interfaces | Same untagged communication and gateway behavior |
| Valid VNI greater than 4094 | Remains valid; internal VLAN has no effect on public range |
| Multiple L2VNIs sharing an L3VNI | Existing Layer 2 separation and symmetric routing remain |
| Resource deletion and restarts | Existing cleanup/recovery expectations and deadlines hold |

The internal type of `br-pe-*` changes from a bridge to a routed VLAN/macvlan endpoint; its neighbor ownership is preserved. `br-vl-*` and `svi*` are internal implementation names and are not public configuration.

## Test contract

Existing e2e manifests, scenarios, assertions, deadlines and mode filters establish compatibility and must not be weakened or rewritten. Internal privileged topology tests can be updated because they inspect implementation details. Add targeted regression proof for filtering/mapping repair, same-port L2/L3 coexistence, name limits, cleanup, VNI/VLAN distinction and repeated setup. Additional proof does not replace existing suite coverage.

Keep CI's current manifests/Helm/operator, systemd, hostmode-boot and Grout packaging lanes. Exact local commands and existing filters are in [quickstart.md](../quickstart.md).

## Platform contract

Use a kernel supporting the selected metadata VXLAN/VNI filtering operations (present upstream in 5.18), compatible iproute2 diagnostics, and the repository's FRR 10.6.0 image. Report unsupported kernel operations through existing failures; do not add an API flag or fallback dataplane. Deployment compatibility must be established with runtime tests on the actual target kernel; a version string alone is insufficient proof.
