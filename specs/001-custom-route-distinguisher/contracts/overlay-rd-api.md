# Contract: Overlay RD Assigned Numbers

## Public interface

The public interface is the `network.openperouter.io/v1alpha1` L2VNI, L3VNI, and L3VPN custom-resource API. L2VNI and L3VNI each gain an optional `spec.rdAssignedNumber`; L3VPN already has the required field. The same resource shape is used by Kubernetes admission and static configuration.

| Kind | Path | Required | Type | Valid range | Omitted behavior |
|------|------|----------|------|-------------|------------------|
| L2VNI | `spec.rdAssignedNumber` | No | integer | 1–65535 | Existing FRR automatic RD |
| L3VNI | `spec.rdAssignedNumber` | No | integer | 1–65535 | Existing FRR automatic RD |
| L3VPN | `spec.rdAssignedNumber` | Yes, existing | integer | 1–65535 | Rejected, as today |

For a configured value, a selected node advertises routes with RD `<that-node's-router-ID>:<rdAssignedNumber>`. The same resource can select multiple nodes; each node contributes its own router ID. The field never changes VNI identity, node selection, VRF, or explicit/default import and export route targets on a VNI.

## Minimal examples

These examples illustrate the field contract; each requires a compatible Underlay and the other normal deployment prerequisites.

```yaml
apiVersion: network.openperouter.io/v1alpha1
kind: L2VNI
metadata:
  name: tenant-l2
  namespace: openperouter-system
spec:
  vni: 5010
  rdAssignedNumber: 120
---
apiVersion: network.openperouter.io/v1alpha1
kind: L3VNI
metadata:
  name: tenant-l3
  namespace: openperouter-system
spec:
  vrf: tenant-l3
  vni: 5020
  rdAssignedNumber: 121
---
apiVersion: network.openperouter.io/v1alpha1
kind: L3VPN
metadata:
  name: tenant-srv6
  namespace: openperouter-system
spec:
  vrf: tenant-srv6
  rdAssignedNumber: 122
  importRTs:
    - "64514:122"
```

On a node with router ID `10.0.0.7`, the resulting configured RDs are `10.0.0.7:120`, `10.0.0.7:121`, and `10.0.0.7:122`. A second node with router ID `10.0.0.8` uses its own address with the same assigned numbers. These are RD examples; route targets follow their existing rules.

## Acceptance and rejection

- Omitted optional VNI fields preserve the existing resource and its automatically derived RD.
- A supplied VNI value must be an integer from 1 through 65535. A present zero, negative number, fraction, string, or value above 65535 is rejected.
- Two selected resources on the same node with the same configured RD assigned number are rejected. L3VPN's required number participates in this check.
- Existing VNI-number uniqueness and VNI-versus-L3VPN number restrictions still apply even when a VNI's assigned number differs from its VNI.
- Resources selected on disjoint nodes may reuse the same configured number. An omitted VNI's runtime-assigned FRR RD is outside the new pre-admission duplicate check.
- An invalid create or update identifies the affected resource and does not replace the last accepted routing configuration. The contract does not prescribe the exact error wording.

## Compatibility

The new fields have no default. Existing L2VNI and L3VNI manifests remain valid and keep their current RD and route-target behavior. L3VPN manifests and behavior remain unchanged. Updating or removing a configured VNI number changes subsequent advertised route identities according to the [data model](../data-model.md).
