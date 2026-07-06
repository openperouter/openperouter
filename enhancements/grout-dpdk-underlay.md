# DPDK-Accelerated Underlay Ports for Grout

## Summary

Replace the TAP-with-`remote=` underlay port mechanism in grout with
direct DPDK port attachment using SR-IOV Virtual Functions. After the
[grout-dataplane](grout-dataplane.md) enhancement (M1),
`configureUnderlayPort()` creates a `net_tap` PMD with `remote=<nic>`,
which installs TC ingress rules to redirect packets between the underlay
NIC and grout — adding kernel overhead on the underlay fast path. By
binding a VF directly to a DPDK poll-mode driver (e.g. `vfio-pci`), grout
can send and receive underlay traffic entirely in user-space, eliminating
the kernel data path for VXLAN encap/decap and BGP-learned forwarding.

This enhancement extends the `UnderlayInterface` discriminated union
(introduced by the [controller-provisioned-underlay-interfaces](controller-provisioned-underlay-interfaces.md)
enhancement) with a new `GroutPort` mode.

## Motivation

### Goals

- **Remove the kernel from the underlay fast path.** The current
  TAP+`remote=` approach routes every underlay packet through TC ingress
  rules in the kernel. Direct VF attachment eliminates this overhead.
- **Expose VF selection in the API.** Users need a way to tell the
  controller which VF to bind — by PCI address, or PF
  name + VF index — without external CNI tooling.
- **Support IPAM for DPDK-bound interfaces.** A VF bound to a DPDK
  driver might have no kernel netdev, so CNI IPAM plugins cannot assign an IP
  to it. `GroutPort` carries inline IPAM applied via `grcli`.
- **Preserve the kernel-based TAP path as a fallback.** The existing
  `NetworkDevice` mode remains available for environments without
  SR-IOV hardware.

### Non-Goals

- **Managing VF creation or driver binding on the host.** The user
  (or the SR-IOV device plugin) creates VFs before the controller
  consumes them.
- **Workload-facing VF pairs for L2VNI HW acceleration.** Covered by
  milestone M3a in [grout-dataplane](grout-dataplane.md).
- **Replacing CNIDevice mode.** CNIDevice serves kernel-based NIC
  sharing (macvlan/ipvlan). GroutPort is for DPDK-bound VFs.

## User Stories

#### Story 1: High-Throughput Underlay
As a network operator with SR-IOV-capable NICs, I want grout to attach
a VF directly via DPDK so that underlay traffic avoids the kernel
entirely and achieves line-rate forwarding.

#### Story 2: VF Selection by PCI Address
As an operator who pre-provisions VFs, I want to specify the VF's PCI
address in the Underlay spec so the controller binds the correct device.

#### Story 3: VF Selection by PF + Index
As an operator on a multi-NIC node, I want to specify the PF name and VF
index so that the controller resolves the correct VF without requiring
me to look up PCI addresses.

## Proposal

### Overview

After the controller-provisioned-underlay-interfaces enhancement, the
`UnderlayInterface` union has two modes: `NetworkDevice` and `CNIDevice`.
This enhancement adds a third: `GroutPort`.

| Mode | Behavior | IPAM | Datapath |
|------|----------|------|----------|
| `NetworkDevice` | Moves host device; grout creates TAP+`remote=` | Native (CIDR-derived) | TAP PMD (kernel TC redirect) |
| `CNI` | Invokes CNI plugin | Delegated to CNI | Kernel |
| `GroutPort` | Binds a VF to grout as a DPDK port | Inline in spec | DPDK PMD (user-space) |

`GroutPort` is only valid when `--grout-enabled=true`. The controller
rejects it in kernel datapath mode. A webhook validation must be implemented 
for this.

### VF Selection

The operator identifies the target VF through two mutually exclusive
field groups on `GroutPortConfig`:

