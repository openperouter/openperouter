# Controller-Provisioned Underlay Interfaces

## Summary

Add a `CNIDevice` provisioning mode to the `interfaces` union introduced by
[PR #341](https://github.com/openperouter/openperouter/pull/341), so the
controller can invoke any CNI plugin via
[libcni](https://pkg.go.dev/github.com/containernetworking/cni/libcni)
to provision underlay interfaces in the router netns. Replaces the removed
Multus-based underlay integration.

## Motivation

### Goals

- **Replace the removed Multus underlay integration.** The router resiliency
  work removed the `--underlay-from-multus` controller flag and all associated
  code paths. With the persistent named netns model (see
  [router-resiliency.md](router-resiliency.md)), the router runs as a
  `hostNetwork` pod or a Podman quadlet — neither integrates with Multus CNI,
  which operates on pod network namespaces managed by the container runtime.
  Users who relied on Multus-based macvlan underlay for NIC sharing currently
  have **no alternative**.

- **Flexible NIC sharing and IPAM.** Operators need to choose how the
  underlay NIC is shared (macvlan, ipvlan, SR-IOV, bridge, OVS) and how IPs
  are assigned (static, DHCP, pool-based) based on their network environment.

- **Work in both Kubernetes and host/systemd modes.** Operators can
  provision CNI-based underlay interfaces regardless of the deployment
  mode. The same interface types and IPAM options are available in both
  environments.

- **Support day-0 operations.** New deployments should be able to install
  OpenPERouter and have underlay interfaces provisioned automatically on
  first startup, without manual pre-configuration of network devices on
  each node.

- **Consistent API pattern.** Extend the discriminated-union pattern already
  established by `UnderlayInterface` and `HostMaster` in the L2VNI CRD, so
  operators encounter a familiar structure across the API surface.

### Non-Goals

- **Implementing per-interface-type provisioning logic in the controller.**
  Macvlan, ipvlan, SR-IOV, OVS, and bridge interface creation is delegated
  entirely to CNI plugins. The controller invokes the plugin; the plugin
  handles netlink operations.

- **Redundant router instances.** NIC sharing via CNI plugins (e.g. macvlan)
  is a prerequisite for running multiple router instances per node, but the
  multi-instance design itself is a separate enhancement.

- **Plugin-specific field validation.** The controller validates structural
  plugin JSON config correctness and plugin binary existence, but does not
  validate plugin-specific fields (e.g. checking `master` device exists
  for macvlan).

## User Stories

#### Story 1: Day-0 Setup

As an operator deploying OpenPERouter for the first time, I want underlay
interfaces to be provisioned automatically on first startup, so that I
don't need to manually pre-configure network devices on each node.

#### Story 2: Single-File Configuration

As an operator, I want to define the entire underlay configuration —
including how the interface is created and how IPs are assigned — in a
single configuration file, so that I don't need to manage multiple
configuration artifacts per node.

#### Story 3: Migrating from Multus

As an operator who previously used Multus to plumb the underlay NIC into
the router pod, I want a replacement that restores NIC sharing without
Multus, so that I can upgrade to the named netns deployment model without
requiring a dedicated physical NIC.

#### Story 4: Shared NIC

As an operator on hardware with limited NICs, I want the host and the
router to share the same physical NIC, so that I don't need a dedicated
NIC for underlay traffic.

#### Story 5: MAC-Restricted Networks

As an operator on a network that limits the number of MAC addresses per
port, I want the router's underlay interface to share the physical NIC's
MAC address, so that only one MAC appears on the wire.

#### Story 6: Per-Node Static IP Assignment

As an operator whose network infrastructure requires specific IP
assignments per router, I want to assign a deterministic static IP to
each node's underlay interface from a single configuration.

#### Story 7: DHCP-Managed Networks

As an operator whose network uses DHCP for IP management, I want the
router's underlay interface to obtain its IP via DHCP from the existing
infrastructure, with a stable identity across restarts.

## Proposal

### Overview

The API improvements enhancement replaces `UnderlaySpec.Nics []string` with
`Interfaces []UnderlayInterface` — a discriminated-union slice whose `type`
field selects how each underlay link is obtained. It defines the first mode
(`NetworkDevice`); this enhancement adds `CNIDevice`.

After this enhancement, the two modes are:

| Mode | Behavior | Host NIC availability |
|------|----------|----------------------|
| `NetworkDevice` | Moves an existing host device into the router netns | Device is exclusively owned by the router |
| `CNIDevice` (this enhancement) | Invokes a CNI plugin to provision an interface in the router netns | Depends on the plugin (e.g. macvlan keeps parent on host) |

### Supported CNI Plugins

Any CNI plugin that operates solely on the network namespace — without
requiring extra host paths mounted into the router pod — works out of the
box. Plugins that depend on external sockets or host directories (e.g.
SR-IOV, OVS) require a modified deployment to mount those paths.

We should ensure we only allow a subset of CNIs - the ones listed in the
table below.

**Known to work without extra mounts:**

| Category | Plugins |
|----------|---------|
| Interface | `macvlan`, `ipvlan`, `vlan`, `host-device` |
| IPAM | `dhcp`, `static` |

### API

The CNI plugin configuration is embedded directly in the Underlay spec
via `RawConfig`. The config source is a discriminated union — additional
source variants (e.g. referencing external NADs or filesystem config files)
can be added later if a concrete user need emerges.

The `rawConfig` and `runtimeConfig` fields are **immutable** — once the
Underlay is created, the CNI configuration cannot be updated in place. To
change them, the operator
must delete and recreate the Underlay. This eliminates the need for
config-drift reconciliation (DEL the old interface, ADD with the new
config) and avoids a class of partial-failure states where the old
interface is torn down but the new one fails to provision. Immutability
is enforced by the validation webhook, correlating the CNI interfaces by
their interface name: the API server rejects CEL transition rules
(`oldSelf`) inside atomic lists, so it cannot be expressed in the CRD
schema.

#### Examples

##### CNI with macvlan

Replaces Multus-based underlay — the parent NIC stays on the host.

```yaml
interfaces:
  - type: CNIDevice
    cniDevice:
      type: RawConfig
      rawConfig:
        cniVersion: "1.0.0"
        name: macvlan-underlay
        plugins:
          - type: macvlan
            master: eth1
            mode: bridge
            ipam:
              type: static
              addresses:
                - address: "192.168.1.10/24"
```

##### CNI with ipvlan

Shared MAC — useful when MAC learning is constrained (e.g. cloud or campus
port-security):

```yaml
interfaces:
  - type: CNIDevice
    cniDevice:
      type: RawConfig
      rawConfig:
        cniVersion: "1.0.0"
        name: ipvlan-underlay
        plugins:
          - type: ipvlan
            master: eth1
            mode: l2
```

##### CNI with per-node static IP

Because each node needs a different IP, the operator creates **one Underlay
per node** with a `nodeSelector` targeting that node and the node-specific
IP in the `static` IPAM `addresses` list:

```yaml
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: Underlay
metadata:
  name: underlay-worker-0
spec:
  nodeSelector:
    matchLabels:
      kubernetes.io/hostname: worker-0
  asn: 64514
  interfaces:
    - type: CNIDevice
      cniDevice:
        type: RawConfig
        rawConfig:
          cniVersion: "1.0.0"
          name: macvlan-static
          plugins:
            - type: macvlan
              master: eth1
              mode: bridge
              ipam:
                type: static
                addresses:
                  - address: "192.168.1.10/24"
  tunnelEndpoint:
    cidrs:
      - "100.65.0.0/24"
  neighbors:
    - address: 192.168.1.1
      asn: 65000
```

Repeat for each node with a different `nodeSelector` and IP — only the
`addresses` entry differs.

##### CNI with custom interface name

```yaml
interfaces:
  - type: CNIDevice
    cniDevice:
      type: RawConfig
      rawConfig:
        cniVersion: "1.0.0"
        name: macvlan-underlay
        plugins:
          - type: macvlan
            master: eth1
            mode: bridge
            ipam:
              type: static
              addresses:
                - address: "192.168.1.10/24"
      interfaceName: underlay0
```

When `interfaceName` is omitted, it defaults to `net1`.

##### CNI with DHCP and pinned MAC

When the operator wants the router's underlay IP assigned by a DHCP server,
the config uses `dhcp` IPAM. Because macvlan generates a random MAC on
every CNI ADD invocation, the DHCP server would assign a different IP after
each netns rebuild — breaking BGP sessions and EVPN routes. To ensure IP
stability, the operator **must pin the MAC address** by passing a per-node
MAC via `runtimeConfig.mac`.

The macvlan plugin natively supports the `mac` capability
([source](https://github.com/containernetworking/plugins/blob/33cc6bd63968280b330b00468afbb66161aaa6bd/plugins/main/macvlan/macvlan.go#L48)):
it sets the MAC on the link **during interface creation**, before IPAM runs.
The DHCP DISCOVER is sent with the pinned MAC — no plugin chaining with
`tuning` is needed.

This requires one Underlay per node (same pattern as static IPs):

```yaml
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: Underlay
metadata:
  name: underlay-worker-0
spec:
  nodeSelector:
    matchLabels:
      kubernetes.io/hostname: worker-0
  asn: 64514
  interfaces:
    - type: CNIDevice
      cniDevice:
        type: RawConfig
        rawConfig:
          cniVersion: "1.0.0"
          name: macvlan-dhcp
          plugins:
            - type: macvlan
              master: eth1
              mode: bridge
              capabilities:
                mac: true
              ipam:
                type: dhcp
        runtimeConfig:
          mac: "02:42:c0:a8:01:0a"
  tunnelEndpoint:
    cidrs:
      - "100.65.0.0/24"
  neighbors:
    - address: 192.168.1.1
      asn: 65000
```

Repeat for each node with a different `nodeSelector` and MAC.

###### Enabling CNI runtime parameters

Some CNI use cases require runtime parameters that vary per node — for
example, pinning a specific MAC address so the DHCP server assigns a
stable IP. The Underlay spec supports this through two fields that work
together:

1. **`capabilities` in the plugin config** — declares which runtime
   parameters the plugin accepts. The CNI specification requires plugins
   to explicitly opt in to each capability. For example, to accept a MAC
   address, the macvlan plugin config must include
   `"capabilities": {"mac": true}`.

2. **`runtimeConfig` in the Underlay spec** — passes the actual values.
   The controller forwards this as `CapabilityArgs` to `libcni`, which
   only delivers keys that the plugin declared in step 1. Undeclared
   keys are silently stripped — the plugin never sees them.

In the DHCP+MAC pinning example above, both pieces are present: the
plugin config declares `"capabilities": {"mac": true}`, and
`runtimeConfig` passes `"mac": "02:42:c0:a8:01:0a"`. Without the
capability declaration, `libcni` would strip the `mac` key and macvlan
would generate a random MAC on each invocation.

Well-known capability keys include `ips`, `mac`, `bandwidth`,
`portMappings`, `ipRanges`, and `deviceID`. See the
[CNI conventions](https://www.cni.dev/docs/conventions/) for the full
list. The controller enforces no schema on `runtimeConfig`.

###### DHCP prerequisites

**DHCP daemon:** The CNI `dhcp` IPAM plugin requires a DHCP daemon
process running on each node (`dhcp daemon` or via systemd socket
activation at `/run/cni/dhcp.sock`). See
[CNI DHCP plugin docs](https://www.cni.dev/plugins/current/ipam/dhcp/)
for setup.

**Lease lifecycle across restarts:**
- **Router pod restart (netns preserved):** The CNI cache survives on
  the persistent hostPath. The controller skips re-invocation. The DHCP
  daemon continues renewing the existing lease.
- **Netns rebuild (CNI DEL + ADD):** The controller calls CNI DEL
  (releases the DHCP lease), then CNI ADD. With a pinned MAC, the
  server should assign the same IP (if using reservations or sticky
  leases).
- **DHCP daemon restart:** The daemon loses in-memory lease state. If
  the lease expires before recovery, the IP may be lost. Mitigate with
  systemd socket activation for automatic daemon restart.

### Risks and Mitigations

| Risk | Mitigation |
|------|------------|
| CNI plugin binaries not installed on host | Clear error at reconcile time with plugin name and search path. Host/systemd package bundles common plugins. |
| CNI cache lost but interface exists | Group ID 4242 detection still identifies the interface. IPAM lease may leak — document as a known edge case. |
| CNI ADD is not idempotent (calling twice fails) | Controller checks `GetNetworkListCachedResult()` before `AddNetworkList()`. If cache is lost, existing interface detected via group ID. |
| CNI DEL fails during teardown | DEL errors are logged but do not block teardown. CNI spec mandates plugins handle repeated DEL calls gracefully. |
| `runtimeConfig` keys silently stripped by `libcni` | `libcni` only forwards keys the plugin declares in its `"capabilities"` block. Document prominently; consider logging a warning when `runtimeConfig` is set but the config has no capabilities. |
| DHCP IPAM: macvlan random MAC causes IP instability | Document that DHCP with macvlan requires MAC pinning via `runtimeConfig.mac`. |
| DHCP IPAM: DHCP daemon not running | CNI ADD fails immediately with a socket connection error. Document the daemon requirement. |
| DHCP IPAM: lease expires during daemon downtime | Mitigate with systemd socket activation for fast daemon recovery and long lease times. |
| Operator forgets to set the sub-struct matching the type | CEL validation rejects the resource at admission time. |
| `containernetworking/cni` dependency version conflicts | Pin to `v1.2.x` in `go.mod`. This version targets CNI spec 1.0.0+ (required for CHECK support and capabilities filtering). Minimal transitive dependencies. |

## Design Details

### API Types

```go
// UnderlayInterface defines how the router obtains a single underlay link.
// Exactly one of the sub-structs must match the type field.
//
// +union
type UnderlayInterface struct {
	// +kubebuilder:validation:Enum=NetworkDevice;CNI
	// +required
	// +unionDiscriminator
	Type string `json:"type,omitempty"`

	// networkDevice moves an existing host network device into the router
	// netns. When IPAM is configured, the controller assigns deterministic
	// per-node IPs from CIDR pools.
	// +optional
	NetworkDevice *NetworkDeviceConfig `json:"networkDevice,omitempty"`

	// cniDevice invokes a CNI plugin to provision an interface in the router
	// netns. IPAM is handled by the CNI plugin.
	// +optional
	CNIDevice *CNIDeviceConfig `json:"cniDevice,omitempty"`
}

// NetworkDeviceConfig specifies which host network device to move into
// the router netns, and optional IPAM configuration.
type NetworkDeviceConfig struct {
	// +kubebuilder:validation:Pattern=`^[a-zA-Z][a-zA-Z0-9._-]*$`
	// +kubebuilder:validation:MaxLength=15
	// +required
	InterfaceName string `json:"interfaceName,omitempty"`

	// +optional
	IPAM *InterfaceIPAM `json:"ipam,omitempty"`
}

// CNIDeviceConfig specifies how to invoke a CNI plugin to provision
// an underlay interface. The config source is a discriminated union —
// additional source variants (e.g. NAD reference, filesystem path)
// can be added later if a concrete user need emerges.
//
// +union
// +kubebuilder:validation:XValidation:rule="self.type == 'RawConfig' ? has(self.rawConfig) : true",message="rawConfig is required when type is RawConfig"
// +kubebuilder:validation:XValidation:rule="self.type != 'RawConfig' ? !has(self.rawConfig) : true",message="rawConfig must not be set when type is not RawConfig"
type CNIDeviceConfig struct {
	// +kubebuilder:validation:Enum=RawConfig
	// +required
	// +unionDiscriminator
	Type string `json:"type,omitempty"`

	// rawConfig embeds a CNI config JSON blob directly in this spec.
	// Immutable once set — delete and recreate the Underlay to change
	// (enforced by the validation webhook: CEL transition rules cannot
	// be evaluated inside atomic lists).
	// +kubebuilder:pruning:PreserveUnknownFields
	// +kubebuilder:validation:Type=object
	// +optional
	RawConfig *apiextensionsv1.JSON `json:"rawConfig,omitempty"`

	// interfaceName is passed as CNI_IFNAME. Defaults to "net1".
	// +kubebuilder:validation:Pattern=`^[a-zA-Z][a-zA-Z0-9._-]*$`
	// +kubebuilder:validation:MaxLength=15
	// +kubebuilder:default=net1
	// +optional
	InterfaceName string `json:"interfaceName,omitempty"`

	// runtimeConfig is opaque JSON passed as CapabilityArgs to libcni.
	// libcni performs capabilities filtering: only keys that the plugin
	// declares in its "capabilities" config block are forwarded.
	// Undeclared keys are silently stripped. Well-known capabilities:
	// ips, mac, bandwidth, portMappings, ipRanges, deviceID.
	// +kubebuilder:pruning:PreserveUnknownFields
	// +kubebuilder:validation:Type=object
	// +optional
	RuntimeConfig *apiextensionsv1.JSON `json:"runtimeConfig,omitempty"`
}

// InterfaceIPAM configures IP address management for a NetworkDevice
// interface. CNI interfaces delegate IPAM to the CNI plugin.
//
// +union
type InterfaceIPAM struct {
	// +kubebuilder:validation:Enum=Native
	// +required
	// +unionDiscriminator
	Type string `json:"type,omitempty"`

	// +optional
	Native *NativeIPAM `json:"native,omitempty"`
}

// NativeIPAM derives per-node IP addresses from CIDR pools using the node
// index. Each node gets the (nodeIndex+1)th address from each pool (the +1
// offset skips the network address, matching the RouterID convention).
// Unlike tunnelEndpoint.cidrs (which assigns /32 or /128 host routes),
// Native IPAM preserves the original pool mask — e.g. 192.168.1.0/24
// assigns 192.168.1.<nodeIndex+1>/24, because the underlay is a shared L2
// subnet with the ToR switch.
type NativeIPAM struct {
	// At most one IPv4 and one IPv6 CIDR (dual-stack).
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=2
	CIDRs []string `json:"cidrs"`
}
```

### Controller Provisioning Flow

The controller's reconciliation pipeline is extended to handle the `CNIDevice`
type. The provisioning logic runs in the same phase where it currently
moves host devices.

#### CNI Mode

The controller invokes CNI ADD / CHECK / DEL via `libcni` as part of the
underlay reconciliation mechanism:

- **CNI ADD** — provisions the interface in the router netns. The
  controller resolves (and validates) the config source, parses the
  config, merges `runtimeConfig` capabilities, and calls
  `AddNetworkList`.
- **CNI CHECK** — validates that a previously-provisioned interface is
  still correctly configured. The controller uses the CNI cache together
  with CNI CHECK to determine whether the underlay interfaces need to
  be rebuilt.
- **CNI DEL** — tears down CNI-provisioned interfaces. Called during
  `HandleNonRecoverableError` / router netns rebuild. The CNI cache is
  also cleared on rebuild to ensure the new instance starts from
  scratch.

IPAM is fully delegated to the CNI plugin configured in the config. The
controller extracts assigned IPs from the CNI result; common IPAM plugins
include `static` and `dhcp`.

### Test Plan

- **Integration tests — CNI invocation**: In a real (non-mock) netns
  environment, with a mock CNI plugin binary that returns canned
  results, verify:
  - Config source resolution: RawConfig embedded bytes parsed into
    config.
  - CNI ADD with valid config returns assigned IPs.
  - CNI DEL with cached result calls `DelNetworkList` successfully.
  - `runtimeConfig` merging: `CapabilityArgs` populated correctly.
  - Capability filtering: `ips` and `mac` capabilities forwarded when
    declared; silently stripped when not declared.
  - Dual-stack: both IPv4 and IPv6 IPs extracted from mock result.
  - Idempotency: second call with existing cache returns cached result.
  - Error paths: malformed JSON, missing plugin binary, CNI ADD
    failure.
  - Delete when no cache exists → succeeds gracefully.
- **Reconciliation tests**: Verify:
  - Type change (`NetworkDevice` → `CNIDevice`) triggers rebuild.
  - Config source change triggers rebuild.
  - Cached CNI result → no re-invocation.
  - CNI ADD fails → error propagated to status.
  - CNI DEL fails during teardown → logged, teardown continues.
- **E2E tests — CNI provisioning**:
  - Macvlan with static IPAM (`static` with `addresses`):
    interface provisioned, IP assigned, end-to-end traffic flows
    through VXLAN tunnels and EVPN routes.
  - Macvlan with DHCP IPAM and pinned MAC: correct MAC, correct IP,
    end-to-end traffic flows through VXLAN tunnels and EVPN routes.
  - DHCP without MAC pinning (negative): random MAC after rebuild.
- **E2E tests — teardown**:
  - Underlay CR deletion: CNI DEL called, interface removed from router
    netns, IPAM resources released.
  - Netns rebuild (`HandleNonRecoverableError`): CNI DEL called, cache
    cleared, fresh CNI ADD on new netns provisions interface correctly.
- **E2E tests — self-healing**:
  - Interface missing but cache present: CNI CHECK detects the
    mismatch, controller re-provisions the interface via CNI ADD
    without operator intervention.
  - Cache lost but interface exists: controller detects interface via
    group ID, treats as existing (no duplicate provisioning).
  - Persistence across router pod restart: cache survives on hostPath,
    interface remains in named netns, no re-provisioning needed.

### Implementation

- `CNIDeviceConfig` type added to the API;
  `UnderlayInterface` enum extended with `CNIDevice`.
- CEL validation rules for `CNIDeviceConfig`
  (RawConfig required/forbidden, immutability).
- CNI invocation layer (`internal/cni/invoker.go`) wrapping `libcni`:
  config resolution from `RawConfig`, `runtimeConfig` merging, cache
  management, `AddNetworkList`/`DelNetworkList`.
- `SetupCNIUnderlay` provisioning function: resolve config → invoke
  CNI → set group ID 4242 → extract IPs.
- CNI DEL path in `HandleNonRecoverableError`.
- CNI CHECK path when reconciling the underlay, to ensure it is
  properly configured.
- Startup validation: embedded JSON parse, plugin binary existence.
- Integration tests: CNI ADD/DEL, runtimeConfig/capability filtering,
  idempotency (cache hit), dual-stack IP extraction, error paths.
- E2E tests: static IPs, DHCP+MAC pinning, persistence across
  restart.
- Package (RPM/deb/tarball) bundles statically-linked CNI plugin
  binaries: `macvlan`, `ipvlan`, `static`, `dhcp`, which are made
  available on the controller pod.
- Installation path: `/opt/openperouter/cni/bin/` (plugins).
- Controller's `CNI_PATH` includes `/opt/openperouter/cni/bin/` by
  default.
- `--cni-bin-dir` flag for custom plugin paths.
- Migration guide published in release notes.

## Drawbacks

- **Slightly more verbose YAML.** The nested sub-struct adds indentation
  compared to `nics: [eth1]`. This is the trade-off for type safety.
- **Operational dependency on CNI plugin binaries.** Most Kubernetes
  clusters already have these. Host/systemd mode bundles them in the
  package.
- **IPAM is fully delegated.** For CNI interfaces, the controller has no
  visibility into IPAM allocation failures beyond what the plugin
  reports.

## Alternatives

### Alternative 1: Flat Discriminated Union (Single Struct, No Sub-Structs)

```yaml
interfaces:
  - type: CNIDevice
    nadName: macvlan-underlay
    nadNamespace: default
```

**Why not chosen:** Unused fields leak into the YAML when using a
different type. Conditional validation is harder than structural
impossibility. The sub-struct pattern is already established by
`NetworkDevice`.

### Alternative 2: One-of Sub-Structs Without Type Enum

```yaml
interfaces:
  - cniDevice:
      type: RawConfig
      rawConfig: { ... }
```

**Why not chosen:** No explicit discriminator — code must check which
pointer is non-nil. A `Type` enum makes switch statements clean and
serialization unambiguous. Inconsistent with the existing `+union` /
`+unionDiscriminator` pattern used by `HostMaster` in L2VNI.

### Alternative 3: Direct Macvlan/Ipvlan Provisioning via Netlink (Discarded)

The original proposal added `Macvlan` and `Ipvlan` modes requiring
per-type netlink code in the controller.

**Why discarded:** Per-type netlink code for each interface type. IPAM
reimplementation. No config reuse from existing CNI configs. New
interface types (SR-IOV, OVS, bridge) would each need controller code.
The CNI approach handles all types through a single invocation path,
and the host-mode package bundles the common plugins.

## Future Work

### NetworkDevice Mode Provisioning

Investigate replacing the current netlink-based NetworkDevice
provisioning (move device, assign group ID, set UP, optional IPAM) with
a `host-device` CNI plugin invocation. This would unify both
provisioning paths under CNI. Must account for day-0 installs where the
`host-device` plugin binary may not yet be present on the node.

### Bonded Underlay (Link Aggregation)

Operators whose infrastructure requires link aggregation for redundancy
or bandwidth need a bond interface for the underlay. Moving a
pre-existing host bond into the router netns via `NetworkDevice` is not
possible — the bond's member interfaces remain in the host netns and the
bond breaks.

Integrate with
[k8snetworkplumbingwg/bond-cni](https://github.com/k8snetworkplumbingwg/bond-cni)
as an additional supported CNI interface plugin. With `bond-cni` invoked
via `CNIDevice` mode, the bond is created directly inside the router netns with
its member interfaces already in place, so the BGP session survives a
single link failure.

### NetworkDevice IPAM

Add IPAM support for NetworkDevice mode by re-using the same
deterministic CIDR-based allocation mechanism that VTEP IPs use today.
Pursue if a concrete user need emerges — operators can already achieve
per-node IPs via CNI with `static` IPAM and per-node `addresses`.

A use case for this would be using EVPN in cloud platforms, which
essentially are MAC-Restricted Networks; the user would have to use
ipvlan (or other alternatives that share the MAC of the lower device),
which do not play well with DHCP - some sort of cluster wide IPAM would
be required. Integrating with whereabouts would be an option, but given
it requires API access, day0 would not be possible.

## Implementation History

- 2026-04-21: Initial proposal drafted (Macvlan/Ipvlan via netlink).
- 2026-06-24: Revised to replace Macvlan/Ipvlan with CNI plugin
  invocation.
- 2026-06-25: Added CNI config source union (RawConfig); API and Path
  variants deferred until user need emerges.
- 2026-06-29: Flattened RawConfig — removed wrapper struct, rawConfig
  field is now directly `apiextensionsv1.JSON` (eliminates redundant
  `rawConfig.config` nesting level). Made rawConfig immutable via CEL
  transition rule.
- 2026-07-02: Moved rawConfig immutability enforcement to the validation
  webhook — the API server rejects CEL transition rules inside atomic
  lists.
