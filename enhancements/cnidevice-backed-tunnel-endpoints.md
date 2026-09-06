# CNIDevice-Backed Tunnel Endpoints

## Summary

Extend `TunnelEndpointConfig` with an optional `interfaceName` field. The
existing `cidrs` field remains the source of tunnel endpoint addresses and
OpenPERouter continues to derive a deterministic address for each node. When
`interfaceName` is set, OpenPERouter configures the derived addresses on the
referenced CNIDevice instead of the router loopback and uses that interface as
the VXLAN source device.

```yaml
tunnelEndpoint:
  interfaceName: net1
  cidrs:
    - 192.168.11.0/24
```

The initial use case is an IPvlan interface in L3 mode. IPvlan lets the router
share the host NIC's MAC address, which is required in cloud and virtualized
networks that reject additional source MAC addresses. In IPvlan L3 mode,
incoming traffic is delivered to a child interface by destination IP. A tunnel
endpoint address assigned only to the router loopback is not associated with
the IPvlan child and cannot receive return VXLAN traffic through it. Assigning
the endpoint address directly to the IPvlan interface resolves this limitation.

OpenPERouter, rather than an external pool allocator such as Whereabouts,
selects the endpoint address. The address is passed to the CNI chain through
the standard `ips` capability. The supported IPvlan configuration uses the
bundled static IPAM plugin to apply that controller-selected address during
CNI ADD; static IPAM does not select the address or maintain an allocation
pool.

When `interfaceName` is omitted, tunnel endpoint addresses continue to be
assigned to the loopback exactly as they are today.

### Scope

This enhancement targets CNIDevice-backed EVPN VXLAN tunnel endpoints only.
`tunnelEndpoint.interfaceName` may reference only a CNIDevice interface. Placing
a tunnel endpoint on a NetworkDevice and placing an SRv6 source on a CNIDevice
each require a separate enhancement. This enhancement adds no cloud-provider
integration and no provider-specific code. Using Whereabouts for a tunnel
endpoint is not packaged, validated, tested, or supported; the supported IPvlan
L3 chain uses the bundled static IPAM plugin. This enhancement applies only to
the kernel datapath; the grout datapath already rejects CNIDevice.

## Motivation

### Why the Tunnel Endpoint Lives on the Loopback

`TunnelEndpointConfig.CIDRs` defines one IPv4 pool, one IPv6 pool, or both.
OpenPERouter derives a host address from each pool and the node index, assigns
the resulting `/32` or `/128` to the router loopback, advertises it through the
underlay routing protocol, and uses it as the tunnel source.

The loopback is not incidental. It decouples the VTEP from any single uplink:

```text
                 lo: 100.65.0.1/32
                       |
      +----------------+----------------+
  toswitch1                         toswitch2
  BGP -> ToR A                      BGP -> ToR B
  network 100.65.0.1/32             network 100.65.0.1/32
```

FRR advertises the same host route over every underlay session. Remote VTEPs
learn it through whichever ToR is up, and if one uplink fails the address, the
VXLAN device bound to `lo`, and every established tunnel survive; only the
route converges. This is how an Underlay with multiple `interfaces` provides
redundancy today, and the default e2e fixture exercises exactly this shape with
two `NetworkDevice` uplinks.

The pattern is agnostic to how the uplink was created. A `CNIDevice` created
by `macvlan` is an ordinary L2 port; a loopback host route resolves through it
by ARP like any physical NIC. Two `macvlan` CNIDevices plus a loopback VTEP
therefore work identically to two `NetworkDevice` uplinks. The loopback remains
the correct default for every uplink type.

### The One Uplink Type That Cannot Reach the Loopback

The Linux IPvlan driver in L3 mode maintains an address table of addresses
assigned to IPvlan child devices. Its receive handler,
`ipvlan_handle_mode_l3()`, extracts the destination IP from a packet arriving on
the parent and looks it up in that table. If a child owns the address, the
packet is delivered into the child's namespace. Otherwise the handler returns
`RX_HANDLER_PASS` and the packet continues up the *parent* stack.

An address on `lo` inside the router namespace is never in that table
(`ipvlan_addr4_event()`/`ipvlan_addr6_event()` register only child addresses).
So a return VXLAN packet destined to a loopback VTEP is not delivered into the
router namespace. It stays in the host namespace, where no L3 path back into the
child exists: the IPvlan master and its slaves cannot exchange traffic, and an
L3-mode child is `IFF_NOARP`, so a host route toward it cannot resolve.

The failure is one-directional and therefore easy to miss. On transmit,
`ipvlan_xmit_mode_l3()` routes in the parent namespace with `FLOWI_FLAG_ANYSRC`,
so packets sourced from the loopback leave normally. Only the return path is
dropped.