| Selector | Use case |
|----------|----------|
| `pciAddress` | Exact VF PCI Address (e.g. `0000:03:02.0`) |
| `pfName`, `vfIndex` | PF name + VF index (e.g. `enp3s0f0` + `2`) |

Both resolve to a PCI address at reconcile time via sysfs.

### Port Creation Flow

1. **Resolve VF** — `pciAddress` used directly; `pfName`, `vfIndex` reads
   `/sys/class/net/<pfName>/device/virtfn<vfIndex>` symlink. The VF must
   already be bound to a DPDK-compatible driver (e.g. `vfio-pci`) or use
   a bifurcated driver (e.g. `mlx5`); driver binding is the
   responsibility of the user or SR-IOV device plugin (see Non-Goals).
2. Moves the VF netlink to the perouter namespace, when dealing with
   bifurcated PMD drivers (e.g. `mlx5`)
3. **Create grout port** —
   `grcli interface add port u_<name> devargs <pci> [mtu MTU] [rxqs N_RXQ] [qsize Q_SIZE]`
   Options are appended only when set in `portOptions`.
4. **Assign addresses** —
   `grcli address add <cidr> iface u_<name>` for each IPAM address.
5. **Kernel route for FRR** — add connected route on `main` so BGP
   sessions transit grout (same as today).

### API Examples

##### GroutPort with PCI address

```yaml
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: Underlay
metadata:
  name: underlay-dpdk
spec:
  asn: 64514
  interfaces:
    - type: GroutPort
      groutPort:
        pciAddress: "0000:03:02.0"
        ipam:
          addresses:
            - 192.168.1.10/24
  tunnelEndpoint:
    cidrs:
      - "100.65.0.0/24"
  neighbors:
    - address: 192.168.1.1
      asn: 65000
```

##### GroutPort with PF + VF index (per-node)

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
    - type: GroutPort
      groutPort:  
        pfName: enp3s0f0
        vfIndex: 0
        ipam:
          addresses:
            - 192.168.1.10/24
  neighbors:
    - address: 192.168.1.1
      asn: 65000
```

## Design Details

### API Types

```go
// +union
type UnderlayInterface struct {
	// +kubebuilder:validation:Enum=NetworkDevice;CNI;GroutPort
	// +unionDiscriminator
	Type          string              `json:"type,omitempty"`
	NetworkDevice *NetworkDeviceConfig `json:"networkDevice,omitempty"`
	CNIDevice     *CNIDeviceConfig     `json:"cniDevice,omitempty"`
	GroutPort     *GroutPortConfig     `json:"groutPort,omitempty"`
}

// GroutPortConfig specifies a VF to bind to grout as a DPDK port.
// Exactly one selector must be used: either pciAddress alone, or pfName + vfIndex together.
// +kubebuilder:validation:XValidation:rule="has(self.pciAddress) != (has(self.pfName) && has(self.vfIndex))",message="specify either pciAddress or both pfName and vfIndex, not both"
// +kubebuilder:validation:XValidation:rule="!has(self.pfName) || has(self.vfIndex)",message="vfIndex is required when pfName is set"
// +kubebuilder:validation:XValidation:rule="!has(self.vfIndex) || has(self.pfName)",message="pfName is required when vfIndex is set"
type GroutPortConfig struct {
	// +kubebuilder:validation:Pattern=`^[0-9a-fA-F]{4}:[0-9a-fA-F]{2}:[0-9a-fA-F]{2}\.[0-7]$`
	PCIAddress *string            `json:"pciAddress,omitempty"`
	// +kubebuilder:validation:Pattern=`^[a-zA-Z][a-zA-Z0-9._-]*$`
	// +kubebuilder:validation:MaxLength=15
	PFName      *string           `json:"pfName,omitempty"`
	// +kubebuilder:validation:Minimum=0
	VFIndex     *int              `json:"vfIndex,omitempty"`
	IPAM        GroutPortIPAM     `json:"ipam"`
	PortOptions *GroutPortOptions `json:"portOptions,omitempty"`
}

