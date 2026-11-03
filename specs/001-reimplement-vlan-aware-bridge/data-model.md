# Data Model: Derived Linux VNI State

## Authoritative public entities

Existing `L2VNI`, `L3VNI`, `L3VPN`, underlay, host-session, and router status definitions remain unchanged. Static configuration embeds those same API specs. Existing VNI uniqueness, node selectors, routing-domain dependency rules, route targets, route distinguishers, gateway validation/immutability, port defaults, and underlay selection remain authoritative. No user-managed VLAN entity is added.

`VNIParams`, `L2VNIParams`, and `L3VNIParams` already carry the required desired-state inputs. VLAN 1 is an internal constant, not a new field in API, conversion, static YAML, or status. Derived kernel state is reconstructed after restart.

## Derived devices and associations

| Entity | Identity / attributes | Relationship |
| --- | --- | --- |
| Switching bridge | `br-vl-<VNI>`; VLAN filtering on, default PVID 0; no IPs or VRF master; root `fq_codel` and positive transmit queue length | One bridge per VNI; explicit VLAN 1 self membership; queue repaired without recreating matching devices |
| VXLAN device | `vni<VNI>`; external, vnifilter, current effective port/VTEP/underlay; nolearning | Master is that VNI's switching bridge |
| Tunnel mapping | VLAN 1 → configured VNI | Exactly one mapping on each VXLAN bridge port |
| Receive filter | Exactly the configured VNI | Distinct from tunnel mapping; required even though link VNI is 0 |
| Router-side access veth | Existing `pe-<VNI>` name; VLAN 1 PVID/untagged | L2VNI switching bridge port; host peer remains untagged |
| L2 anycast endpoint | `br-pe-<VNI>`, VLAN ID 1; current gateway CIDRs and deterministic per-VNI MAC | Direct child of switching bridge; existing VRF; one neighbor table shared by FRR, gateway and refresher |
| Addressless L2/L3 endpoint | `br-pe-<VNI>`, VLAN ID 1, no gateway addresses | Direct child of switching bridge; existing VRF when referenced, default domain otherwise |
| Gateway local FDB entry | Existing anycast MAC, VLAN 1, local disposition | Owned by the VNI on the switching bridge; prevents gateway traffic using overlay |

For L2 without gateways, use the direct addressless endpoint with the node-local router MAC. For disconnected L2, no VRF or gateway is introduced. For L3, use the direct addressless endpoint with IPv6 address generation disabled; existing host-session veths remain separate VRF attachments.

All names fit Linux's 15-character maximum at VNI 16777215: `br-vl-16777215` and `br-pe-16777215` are 14 characters. Do not append `.1` to these names. Existing `BridgeName(VNI)` continues returning `br-pe-*`, preserving Grout and refresher callers; new private helpers distinguish switching bridges from routed endpoints.

## Identity and invariants

- Configured VNI and VLAN ID are different namespaces. VLAN 1 reuse is safe across dedicated bridges and requires no node-wide allocator.
- The bridge, VXLAN and addressless SVI use a pinned node-local router MAC. The existing deterministic anycast MAC is used only for L2 gateways and remains equal across nodes for a VNI.
- Host-side veth names, managed `br-hs-<VNI>` names, external bridge references and ownership rules remain unchanged.
- Bridge self membership, VXLAN membership, mapping and receive filter are independently reconciled. Default PVID is not an implicit source of membership.
- Router-side access traffic enters/leaves untagged. The tunnel mapping consumes internal VLAN classification before overlay encapsulation, preserving workload payload and MTU.
- No unnecessary link-up, master, MAC or address writes occur when observed state matches desired state.
- Kernel errors propagate through existing resource failures and router readiness; no new public status category exists.

## Lifecycle

`Absent → SwitchingReady → TunnelReady → RoutedEndpointReady → HostAttachmentReady → Configured` describes provisioning order, not new persisted status values. Start neighbor refresh only after successful setup. Observe actual kernel state on every retry rather than trusting a cached stage.

A missing mutable membership, mapping, filter or local FDB entry moves the device back through reconciliation without full replacement. An immutable VXLAN mismatch requires removing that owned device and recreating it. Namespace loss results in complete reconstruction.

On deletion, stop the relevant refresher, remove owned VLAN endpoints (including obsolete split SVI/macvlan devices if present), remove VXLAN and private bridge, then apply existing host attachment and VRF ownership cleanup. Treat already-deleted child links as absent, while reporting other errors. Never delete an external host bridge or a shared still-referenced VRF.

Before initial conversion, inspect owned legacy topology. Remove all old owned fixed-ID VXLANs before creating metadata-mode replacements, then free obsolete `br-pe-*` bridge names. Leave unrelated interfaces untouched. No hitless transition or persisted migration version is promised.

Migration regression evidence requires gateway addresses and EVPN neighbor learning on the same VLAN SVI. A separate macvlan gateway leaves FRR monitoring a different neighbor table and can retain stale remote Type 2 host routes after movement. Reconciliation removes an obsolete `svi<VNI>` parent and its macvlan child before restoring the direct `br-pe-*` VLAN endpoint.
