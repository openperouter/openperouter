# Research: Configurable RD Assigned Numbers

## Decision 1: Expose the assigned number on VNI resources

**Decision**: Add optional `spec.rdAssignedNumber` to L2VNI and L3VNI as an integer from 1 through 65535. Keep L3VPN's existing required field and range. Absence on a VNI means FRR continues to choose its automatic RD.

**Rationale**: This matches the operator's requested router-ID-plus-number model and the existing [L3VPN API](../../api/v1alpha1/l3vpn_types.go). A pointer or equivalent presence-aware representation distinguishes omission from an invalid zero. The VNI's numeric ID remains separate from the RD assigned number.

**Alternatives considered**: A complete user-supplied RD would repeat one administrator value across selected nodes. Making the new field required would change every existing VNI resource. Deriving a new default in OpenPERouter would replace FRR's existing automatic behavior.

## Decision 2: Render only configured VNI RDs

**Decision**: When a VNI assigned number is present, construct `<node-router-ID>:<assigned-number>` and render an `rd` command in that VNI's EVPN configuration. Put L2VNI's command under the underlay `l2vpn evpn` `vni` stanza and L3VNI's command under its VRF `l2vpn evpn` address family. Leave the command absent when the field is omitted. Retain an L2VNI in the generated FRR configuration when it has an RD setting but no explicit route targets.

**Rationale**: Existing templates omit RD commands for both VNI kinds: [L2VNI template](../../internal/frr/templates/underlay_evpn.tmpl), [L3VNI template](../../internal/frr/templates/vni.tmpl). [L2VNI conversion](../../internal/conversion/frr_conversion.go) currently omits entries when both route-target lists are empty; that would drop an RD-only setting. FRR's [EVPN configuration](https://docs.frrouting.org/en/stable-10.6/evpn.html) treats L2VNI and L3VNI as separate RD-bearing entities, and the [FRR project example](https://github.com/FRRouting/frr/discussions/19301) shows the relevant `rd` stanza forms.

**Alternatives considered**: Always emitting an RD would change fallback behavior. Injecting arbitrary raw FRR snippets would bypass the typed API and shared validation. Reusing `rd vpn export` would target L3VPN route export, not the EVPN VNI contexts.

## Decision 3: Keep route targets independent

**Decision**: Do not modify import or export route-target fields, default derivation, or generated route-target commands when `rdAssignedNumber` changes on an L2VNI or L3VNI. Keep L3VPN's existing default export route target based on its required assigned number.

**Rationale**: The user confirmed this behavior. FRR distinguishes the RD, which makes a route unique, from route targets, which control route import and export policy ([FRR BGP documentation](https://docs.frrouting.org/en/stable-10.6/bgp.html)). Existing conversion already handles VNI route targets independently from L3VPN's default route target.

**Alternatives considered**: Deriving a VNI default route target from the new number could silently change route import and connectivity.

## Decision 4: Validate configured values per node; preserve legacy reservations

**Decision**: Continue the existing VNI-number uniqueness and VNI-versus-L3VPN assigned-number checks. Add one per-node collision check over configured RD assigned numbers on selected L2VNIs and L3VNIs and the required numbers on selected L3VPNs. Report both resource identities on a conflict. Do not infer an omitted VNI's automatic RD from its VNI value.

**Rationale**: [Overlay validation](../../internal/conversion/validate_vni.go) already applies node selectors and runs the existing numeric checks; all three admission webhooks use it. The same rule also needs to run in reconciliation, which currently repeats the uniqueness filters in [router configuration reconciliation](../../internal/controller/routerconfiguration/reconcile.go). This preserves the user's requested legacy restriction, even when a VNI override differs from its VNI.

**Alternatives considered**: Replacing the existing numeric checks with RD-only comparison could allow previously rejected route-target overlaps. A cluster-wide number reservation would reject valid resources selected on disjoint nodes.

## Decision 5: Do not predict FRR automatic RD collisions

**Decision**: Keep FRR automatic RD behavior for VNIs without `rdAssignedNumber`. Pre-admission checks cover configured numbers only; an observed automatic-versus-configured collision is a runtime diagnostic case, outside the new validation guarantee.

**Rationale**: The user selected this compatibility-preserving rule. In pinned [FRR 10.6.0](https://raw.githubusercontent.com/FRRouting/frr/frr-10.6.0/bgpd/bgp_evpn.c), automatic L2VNI and L3VNI RD suffixes come from FRR-allocated IDs, not the VNI number. [FRR's ID allocator](https://raw.githubusercontent.com/FRRouting/frr/frr-10.6.0/bgpd/bgpd.c) shares that allocation space with other BGP instances, so admission cannot calculate an exact automatic RD from Kubernetes objects. The feature specification now states this limit explicitly.

**Alternatives considered**: Assigning deterministic RDs to all VNIs would make static collision checks possible but alter existing route identities. Querying current FRR state cannot guarantee future allocations after resource churn.

## Decision 6: Preserve generated manifests and validate at the public boundary

**Decision**: Regenerate deepcopy code, CRD schemas, all-in-one manifests, bundle artifacts, and API reference through `make generate-all-ci`. Verify admission/schema boundaries, conversion and FRR rendering, then run the relevant unit tests, lint, and an EVPN end-to-end scenario.

**Rationale**: These are the repository's [generation targets](../../Makefile) and user-facing [configuration guide](../../website/content/docs/configuration/evpn.md). Static configuration also consumes generated CRD schemas. Tests should cover field omission, 1 and 65535, invalid zero and 65536, same-node conflicts, disjoint selectors, RD-only L2VNI, unchanged route targets, and router-ID-specific RDs on two nodes.

**Alternatives considered**: Editing generated manifests by hand risks disagreement between typed APIs, admission, Helm, and static configuration.