type GroutPortIPAM struct {
    // At most one IPv4 and one IPv6 (dual-stack).
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=2
	// +kubebuilder:validation:XValidation:rule="self.all(c, isCIDR(c))",message="all entries must be valid CIDRs"
	// +kubebuilder:validation:XValidation:rule="self.filter(c, isCIDR(c) && cidr(c).ip().family() == 4).size() <= 1",message="at most one IPv4 address is allowed"
	// +kubebuilder:validation:XValidation:rule="self.filter(c, isCIDR(c) && cidr(c).ip().family() == 6).size() <= 1",message="at most one IPv6 address is allowed"
	Addresses []string `json:"addresses"`
}

type GroutPortOptions struct {
	// +kubebuilder:validation:Minimum=68
	// +kubebuilder:validation:Maximum=9702
	MTU *int `json:"mtu,omitempty"`
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=64
	RXQueues *int `json:"rxQueues,omitempty"`
	// +kubebuilder:validation:Minimum=64
	// +kubebuilder:validation:Maximum=32768
	QSize *int `json:"qSize,omitempty"`
}
```

### Datapath Validation

`KernelDatapathConfigValidator` is extended to reject `GroutPort`.

### VF Resolution

Operates on sysfs:

- **PCIAddress**: validate format, check `/sys/bus/pci/devices/<addr>`.
- **PFVFIndex**: read symlink `/sys/class/net/<pf>/device/virtfn<idx>`,
  extract PCI address from target path.

*Optional*: The resolved PCI address is stored in Underlay status for observability.

### Teardown

On Underlay deletion or netns rebuild:

1. `grcli interface del u_<name>` — removes the DPDK port.
2. If the VF binds a bifurcated driver (e.g. `mlx5`) the VF returns
   to the host namespace.

### Risks and Mitigations

| Risk | Mitigation |
|------|------------|
| VF not available (not created, already bound) | Clear error at reconcile with PCI address and resolution source |
| DPDK driver not loaded for the VF's NIC model | Document supported NIC families; grout logs probe failure |
| VF kernel netdev disappears after DPDK binding | Resolution reads sysfs before binding |
| Multiple Underlays claim the same VF | `grcli interface add` fails with "device busy"; surfaced in status |

### Test Plan

- **E2E tests / Kind**: Against grout in test-mode (no hugepages),
  verify port creation with `net_tap` devargs (no VF hardware in CI),
  address assignment, and teardown.
- **E2E tests / QEMU**: Deploy a cluster based on KVM / QEMU with emulated
  SR-IOV NICs. Running the entire e2etest suite is hard, as the same clab
  topology can't be implemented with VMs. A small set of test cases will be 
  implemented for this lane, using a simple FRR BGP peer in a container.
- **Validation tests**: `GroutPort` rejected when grout disabled;
  missing VFSelector sub-struct rejected by CEL.

## Alternatives

### Alternative 1: Use CNIDevice with SR-IOV CNI

Use existing `CNIDevice` mode with an `sriov` CNI plugin config.

**Why not chosen:** The SR-IOV CNI moves a VF kernel netdev into a
container namespace — it does not hand off to grout's DPDK port creation.
IPAM via CNI is meaningless for DPDK-bound interfaces (no kernel netdev
to assign the IP to).

### Alternative 2: Add DPDK devargs to NetworkDevice

Extend `NetworkDeviceConfig` with an optional `devargs` field.

**Why not chosen:** `NetworkDevice` semantics are "move a kernel device."
DPDK port binding is fundamentally different — no kernel device to move.
Mixing both in one type makes validation harder and the API confusing.
No VF selector abstraction — the operator must always know PCI addresses.

### Alternative 3: Allow selecting the VF by its netlink name

The VF is supposed to be bound to a DPDK driver, which can be `vfio-pci`. 
In such case there is no kernel netlink for the VF.
