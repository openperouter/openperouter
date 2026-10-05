# Data Model: Configurable RD Assigned Numbers

## L2VNI

Existing Kubernetes custom resource. The new optional `spec.rdAssignedNumber` is an integer from 1 through 65535. Omission retains FRR's automatically assigned RD. When present, the node's effective RD is `<router-ID>:<rdAssignedNumber>` for EVPN routes originated by this L2VNI.

The existing `spec.vni`, route targets, node selector, and optional routing-domain reference keep their meanings. An L2VNI with only `rdAssignedNumber` and no explicit route targets is still a valid configured overlay; the RD setting must reach FRR even though both route-target lists are empty.

## L3VNI

Existing Kubernetes custom resource. The new optional `spec.rdAssignedNumber` has the same range and omission semantics as L2VNI. When present, the node's effective RD is `<router-ID>:<rdAssignedNumber>` for EVPN routes originated by the associated routing domain, including its advertised IP prefixes.

The VNI, VRF, route targets, node selector, and host session remain independent of the new field. A dual-family host session may yield multiple rendered BGP configurations for one L3VNI; each uses the same RD on a given node.

## L3VPN

Existing custom resource with required `spec.rdAssignedNumber` from 1 through 65535. Its effective RD already uses `<router-ID>:<rdAssignedNumber>`. Its default export route target still uses the assigned number. This feature does not add or change an L3VPN field.

## Node and effective RD

The selected node supplies its router ID. A resource's assigned number is identical across its selected nodes, while each node's router ID distinguishes its originated routes. Effective RDs are derived values in FRR configuration and route advertisements, not fields stored on the resource.

| Resource | Assigned number present | RD on selected node | Route-target source |
|----------|-------------------------|---------------------|---------------------|
| L2VNI | No | Existing FRR automatic RD | Existing explicit or automatic behavior |
| L2VNI | Yes | `router-ID:rdAssignedNumber` | Existing explicit or automatic behavior |
| L3VNI | No | Existing FRR automatic RD | Existing explicit or automatic behavior |
| L3VNI | Yes | `router-ID:rdAssignedNumber` | Existing explicit or automatic behavior |
| L3VPN | Required | `router-ID:rdAssignedNumber` | Existing explicit values or current assigned-number default |

## Validation and uniqueness

- The new VNI fields are optional, but a supplied value must be an integer in 1–65535. Zero, negative numbers, fractions, strings, and values above 65535 are invalid.
- On each node, compare configured RD assigned numbers from selected L2VNIs and L3VNIs with each other and with selected L3VPNs. Reject duplicates with both resource identities. Different nodes may use the same assigned number because their router IDs differ.
- Keep existing VNI uniqueness and VNI-versus-L3VPN assigned-number restrictions. A VNI's configured RD number does not release its VNI number from that reservation.
- Do not treat an omitted VNI field as equal to its VNI number. FRR chooses an automatic RD suffix from runtime state, so automatic-versus-configured collisions cannot be predicted at admission.

## State transitions

| Transition | Expected result |
|------------|-----------------|
| Omitted → configured | Generated EVPN configuration gains a manual RD; subsequent local advertisements carry it. |
| Configured → different valid number | Generated RD changes and subsequent local advertisements use the new value. |
| Configured → omitted | Manual RD is removed; FRR resumes automatic RD behavior. |
| Valid → invalid or colliding | Admission rejects the update and retains the last accepted configuration. |
| Node selection changes | Per-node uniqueness is re-evaluated for the new set of resources sharing a node. |

The configured-to-configured and configured-to-omitted transitions may withdraw and re-advertise routes as their RD changes; this follows [FRR's EVPN RD update path](https://raw.githubusercontent.com/FRRouting/frr/frr-10.6.0/bgpd/bgp_evpn.c).