Consequently, when the uplink is an IPvlan L3 child, the VXLAN endpoint must be
an address assigned to that child. No routing configuration in the host or the
cloud can substitute for this.

| Uplink | Loopback VTEP reachable | Multi-uplink redundancy |
|--------|-------------------------|-------------------------|
| `NetworkDevice` | yes | yes |
| `macvlan` CNIDevice | yes | yes |
| `ipvlan` L2 CNIDevice | yes | yes |
| `ipvlan` L3 CNIDevice | **no** | **no**: one child, one routed address |

### Cloud Network Constraints

Many cloud and virtual networking implementations do not provide a transparent
Ethernet segment to a virtual machine. They commonly enforce one or both of the
following policies:

- Only the MAC address registered for the virtual NIC may be used as a source.
- Only IP addresses registered or routed to that virtual NIC may be used as a
  source or destination.

Macvlan assigns a different MAC address to each child, so traffic can be
rejected by MAC anti-spoofing. IPvlan children share the parent device's MAC and
therefore avoid introducing another MAC on the external network. IPvlan L3 mode
also matches a routed cloud network better than an emulated L2 attachment. This
is why IPvlan L3 is the uplink type of interest in the cloud, and why its
loopback limitation matters.

The surrounding network must still authorize and route the endpoint IP. The
mechanism is provider-specific, for example:

- GCP alias IP ranges;
- AWS secondary private IP addresses on an ENI;
- Azure secondary IP configurations on a NIC;
- OpenStack allowed-address pairs; or
- an explicitly routed prefix in another virtualized environment.

GCP motivated and validated the original proof of concept, but this enhancement
is intentionally provider-neutral.

### Redundancy in the Cloud Comes From the Fabric

The on-premises reason for the loopback, two uplinks to two ToRs, has no cloud
equivalent. Cloud providers position multiple virtual NICs on a VM for reaching
separate virtual networks, for acting as a routing or NAT appliance between
them, or for bandwidth; not for high availability within one network. GCP is
explicit: each NIC must use a unique subnet, inbound traffic to same-VPC NICs
is delivered only to `nic0`, interface bonding is not supported, and HA is
achieved with multiple instances behind a load balancer rather than multiple
NICs. AWS and Azure secondary NICs follow the same model.

Redundancy for a cloud node's underlay therefore comes from the provider's
software-defined fabric beneath a single virtual NIC. A cloud router node has
exactly one underlay uplink by design, so it gives up nothing by terminating the
tunnel on that uplink instead of on a loopback.

### Why Not an Implicit Rule

Because the loopback still serves multi-uplink redundancy for `NetworkDevice`,
`macvlan`, and IPvlan L2, the tunnel endpoint cannot simply move to the
interface whenever the underlay uses a CNIDevice. Doing so would silently remove
redundancy from existing `macvlan` users and move their VTEP on upgrade. The
placement must be an explicit, per-Underlay opt-in, which is what
`tunnelEndpoint.interfaceName` provides.

OpenPERouter previously supported `vtepInterface`, which selected the first
IPv4 address already present on an arbitrary interface and was removed with
router Multus support. This proposal is not a restoration of that field: `cidrs`
remains the address authority, and the referenced interface is one OpenPERouter
provisions and owns.

### Why OpenPERouter IP Allocation

Tunnel endpoints are long-lived, node-scoped identities. OpenPERouter already
derives them deterministically from `tunnelEndpoint.cidrs` and the persistent
node index. Keeping that model has useful properties for cloud deployments:

- the expected endpoint can be calculated before CNI execution;
- controller and router restarts preserve the same endpoint;
- cloud infrastructure automation can authorize a predictable address;
- Kubernetes and systemd deployments use the same allocation model; and
- no distributed lease store or allocation garbage collector is required.

