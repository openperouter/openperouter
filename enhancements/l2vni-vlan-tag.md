# L2VNI VLAN Trunks

## Summary and Motivation

Today, each L2VNI has its own host-facing veth pair, bridge, and VXLAN device:

![Current per-VNI host veth model](drawings/current_multiveth.png)

This proposal adds an `L2Trunk` resource so VLAN-tagged L2VNIs can share one
host attachment, bridge, and VXLAN device:

![Proposed shared VLAN trunk model](drawings/vlan_novlanbridges.png)

In order to make the two implementations coexist, the current L2VNI
is reimplemented using the VLan aware model. This is because in order to make a VLan aware
bridge and a non VLan aware bridge, we would have to use different VXLan ports (a limitation
of linux).

All L2VNIs and L3VNIs use the
[FRR VLAN filtering bridge and single VXLAN device model](https://docs.frrouting.org/en/latest/evpn.html#vlan-filtering-bridge-and-single-vxlan-device).

## Reimplementing the Current API

Because of the limitation described above, the current L2VNI which doesn't expect a 
VLan tag must be reimplemented using a VLan aware bridge. This proposal describes how
to do that tagging the traffic internally to the OpenPERouter network namespace while
keeping the traffic to / from the host's namespace untagged.


## Proposed API

The existing standalone L2VNI API remains supported alongside the new trunk
API. A standalone L2VNI can set `spec.hostMaster` to attach its dedicated
host-facing veth to a Linux or OVS bridge, with untagged traffic on that
veth. For example, the current API allows:

```yaml
apiVersion: network.openperouter.io/v1alpha1
kind: L2VNI
metadata:
  name: standalone
spec:
  vni: 10300
  hostMaster:
    type: LinuxBridge
    linuxBridge:
      lifecycle: External
      name: br-standalone
```

With the new API, an L2VNI instead sets `spec.trunk` to select a shared
attachment and its VLAN tag:

```yaml
apiVersion: network.openperouter.io/v1alpha1
kind: L2VNI
metadata:
  name: tenant-a
spec:
  vni: 10100
  trunk:
    name: workloads
    vlan:
      tag: 100
```

The `hostMaster` configuration belongs to the
referenced `L2Trunk`. An L2VNI cannot set both `spec.hostMaster` and
`spec.trunk`; existing standalone manifests need no changes. A standalone
L2VNI can still omit `hostMaster` for external attachment of its veth.

`L2Trunk` owns the shared host attachment. The host-facing veth carries a
VLAN trunk that can extend the L2 domains through VLAN interfaces created
on it, Linux or OVS bridges, or other L2 connectivity mechanisms.
`hostMaster` behaves the same as for a regular L2VNI, with the difference
that `L2Trunk`s with `lifecycle: Managed` will be rejected because the
generated name is not predictable.

```yaml
apiVersion: network.openperouter.io/v1alpha1
kind: L2Trunk
metadata:
  name: workloads
spec:
  hostMaster:
    type: LinuxBridge
    linuxBridge:
      lifecycle: External
      name: br-workloads
```

Each L2VNI specifies its trunk and VLAN tag, retaining its own VNI, routing
domain, gateway, and route targets:

```yaml
apiVersion: network.openperouter.io/v1alpha1
kind: L2VNI
metadata:
  name: tenant-a
spec:
  vni: 10100
  trunk:
    name: workloads
    vlan:
      tag: 100
---
apiVersion: network.openperouter.io/v1alpha1
kind: L2VNI
metadata:
  name: tenant-b
spec:
  vni: 10200
  trunk:
    name: workloads
    vlan:
      tag: 200
```

`L2VNISpec.trunk` is optional. When set:

- `name` must resolve to an `L2Trunk`, and `vlan.tag` is required (1–4094).
  L2VNIs referencing a missing or deleted trunk are invalidated.
- The reference and tag are immutable; `hostMaster` is mutually exclusive.
- L2VNIs sharing a trunk must agree on effective `vxlanPort` and
  `underlayAddressFamily` settings.
- Tags must be unique per trunk per node; different trunks may reuse tags.
- Tags must be unique also for those trunks that share the same hostmaster.

VNIs must be unique per node across trunks, standalone L2VNIs, and L3VNIs.
The controller reports conflicts without programming ambiguous mappings.

## Dataplane

### Trunk-attached L2VNIs

Creating an `L2Trunk` creates its veth pair, VLAN-aware bridge, and
`external vnifilter` VXLAN device, even without referencing L2VNIs.
The host veth attaches to `hostMaster` when specified; otherwise it remains
available for external attachment.
The controller configures tagged membership on the router-side veth and
VXLAN port, VLAN-to-VNI mappings, and VNI filters. No default PVID is set:
untagged frames and unconfigured tags are dropped.

The operator of the host-side L2 connectivity must restrict each workload
to its assigned VLAN, for example through VLAN membership on bridge ports.
Trunk filtering alone cannot stop workloads from sending another configured
tenant's tag.

Deleting an L2VNI removes its VLAN membership, mapping, VNI filter entry,
and gateway configuration. Shared devices are deleted only when the
`L2Trunk` is deleted. As with deletion of a referenced L3VNI, deleting a
trunk removes the referencing L2VNIs' dataplane configuration and marks
them as orphaned of the trunk, retaining their Kubernetes resources.

An L2VNI referencing a missing or deleted trunk is reported through the
affected node's `RouterNodeConfigurationStatus`: `Ready=False` and
`Degraded=True`, with the existing condition reason `ConfigurationFailed`.
Its `status.failedResources` entry identifies the dependency as follows
(for `tenant-a` referencing trunk `workloads`):

```yaml
failedResources:
- kind: L2VNI
  name: tenant-a
  reason: DependencyFailed
  message: 'referenced L2Trunk "workloads" not found'
```

The message distinguishes a missing trunk from a missing L3VNI while
reusing the existing failure reasons. Other valid resources continue to
be configured.

FRR retains per-L2VNI route targets; Zebra discovers mappings and routing
domains from Linux. Each VNI with `gatewayIPs` gets a macvlan gateway in its
routing domain, following FRR's
[anycast gateway guidance](https://docs.frrouting.org/en/latest/evpn.html#anycast-gateways-with-single-vxlan-device).
This supports repeated gateway IPs across VNIs. A local FDB entry for each
anycast MAC keeps gateway traffic off the overlay.

Each trunk supports up to 4094 VLAN-to-VNI mappings. Additional trunks can
extend capacity while preserving per-node VNI uniqueness.

### Standalone L2VNIs and L3VNIs

This is what the current API provides.

In order to provide the same behavior
with a VLan aware bridge, an L2VNI without `trunk` keeps its dedicated bridge,
VXLAN device, and veth pair. The host veth carries untagged traffic and 
attaches to `hostMaster` when specified; otherwise it remains available for
external attachment. Internally, the router-side veth is an access port for
one VLAN mapped to its VNI on an `external vnifilter` VXLAN device.

![reimplementation of the current api with vlan aware bridge](drawings/vlan_novlan_vrf.png)

Each L3VNI likewise keeps a dedicated bridge and VXLAN device, with one
internal VLAN. L3VNI, VRF, gateway, and routing APIs remain unchanged.
Internal VLAN IDs can be reused across dedicated bridges, requiring no
node-wide allocation or status field.

This can be achieved with the following properties on the bridge:

```bash
bridge vlan add dev pe-100 vid 1 pvid untagged
```

where pe-100 is the veth leg on the OpenPERouter's namespace
side and 1 is a fixed VLan.

## Possible alternatives

### Shared host trunk selected by `hostMaster`

Keep `hostMaster` and `vlanTag` on each L2VNI. Matching external bridge
names identify one shared trunk; conflicting values are rejected. Its veth
remains until the last selected reference is removed. This avoids a new
resource but leaves attachment ownership shared between L2VNIs.

```yaml
apiVersion: network.openperouter.io/v1alpha1
kind: L2VNI
metadata:
  name: tenant-a
spec:
  vni: 10100
  vlanTag: 100
  hostMaster:
    type: LinuxBridge
    linuxBridge:
      lifecycle: External
      name: br-workloads
---
apiVersion: network.openperouter.io/v1alpha1
kind: L2VNI
metadata:
  name: tenant-b
spec:
  vni: 10200
  vlanTag: 200
  hostMaster:
    type: LinuxBridge
    linuxBridge:
      lifecycle: External
      name: br-workloads
```

### Dedicated `L2Trunk` with the reference under `vlan`

The PR's original `L2Trunk` alternative has the same ownership model as the
proposal, but places the trunk reference under `vlan`:

```yaml
apiVersion: network.openperouter.io/v1alpha1
kind: L2Trunk
metadata:
  name: workloads
spec:
  hostMaster:
    type: LinuxBridge
    linuxBridge:
      lifecycle: External
      name: br-workloads
```

```yaml
apiVersion: network.openperouter.io/v1alpha1
kind: L2VNI
metadata:
  name: tenant-a
spec:
  vni: 10100
  vlan:
    tag: 100
    trunk:
      name: workloads
---
apiVersion: network.openperouter.io/v1alpha1
kind: L2VNI
metadata:
  name: tenant-b
spec:
  vni: 10200
  vlan:
    tag: 200
    trunk:
      name: workloads
```

### `L2VNITrunk` with VLAN-to-VNI mappings

Put the attachment and mappings in one resource. Its controller creates and
owns an L2VNI per mapping; users update mappings through the trunk. The
mapping API must accommodate differing routing domains, route targets, and
gateway IPs. Adding a VNI requires editing the shared object.

```yaml
apiVersion: network.openperouter.io/v1alpha1
kind: L2VNITrunk
metadata:
  name: workloads
spec:
  hostMaster:
    type: LinuxBridge
    linuxBridge:
      lifecycle: External
      name: br-workloads
  mappings:
  - vni: 10100
    vlan:
      tag: 100
  - vni: 10200
    vlan:
      tag: 200
```

### Controller-assigned internal VLANs

Share a VLAN-aware bridge and VXLAN device across untagged L2VNIs too.
The controller allocates internal VLANs while host-facing access ports
remain untagged. Allocations must be persisted, stable, exclude configured
tags, and be reclaimed after deletion, within the bridge's 4094-VLAN limit.

Omitting `vlan` requests an internal allocation, recorded in status:

```yaml
apiVersion: network.openperouter.io/v1alpha1
kind: L2VNI
metadata:
  name: untagged-workload-network
spec:
  vni: 10300
```

Explicit `vlan.tag` reserves a user-selected tag instead. This reduces device
counts but requires allocation management.

### VLAN switching on the host bridge

Map each host VLAN to a dedicated access veth, retaining the traditional
router-side bridge and VXLAN device per VNI. The API can retain `vlanTag`
and `hostMaster` as in the shared-host-trunk example above.

The external bridge operator can configure VLAN membership and access ports,
or OpenPERouter can manage them on a managed bridge. OpenPERouter must avoid
modifying externally owned configuration and define cleanup of shared state.
This preserves existing L2/L3 wiring but still needs one veth pair per VNI.

### Hybrid variant using the current implementation

An internal switching bridge can connect a shared host trunk to the existing
per-VNI dataplane, using the trunk API above:

![Hybrid model](drawings/vlan_hybrid.png)

This retains per-VNI bridges and veths internally while sharing the host
attachment. Retaining traditional devices alongside VLAN-aware devices
would require a second dataplane configuration path.

## Implementation steps

1. Migrate existing L2VNIs and L3VNIs to VLAN-aware bridges and
   `external vnifilter` VXLAN devices, preserving their current APIs and
   host-facing behavior.
2. Add `L2Trunk` and `L2VNI.trunk` to support shared VLAN-tagged attachments
   using the same dataplane implementation.

## Potential followups

### L2VNI / L3VNI with single bridge

This is a potential improvement, if we find out that using a bridge per 
VRF / L2VNI results in worse performances.

The complexity of the implementation increases as: 

- it would require pooling the vlan tags and tracking which vlan tag is associated
to which VRF / L2VNI
- it would require dynamic bridge creation in case more than 4096 VRFs / L2VNIs
are required

### Allowing managed mode on hostmaster

If the need arises we can enable managed by either allowing the name field
or by reusing the resource name (or a variation of it, to be defined).
   
## Validation and Testing

- API validation for references, exclusions, immutability, shared VXLAN
  settings, and tag/VNI uniqueness.
- Conversion and Linux/FRR configuration unit tests for all three cases.
- Connectivity for two trunk mappings, standalone L2VNIs with and without
  `hostMaster`, and a tagged L2VNI using an L3VNI/VRF.
- Rejection of untagged and unconfigured tags, deletion without disrupting
  other mappings, and recovery after restart or partial updates.
- Trunk creation without L2VNIs, retention after the last mapping is removed,
  and trunk deletion invalidating referencing L2VNIs.
- Shared UDP-port operation, FRR VRF discovery, and local anycast forwarding.
- Performance comparison against per-VNI host attachments.

## Compatibility

The `external vnifilter` VXLAN model requires Linux kernel 5.18 or later,
including for existing L2VNIs and L3VNIs migrated to this model.

Existing manifests and host-facing behavior remain valid. Internal VLANs
require no user configuration. Static configuration supports the same
`L2Trunk` and `L2VNI.trunk` fields and semantics as the Kubernetes API.