Whereabouts is a poor fit for this identity; see
[Alternatives Considered](#support-whereabouts).

## Goals

- Allow a tunnel endpoint address derived from `tunnelEndpoint.cidrs` to be
  placed on a named `CNIDevice` instead of `lo`.
- Use the referenced interface and derived address as the EVPN VXLAN source.
- Support IPvlan L3 underlays in cloud and virtualized networks that restrict
  source MAC addresses.
- Keep OpenPERouter's deterministic node-index-based tunnel endpoint
  allocation.
- Configure the derived addresses through the CNI lifecycle so CNI ADD, CHECK,
  DEL, and the libcni cache agree on the interface state.
- Preserve existing loopback-backed tunnel endpoint behavior when
  `interfaceName` is omitted.
- Work in Kubernetes and host/systemd modes without requiring a Kubernetes
  IPAM service.

## Non-Goals

- Supporting Whereabouts as a tunnel endpoint allocator.
- Managing cloud-provider alias IPs, secondary IPs, routes, firewall rules, or
  credentials.
- Discovering a tunnel endpoint by selecting an address already present on an
  interface.
- Allowing `tunnelEndpoint.interfaceName` to reference a NetworkDevice.
- Supporting an addressless standalone invocation of the upstream `ipvlan` CNI
  plugin.
- Supporting `tunnelEndpoint.interfaceName` with the grout datapath.
- Supporting SRv6 endpoint placement on IPvlan L3 (see [SRv6](#srv6)).

## User Stories

### Cloud Router Underlay

As an operator running OpenPERouter in a cloud that rejects additional source
MAC addresses, I want to provision an IPvlan L3 interface and use its address
as the VTEP so that BGP and VXLAN traffic use a cloud-compatible interface.

### Predictable Provider Registration

As an infrastructure operator, I want the VTEP address to be derived
deterministically from a configured pool and node index so that I can register
or route it in the cloud control plane without discovering a dynamic CNI
lease.

### Persistent CNI Lifecycle

As an operator, I want OpenPERouter to own creation, validation, and deletion
of the IPvlan interface so that router or controller restarts do not depend on
Multus reattaching a pod network.

### Existing Deployment Compatibility

As an existing user, I want a `tunnelEndpoint` containing only `cidrs` to keep
using the loopback so that this enhancement does not change my datapath.

## Proposal

### API

Add `InterfaceName` to `TunnelEndpointConfig`. The existing `CIDRs` field and
its validation markers are unchanged; only the new field is shown in full:

```go
// TunnelEndpointConfig contains tunnel endpoint configuration for the underlay.
type TunnelEndpointConfig struct {
    // cidrs is a list of pools from which OpenPERouter derives the local
    // tunnel endpoint addresses. The derived addresses are /32 or /128 host
    // addresses. Existing markers are retained: MinItems=1, MaxItems=2, the
    // valid-CIDR CEL rule, and at most one CIDR of each address family.
    // +required
    CIDRs []string `json:"cidrs,omitempty"`

    // interfaceName optionally names the CNIDevice interface (its effective
    // cniDevice.interfaceName) on which OpenPERouter places the derived tunnel
    // endpoint addresses. When omitted, the addresses are placed on the router
    // loopback.
    // +kubebuilder:validation:Pattern=`^[a-zA-Z][a-zA-Z0-9._-]*$`
    // +kubebuilder:validation:MinLength=1
    // +kubebuilder:validation:MaxLength=15
    // +optional
    InterfaceName *string `json:"interfaceName,omitempty"`
}
```

`interfaceName` and `cidrs` are intentionally used together. The field does not
select an already configured IP address. Instead:

- `cidrs` says which addresses OpenPERouter allocates; and
- `interfaceName` says where OpenPERouter places those addresses.

The resulting behavior is:

| Configuration | Address placement | VXLAN source device |
|---------------|-------------------|---------------------|
| `cidrs` only | Router loopback | Router loopback |
| `cidrs` and `interfaceName` | Referenced CNIDevice | Referenced CNIDevice |

`interfaceName` matches the effective `cniDevice.interfaceName` of one entry in
`spec.interfaces`. An omitted `cniDevice.interfaceName` has the existing default
of `net1`, so `tunnelEndpoint.interfaceName: net1` matches it.

Adding, removing, or changing `interfaceName` is rejected on update, enforced
by a CEL transition rule on `TunnelEndpointConfig`:

```text
has(self.interfaceName) == has(oldSelf.interfaceName) &&
(!has(self.interfaceName) || self.interfaceName == oldSelf.interfaceName)
```

Moving an endpoint between the loopback and a CNIDevice, or between CNIDevices,
requires deleting and recreating the Underlay. This aligns with the existing
immutability of the referenced CNI configuration, avoids leaving an addressless
former endpoint attachment behind, and gives static-file mode a deterministic
way to reject a placement change (see [Reconciliation](#reconciliation-and-lifecycle)).
Changes to `cidrs` retain their current API behavior and cause a controlled
reprovision of the referenced CNI attachment with the newly derived addresses.

### Complete IPvlan L3 Example

```yaml
apiVersion: network.openperouter.io/v1alpha1
kind: Underlay
metadata:
  name: cloud-underlay
  namespace: openperouter-system
spec:
  asn: 65001
  interfaces:
    - type: CNIDevice
      cniDevice:
        type: RawConfig
        interfaceName: net1
        rawConfig:
          cniVersion: "1.0.0"
          name: cloud-underlay
          plugins:
            - type: ipvlan
              master: br-ex
              mode: l3
              capabilities:
                ips: true
              ipam:
                type: static
                routes:
                  - dst: 10.250.1.0/24
  tunnelEndpoint:
    interfaceName: net1
    cidrs:
      - 192.168.11.0/24
  neighbors:
    - asn: 64515
      address: 10.250.1.3
      properties:
        - type: ebgpMultiHop
          ebgpMultiHop:
            ttl: 10
```

The example's endpoint pool and external route are illustrative. The endpoint
pool must be valid for the deployment's cloud or routed network. The route in
the CNI IPAM block describes how the router namespace reaches the remote BGP
network. Directly connected deployments do not necessarily need eBGP
multihop or the example route.

The raw CNI configuration does not contain a node-specific address. A single
Underlay can therefore select multiple nodes while OpenPERouter derives a
different endpoint for each node.

### Address Allocation

The existing `ipam.TunnelEndpointIP()` behavior remains authoritative.
OpenPERouter derives one host address for each configured family using the
node index and returns it with a `/32` or `/128` prefix. Both the FRR and host
network conversions must consume the same derived values.

Node index `0` maps to the first address of the pool, which for a pool such as
`192.168.11.0/24` is `192.168.11.0/32`. This is existing behavior and works for
a routed endpoint, but some networks treat the first address of a prefix as
reserved. For cloud deployments, choose an endpoint pool whose every address the
provider allows a node to own; a dedicated secondary range is typically the
right choice, as in GCP where all addresses of a secondary range are usable.

When no endpoint interface is configured, `SetupUnderlay()` continues to
assign those values to `lo`.

When an endpoint interface is configured, conversion associates the derived
values with the matching CNIDevice before CNI `ADD`. The addresses are not
also assigned to `lo`.

### Applying Addresses Through CNI

The supported and tested CNI chain is `ipvlan` in L3 mode with static IPAM.
Other chains are unsupported.

The upstream `ipvlan` plugin does not have a successful standalone link-only
mode. During ADD, it requires either:

- an IPAM result containing at least one address; or
- a chained `prevResult` containing at least one address.

Creating the link with CNI and assigning its address later with netlink would
therefore fail before OpenPERouter could perform the assignment. It would also
split interface ownership between CNI and OpenPERouter.

Instead, OpenPERouter adds its derived endpoint addresses to the effective CNI
capability arguments:

```json
{
  "ips": ["192.168.11.10/32"]
}
```

libcni forwards a capability argument only to a plugin whose config declares
that capability, so the `ipvlan` plugin entry that runs static IPAM must set
`capabilities: {"ips": true}`. libcni then passes the argument to the chain as
`runtimeConfig.ips`. Static IPAM returns the supplied addresses to `ipvlan`,
which assigns them to the child as part of ADD.

This division of responsibility is intentional:

- OpenPERouter selects and owns the endpoint address.
- Static IPAM transports the selected address through the CNI result.
- `ipvlan` creates the child and applies the CNI result.
- libcni records the effective config, capability arguments, and result.
- CNI CHECK validates the interface and address.
- CNI DEL removes the attachment.

The static plugin does not reserve from the pool and does not maintain a
second allocation database.

### Runtime Configuration Ownership

The endpoint-bearing CNIDevice is address-exclusive: OpenPERouter owns every
address on it. Users may continue to provide unrelated CNIDevice runtime
capabilities such as `mac`. The controller copies those arguments and adds the
derived `ips` value.

The referenced CNIDevice MUST NOT set user-supplied `runtimeConfig.ips` and
MUST NOT set `ipam.addresses`, because either would create a second authority
for the endpoint addresses. Both are rejected by validation.

The controller-derived `ips` value is not part of the user-supplied immutable
`cniDevice.runtimeConfig`. It is an effective runtime argument computed from
the mutable endpoint CIDRs and node index.

### Validation

When `tunnelEndpoint.interfaceName` is set, validation rejects the Underlay
unless all of the following hold:

- `tunnelEndpoint.cidrs` satisfies the existing constraints.
- The name matches exactly one effective `cniDevice.interfaceName` in
  `spec.interfaces`.
- The matching entry has type `CNIDevice`.
- `spec.interfaces` contains exactly one entry. Terminating the tunnel on an
  interface gives up the loopback's multi-uplink redundancy, which IPvlan L3
  cannot provide anyway and which cloud networks do not offer (see
  [Redundancy in the Cloud Comes From the
  Fabric](#redundancy-in-the-cloud-comes-from-the-fabric)). Rejecting additional
  interfaces turns an ambiguous runtime choice into an admission error.
- The referenced CNIDevice does not set user-supplied `runtimeConfig.ips`.
- The referenced CNIDevice does not set `ipam.addresses`.
- The referenced chain contains exactly one plugin entry with `type: ipvlan`,
  and that same entry has `mode: l3`, `ipam.type: static`, and
  `capabilities.ips: true`. libcni forwards capability arguments per plugin
  entry, and `ipvlan` hands its own received configuration to static IPAM, so a
  capability declared on a different entry does not deliver the address to this
  pair.
- The referenced chain does not set `disableCheck: true`, so CNI CHECK drift
  detection remains usable.
- `spec.srv6` is not configured.

Existing grout validation already rejects CNIDevice, so no additional datapath
check is required.

The API does not grow a plugin-specific union. The `ipvlan` entry check above
is a targeted rule on the single supported chain, applied to the opaque
`rawConfig` in Go; it is not a general model of CNI plugin configuration.
Reconciliation additionally verifies that the derived addresses are present
after ADD and returns a contextual error otherwise.

Validation that requires parsing an opaque `rawConfig` or resolving the
effective interface name is implemented in Go. Field syntax and the
`interfaceName` immutability transition rule use CRD schema validation. The
same semantic validation runs for static-file configurations, which bypass
admission webhooks.

### Host Configuration

`UnderlayTunnelEndpointParams` gains the selected interface name in addition
to the derived IPv4 and IPv6 CIDRs. Host conversion performs these steps:

1. Convert and validate every Underlay interface.
2. Derive endpoint addresses from `cidrs` and the node index.
3. Resolve `tunnelEndpoint.interfaceName` to a CNIDevice, if set.
4. Merge the derived address list into that CNIDevice's effective capability
   arguments.
5. Carry the endpoint interface name to Underlay and VNI host parameters.

The interface is provisioned before any VNI because `SetupUnderlay()` already
runs before `SetupL3VNI()` and `SetupL2VNI()`.

After CNI ADD, Underlay setup verifies that every derived endpoint address is
present on the referenced interface. Failed verification is treated as failed
provisioning: Underlay setup invokes CNI DEL with the desired arguments to
avoid leaving an invalid cached attachment that later reconciles would skip,
reports the expected interface and addresses, and does not create dependent
VNIs. The next reconcile retries a clean ADD.

### VXLAN Configuration

VNI host parameters gain the tunnel endpoint device name. VXLAN setup resolves
that device inside the router namespace and uses:

```text
VtepDevIndex = endpoint interface index
SrcAddr      = derived endpoint address for the VNI underlay family
```

For the default mode, the endpoint device remains `lo`, preserving current
behavior.

VXLAN drift detection compares the source address and source device index.
An existing VXLAN with a stale source device or address is deleted and
recreated.

This differs from the old `vtepInterface` implementation, which discovered the
first IPv4 address on an interface. Address discovery is unnecessary because
`cidrs` remains authoritative and supports both address families.

### FRR Configuration

FRR continues to consume the addresses derived from
`tunnelEndpoint.cidrs`. Address placement does not change the BGP-visible
endpoint identity.

The current behavior of advertising configured tunnel endpoint host routes in
the IPv4 and IPv6 unicast address families remains unchanged. The same address
is used as the EVPN next hop and VXLAN source for the selected underlay address
family.

BGP session establishment is unchanged. Endpoint placement binds only the VXLAN
source device and address; it does not add a BGP `update-source` or bind the
session to the CNIDevice. The session continues to use the router's existing
route lookup and source-address selection toward each neighbor, over the
CNIDevice when that is the egress interface.

An operator may need eBGP multihop when the peer is reached through a routed
cloud network rather than a directly connected segment. This is configured by
the existing `ebgpMultiHop` neighbor property and is not enabled implicitly by
endpoint interface placement.

### Reconciliation and Lifecycle

The current reconciliation order is retained: FRR is applied first, then the
datapath configurator runs `SetupUnderlay()` followed by VNI setup. FRR must be
applied before any VXLAN device exists because bgpd crashes when a VXLAN
appears while no EVPN instance is configured.

Because the FRR templates set `no bgp network import-check`, the tunnel
endpoint host route is advertised as soon as FRR is applied, briefly before the
address is configured by `SetupUnderlay()`. This transient already exists today
for the loopback endpoint and is unchanged by interface-backed placement. It
does not blackhole traffic: the address is configured within the same
reconcile, before any VNI is created.

For a stable desired configuration, endpoint provisioning is idempotent:

1. CNI CHECK validates the cached attachment.
2. The desired effective capability arguments are compared with the cached
   attachment.
3. CNI ADD is skipped when the config and arguments match.
4. Endpoint addresses and the VXLAN source device are verified.

Changing `tunnelEndpoint.cidrs` or a node index changes the controller-derived
`ips` capability. The libcni cache stores only the merged arguments, so the
invoker permits an in-place attachment replacement only when the raw config,
the interface name, and every capability argument except the controller-owned
`ips` are semantically unchanged. Any other difference is treated as a
forbidden user mutation and rejected, matching the existing immutability of
`rawConfig` and `runtimeConfig`. Adding or removing the `ips` argument entirely
corresponds to a placement change and is blocked by the `interfaceName`
transition rule.

When only the derived `ips` changes, `SetupUnderlay()` performs a controlled
replacement:

1. Remove dependent VNIs so no VXLAN references the endpoint device.
2. Invoke CNI DEL using the cached old configuration and capability arguments.
3. Invoke CNI ADD with the newly derived endpoint addresses.
4. Verify the endpoint interface and addresses.
5. Recreate the VNIs with the new source address and device.

If ADD or verification fails, the failed-ADD cleanup runs and reconciliation
returns an error. The node is then in a known degraded state: FRR was already
reconciled to the new endpoint at the start of the reconcile, so the new host
route is advertised while no endpoint address is configured, and the old
address is gone. The node's overlay is down until a later reconcile succeeds.
This is the same outage window as any other failed CNI ADD for an underlay
interface today, and it is bounded by the reconcile retry.

The old attachment cannot be kept alive until the new one is proven: the
`ipvlan` plugin owns a single interface name per CNI attachment, so the new
address can only be applied by replacing the attachment, and FRR is applied
before `SetupUnderlay()` by design (see above). Restoring the old attachment on
failure would require re-running ADD with the previous arguments and then
re-applying the previous FRR configuration, reintroducing a two-step rollback
that can itself fail halfway. The proposal therefore does not add a rollback
path; it relies on the retry-to-desired-state model already used for every
other underlay change. Cloud operators who need to rotate the endpoint pool
without a per-node outage should drain the node first.

Deleting the Underlay invokes CNI DEL; the endpoint addresses disappear with
the CNI interface. No separate loopback cleanup is required for
interface-backed endpoints.

### Cloud Control-Plane Responsibilities

OpenPERouter configures only the guest networking and routing stack. It does
not call cloud APIs.

Before traffic can use the endpoint, the surrounding infrastructure must:

- authorize the derived source address for the node's virtual NIC;
- route the address or its containing pool to that node;
- permit BGP, VXLAN, and any required control-plane traffic; and
- provide reachability to configured BGP neighbors.

For GCP, a typical deployment places the endpoint pool in a subnet secondary
range and assigns each node's derived endpoint as a `/32` alias IP. Other
providers use their equivalent secondary-IP or allowed-address mechanism.

The deterministic relationship between node index and endpoint address makes
this configuration suitable for infrastructure-as-code or a separate
cloud-integration controller. Automating provider registration is outside this
enhancement and should not block core OpenPERouter reconciliation.

## SRv6

`TunnelEndpointConfig` is shared by EVPN and SRv6, but interface-backed
placement is limited to EVPN. Validation rejects `tunnelEndpoint.interfaceName`
when `spec.srv6` is present.

The limitation is deliberate. Assigning the IPv6 tunnel source to an IPvlan
child would make that exact address visible to IPvlan's L3 receive lookup, but
SRv6 data packets are addressed to segment IDs allocated from the locator.
Those destination addresses are not necessarily assigned to the child. A
separate design must establish how the parent namespace routes the complete
locator into the router namespace and how that interacts with IPvlan's receive
demultiplexing, local SID routes, ISIS, and restart behavior.

Existing loopback-backed SRv6 behavior is unchanged.

## Backward Compatibility

This is an additive API change.

- Existing resources omit `tunnelEndpoint.interfaceName` and keep using `lo`.
- Existing node-index allocation, endpoint addresses, FRR advertisements, and
  VXLAN behavior remain unchanged.
- Existing CNIDevice resources that are not endpoint targets receive no
  controller-generated capability arguments.
- Existing user-supplied CNIDevice runtime capabilities remain unchanged,
  except that `ips` is reserved when the device is selected as the endpoint.
- Grout behavior remains unchanged because it already rejects CNIDevice.

The removed `vtepInterface` field is not restored. Its old semantics are not
accepted as an alias, and no migration is needed because the field is absent
from the current API.

## Security Considerations

This enhancement does not bypass cloud anti-spoofing. It avoids an additional
MAC address by using ipvlan, but the endpoint IP must still be authorized by
the provider.

The CNI configuration remains privileged input. Existing controls around
Underlay creation and CNI binary directories apply. OpenPERouter must not
allow user-provided `runtimeConfig.ips` to override its allocated endpoint and
must log the resolved interface and address without logging unrelated sensitive
runtime capability values.

The feature does not add cloud credentials or provider SDKs to OpenPERouter.

## Failure Modes

| Failure | Expected behavior |
|---------|-------------------|
| Referenced interface does not exist in the Underlay | Reject configuration |
| Referenced interface is not a CNIDevice | Reject configuration |
| CNI chain does not declare `ips` | Reject configuration |
| CNI chain fails to apply the derived address | Fail reconciliation before VNI setup |
| Provider has not authorized the endpoint IP | CNI succeeds; BGP/VXLAN remains unreachable |
| Cached interface is missing or misconfigured | CNI `CHECK` fails; DEL and ADD repair the attachment |
| Derived endpoint changes | Tear down dependent VNIs, replace the CNI attachment, then recreate VNIs |
| CNI ADD fails after replacement | Clean partial state and retry on the next reconcile |
| Controller restarts | Reuse the persistent libcni cache and validate with CNI `CHECK` |
| Router restarts | Reuse the persistent named network namespace and endpoint interface |

## Test Plan

### API and Validation Tests

- Accept `cidrs` without `interfaceName` and preserve loopback behavior.
- Accept `interfaceName` referencing an explicit CNIDevice interface name.
- Resolve an omitted CNIDevice interface name to `net1`.
- Reject an unknown interface name.
- Reject a reference to a NetworkDevice.
- Reject `interfaceName` when `spec.interfaces` has more than one entry.
- Reject user-provided `runtimeConfig.ips` on the selected CNIDevice.
- Reject `ipam.addresses` on the selected CNIDevice.
- Reject a CNI chain that does not declare the `ips` capability.
- Reject a CNI chain that sets `disableCheck: true`.
- Reject `interfaceName` when SRv6 is configured.
- Enforce `interfaceName` immutability.
- Apply the same checks to static-file configuration.

### Conversion Tests

- Derive the same IPv4 and IPv6 host addresses used by the existing loopback
  path.
- Add both derived addresses to the selected CNIDevice capability arguments.
- Preserve unrelated user capability arguments.
- Do not modify capability arguments for unselected CNIDevices.
- Carry the endpoint device to every EVPN VNI parameter.
- Keep `lo` as the device when `interfaceName` is absent.

### CNI Invoker Tests

- Forward generated `ips` through libcni as `runtimeConfig.ips`.
- Store the effective capability arguments in the persistent cache.
- Treat matching generated arguments as idempotent.
- Detect changed generated arguments.
- Replace a cached attachment only through the explicit controller-owned
  endpoint update path.
- Use cached old arguments for `CHECK` and `DEL`.
- Clean up after failed replacement `ADD`.

### Host Network Tests

- Provision the CNIDevice before checking or using the endpoint address.
- Verify endpoint addresses exist on the selected device and not on `lo`.
- Verify an unrelated address or capability is not removed.
- Remove dependent VNIs before replacing the endpoint attachment.
- Recreate VXLAN devices when the endpoint device index or source address
  changes.
- Verify CNI teardown removes the device and endpoint addresses.

### FRR Tests

- Preserve endpoint host-route advertisements for interface-backed endpoints.
- Use the derived address as the EVPN next hop.
- Preserve IPv4, IPv6, and dual-stack conversion behavior.
- Render existing eBGP multihop configuration unchanged.

### End-to-End Tests

Add an IPvlan L3 flavor to the existing EVPN routes-over-underlay matrix. The
test topology must model routed cloud reachability instead of relying on direct
ARP to the IPvlan child:

1. Create an IPvlan L3 CNIDevice over a parent that remains in the node
   namespace.
2. Derive the endpoint from `tunnelEndpoint.cidrs` and inject it through the
   CNI `ips` capability.
3. Install a host route for each node's derived endpoint address toward that
   node in the simulated external network.
4. Add the router-side route required to reach the external BGP peer.
5. Configure eBGP multihop where the simulated route includes an intermediate
   hop.

The test verifies:

- the interface reports IPvlan L3 mode;
- the child and parent share a MAC address;
- the parent remains in the host namespace;
- the derived `/32` is present on the child and absent from `lo`;
- the BGP session establishes over routed reachability;
- EVPN Type-3 and Type-5 routes use the derived endpoint;
- the VXLAN device uses the IPvlan interface and derived source address;
- overlay traffic succeeds;
- controller and router restarts retain or recover the attachment;
- external drift is detected by CNI CHECK; and
- deleting the Underlay removes the interface and libcni cache entry.

This entry is kernel-datapath-only and does not carry grout support labels.

## Documentation Plan

Implementation of this enhancement updates:

- the CNI-provisioned interface section in
  `website/content/docs/configuration/_index.md`;
- the tunnel endpoint section in
  `website/content/docs/configuration/evpn.md`;
- generated API documentation;
- a new `config/samples/underlay-cni-ipvlan-l3-static.yaml` sample; and
- the runnable `examples/evpn/cni-underlay` example.

Documentation leads with provider-neutral cloud and virtual-network language,
uses GCP as a proven example, and states that provider-side IP authorization
and routing remain external responsibilities.

## Alternatives Considered

### Support Whereabouts

Whereabouts could select an address dynamically and pass it to ipvlan, as the
original GCP proof of concept did through Multus. It is not selected for tunnel
endpoints because it would add:

- another binary and version to package or require from the host;
- CRDs, RBAC, and a Kubernetes or etcd allocation backend;
- lease reconciliation and stranded-allocation handling;
- a different operational model for systemd deployments; and
- dynamic identities that cloud automation must discover after allocation.

Whereabouts also would not solve VXLAN binding: OpenPERouter would still need
to use the allocated IPvlan interface as the endpoint device.

### Skip the Loopback Implicitly for Every CNIDevice

Instead of a new field, OpenPERouter could place the `cidrs` address on the
interface whenever the underlay uses a CNIDevice, on the grounds that it owns
the uplink and needs no indirection. This is rejected because the loopback's
job is redundancy, not indirection: two `macvlan` CNIDevices plus a loopback
VTEP survive an uplink failure exactly as two `NetworkDevice` uplinks do. An
implicit rule would strip that from existing `macvlan` users, move their VTEP
from `lo` to the child on upgrade, and leave no unambiguous target when more
than one CNIDevice is present. The explicit field costs one optional string and
makes the redundancy trade-off a deliberate choice.

### Route the Loopback VTEP Through IPvlan L3

Keeping the loopback and reaching it by plain L3 routing is the natural first
idea, since everything in a cloud VPC is routed. It fails at the kernel, not in
the cloud. A return packet to the loopback address arrives on the IPvlan
parent, misses the child address table in `ipvlan_handle_mode_l3()`, is passed
up the host stack with `RX_HANDLER_PASS`, and cannot then be routed into the
router namespace: the IPvlan master cannot talk to its slaves and an L3-mode
slave is `IFF_NOARP`. Transmit from the loopback works because the driver
routes with `FLOWI_FLAG_ANYSRC`, which is why the problem only shows up as
silent return-path loss. Registering the loopback `/32` as a second cloud alias
IP would not help; the packet is dropped before any host routing decision.

### Restore the Old `vtepInterface`

The removed field selected an arbitrary existing interface and discovered its
first IPv4 address, and was mutually exclusive with the endpoint pool. The
proposed model keeps `cidrs` as the address authority, references an
OpenPERouter-managed CNIDevice, and supports both address families.

### Assign the Address With Netlink After CNI ADD

The upstream `ipvlan` plugin requires an IP result and fails a standalone ADD
without IPAM or a populated previous result. Assigning an address afterward
would require a custom link-only plugin or changes upstream. It would also
split validation and teardown between CNI and OpenPERouter.

Passing the selected address through static IPAM uses the standard plugin
contract and keeps CNI CHECK and DEL authoritative.

### Use IPvlan L2

IPvlan L2 shares the parent MAC and may work in some MAC-restricted networks,
but it depends on L2 neighbor discovery and broadcast behavior. Cloud virtual
networks commonly provide routed semantics instead. Testing only L2 mode would
not validate the cloud topology demonstrated by the existing proof of concept.

### Ship a Link-Only IPvlan Plugin

A custom plugin could create an unnumbered IPvlan interface and let
OpenPERouter assign addresses later. Maintaining another privileged networking
plugin is not justified when the standard `ips` capability and bundled static
IPAM plugin provide the required behavior.

## Implementation Outline

1. Add `TunnelEndpointConfig.InterfaceName`, schema validation, the
   immutability transition rule, and generated API artifacts.
2. Resolve the CNIDevice reference and derive effective `ips` capability
   arguments during host conversion.
3. Add the controller-owned CNI replacement path for endpoint address changes.
4. Generalize Underlay address placement and VXLAN source-device selection.
5. Add API, conversion, CNI, hostnetwork, FRR, and reconciliation unit tests.
6. Add the routed IPvlan L3 end-to-end flavor.
7. Add user documentation and samples.

## Prior Art

The [GCP proof-of-concept branch](https://github.com/qinqon/openperouter/tree/gcp)
used a Multus-provisioned IPvlan L3 interface, Whereabouts, GCP alias IPs, eBGP
multihop, and the former `vtepInterface` field. It demonstrated that using the
IPvlan address as both the routed underlay identity and VXLAN source works in a
cloud network.

[PR #214](https://github.com/openperouter/openperouter/pull/214) introduced
the old interface-backed VTEP behavior. It was removed by
[PR #461](https://github.com/openperouter/openperouter/pull/461) after router
Multus support disappeared. CNIDevice changes the premise by giving
OpenPERouter a persistent, controller-owned interface lifecycle.

## References

- [Linux IPVLAN Driver HOWTO](https://www.kernel.org/doc/html/latest/networking/ipvlan.html)
- [CNI ipvlan plugin](https://www.cni.dev/plugins/current/main/ipvlan/)
- [CNI static IPAM plugin](https://www.cni.dev/plugins/current/ipam/static/)
- [Whereabouts](https://github.com/k8snetworkplumbingwg/whereabouts)
- [GCP alias IP ranges](https://cloud.google.com/vpc/docs/alias-ip)
- [Controller-Provisioned Underlay Interfaces](controller-provisioned-underlay-interfaces.md)
- [Router Resiliency](router-resiliency.md)
