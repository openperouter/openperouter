# DPDK-Accelerated Underlay Ports for Grout

## Summary

Replace the TAP-with-`remote=` underlay port mechanism in grout with
direct DPDK port attachment. After the
[grout-dataplane](grout-dataplane.md) enhancement (M1),
`configureUnderlayPort()` creates a `net_tap` PMD with `remote=<nic>`,
which installs TC ingress rules to redirect packets between the underlay
NIC and grout — adding kernel overhead on the underlay fast path. By
binding a network device directly to a DPDK poll-mode driver
(e.g. `vfio-pci`), grout can send and receive underlay traffic entirely
in user-space, eliminating the kernel data path for VXLAN encap/decap
and BGP-learned forwarding.

The controller takes ownership of driver binding and IP address
migration: it reads the IP configuration from the kernel netdev before
binding the DPDK driver, applies it to the grout port, and restores it
on teardown.

This enhancement extends `NetworkDevice` with an optional `Grout` field.
When `Grout` is set, the controller binds the device as a DPDK port
instead of creating a TAP+`remote=` bridge. When `Grout` is absent, the
existing TAP-based path is used (no behavioral change).

## Motivation

### Goals

- **Remove the kernel from the underlay fast path.** The current
  TAP+`remote=` approach routes every underlay packet through TC ingress
  rules in the kernel. Direct DPDK port attachment eliminates this
  overhead.
- **Opt-in per interface.** Operators choose DPDK acceleration on a
  per-interface basis by adding the `grout` field to a `NetworkDevice`
  entry. Interfaces without it continue to use the TAP path.
- **Automatic IP address migration.** The controller reads the IP
  configuration from the kernel netdev before binding the DPDK driver
  and applies it to the grout port via `grcli`. On teardown the
  original driver and IP addresses are restored.
- **Controller-managed driver binding.** The controller determines the
  NIC type and binds the appropriate driver (`vfio-pci` for Intel,
  namespace move for Mellanox/bifurcated). The user does not need to
  pre-bind devices to a DPDK driver.
- **Preserve the kernel-based TAP path as default.** A `NetworkDevice`
  without `grout` behaves exactly as today — no migration required for
  existing Underlay resources.

### Non-Goals

- **Managing VF creation on the host.** When SR-IOV is used, the user
  creates VFs (`echo N > /sys/.../sriov_numvfs`) before the controller
  consumes them.
- **Workload-facing VF pairs for L2VNI HW acceleration.** Covered by
  milestone M3a in [grout-dataplane](grout-dataplane.md).
- **Replacing CNIDevice mode.** CNIDevice serves kernel-based NIC
  sharing (macvlan/ipvlan). The `grout` field on `NetworkDevice` is for
  DPDK-bound devices.

## User Stories

#### Story 1: High-Throughput Underlay
As a network operator with DPDK-capable NICs, I want grout to attach
a device directly via DPDK so that underlay traffic avoids the kernel
entirely and achieves line-rate forwarding.

#### Story 2: Opt-In Acceleration
As an operator running grout, I want to add `grout: {}` to an existing
`NetworkDevice` interface to switch it from TAP mode to DPDK mode
without changing anything else in my Underlay spec.

#### Story 3: Tuning Port Parameters
As an operator, I want to set MTU, RX queue count, and queue size on
a DPDK-bound underlay port to match my NIC capabilities and traffic
profile.

## Proposal

### Overview

The existing `NetworkDevice` type gains an optional `grout` field. When
present, the controller binds the device as a DPDK port instead of
creating a TAP+`remote=` bridge:

| `grout` field | Behavior | IPAM | Datapath |
|---------------|----------|------|----------|
| Absent | Moves host device; grout creates TAP+`remote=` | Native (CIDR-derived) | TAP PMD (kernel TC redirect) |
| Present | Binds device to DPDK driver; creates grout port | Scraped from kernel netdev | DPDK PMD (user-space) |

The `grout` field is only valid when `--datapath=grout`. The controller
rejects it in kernel datapath mode. A webhook validation must be
implemented for this.

### Device Selection

The device is identified by the existing `interfaceName` field on
`NetworkDevice`, which is the kernel netlink device name (e.g.
`enp3s0f0v0`). The controller resolves the PCI address from sysfs at
reconcile time via `/sys/class/net/<name>/device`.

### Port Creation Flow

1. **Resolve device** — Read the PCI address from
   `/sys/class/net/<interfaceName>/device` symlink.
2. **Save original state** — Read the IP addresses from the netlink
   device and the current kernel driver name (from
   `/sys/bus/pci/devices/<addr>/driver`). Write both to a per-device
   state file at `/var/run/openperouter/grout/<pci-address>.json`.
3. **Bind DPDK driver** — Determine the NIC type:
   - *Non-bifurcated* (e.g. Intel): unbind from the current kernel driver
     and bind to `vfio-pci`. This destroys the kernel netdev.
   - *Bifurcated* (e.g. Mellanox `mlx5`): move the netlink device into
     the perouter namespace. The kernel driver stays; DPDK shares the
     device via the bifurcated model.
4. **Create grout port** —
   `grcli interface add port u_<name> devargs <pci> [mtu MTU] [rxqs N_RXQ] [qsize Q_SIZE]`
   Options are appended only when set in `grout`.
5. **Assign addresses** —
   `grcli address add <cidr> iface u_<name>` for each saved IP address.
6. **Kernel route for FRR** — add connected route on `main` so BGP
   sessions transit grout (same as today).

### API Examples

##### NetworkDevice with DPDK acceleration (defaults)

The device `enp3s0f0v0` must already exist as a kernel netdev with
an IP address configured (e.g. via `ip addr add`).

```yaml
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: Underlay
metadata:
  name: underlay-dpdk
spec:
  asn: 64514
  interfaces:
    - type: NetworkDevice
      networkDevice:
        interfaceName: enp3s0f0v0
        grout: {}
  tunnelEndpoint:
    cidrs:
      - "100.65.0.0/24"
  neighbors:
    - address: 192.168.1.1
      asn: 65000
```

##### NetworkDevice with DPDK acceleration and port options

```yaml
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: Underlay
metadata:
  name: underlay-dpdk-tuned
spec:
  asn: 64514
  interfaces:
    - type: NetworkDevice
      networkDevice:
        interfaceName: enp3s0f0v0
        grout:
          mtu: 9000
          rxQueues: 4
          qSize: 1024
  neighbors:
    - address: 192.168.1.1
      asn: 65000
```

##### NetworkDevice without grout (TAP path, unchanged behavior)

```yaml
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: Underlay
metadata:
  name: underlay-tap
spec:
  asn: 64514
  interfaces:
    - type: NetworkDevice
      networkDevice:
        interfaceName: enp3s0f0
  neighbors:
    - address: 192.168.1.1
      asn: 65000
```

## Design Details

### API Types

```go
// NetworkDevice moves an existing host network device into the router netns.
type NetworkDevice struct {
	// interfaceName is the name of the host network device to move into
	// the router netns.
	// +kubebuilder:validation:Pattern=`^[a-zA-Z][a-zA-Z0-9._-]*$`
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=15
	// +required
	InterfaceName string `json:"interfaceName,omitempty"`

	// grout, when set, binds the device as a DPDK port instead of
	// creating a TAP+remote= bridge. Only valid when --datapath=grout.
	// +optional
	Grout *GroutPortOptions `json:"grout,omitempty"`
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

The `UnderlayInterface` union is unchanged — no new discriminator
variant is needed:

```go
// +union
type UnderlayInterface struct {
	// +kubebuilder:validation:Enum=NetworkDevice;CNIDevice
	// +unionDiscriminator
	Type          UnderlayInterfaceType `json:"type,omitempty"`
	NetworkDevice *NetworkDevice        `json:"networkDevice,omitempty"`
	CNIDevice     *CNIDevice            `json:"cniDevice,omitempty"`
}
```

### Saved Device State

Before binding the DPDK driver the controller writes a per-device state
file to `/var/run/openperouter/grout/<pci-address>.json`:

```json
{
  "pciAddress": "0000:03:02.0",
  "netlinkName": "enp3s0f0v0",
  "originalDriver": "ice",
  "addresses": ["192.168.1.10/24", "fd00::10/64"]
}
```

This file is read at teardown to restore the original driver and IP
addresses. It is deleted after successful restoration.

### Datapath Validation

`KernelDatapathConfigValidator` is extended to reject `NetworkDevice`
entries that have `grout` set.

### Device Resolution

The device is identified by `interfaceName` (the kernel netlink name).
The controller resolves the PCI address at reconcile time:

- Validate the device exists at `/sys/class/net/<interfaceName>`.
- Read PCI address from the `/sys/class/net/<interfaceName>/device`
  symlink (resolves to `/sys/bus/pci/devices/<addr>`).

### Teardown

On Underlay deletion or netns rebuild:

1. `grcli interface del u_<name>` — removes the DPDK port.
2. Read the saved state file from
   `/var/run/openperouter/grout/<pci-address>.json`.
3. **Restore driver** — Rebind the device to the original kernel driver
   recorded in the state file (`echo <pci> > /sys/bus/pci/drivers/<orig>/bind`).
   For bifurcated drivers, move the netlink device back to the host
   namespace.
4. **Restore IP addresses** — Re-apply the saved IP addresses to the
   re-created kernel netdev.
5. Delete the state file.

### Risks and Mitigations

| Risk | Mitigation |
|------|------------|
| Device not available (no kernel netdev) | Clear error at reconcile with device name |
| No IP address configured on the device | The grout port is configured with zero IP addresses |
| `vfio-pci` module not loaded | Check before binding; surface actionable error in status |
| State file lost (e.g. `/var/run` cleared) | Log warning; operator must manually rebind driver and restore IP |
| Driver rebind fails at teardown | Retry with backoff; leave state file for manual recovery |
| Multiple Underlays claim the same device | `grcli interface add` fails with "device busy"; surfaced in status |

### Test Plan

- **E2E tests / Kind**: Against grout in test-mode (no hugepages),
  verify port creation with `net_tap` devargs (no VF hardware in CI),
  address assignment, and teardown.
- **E2E tests / QEMU**: Deploy a cluster based on KVM / QEMU with emulated
  SR-IOV NICs. Running the entire e2etest suite is hard, as the same clab
  topology can't be implemented with VMs. A small set of test cases will be
  implemented for this lane, using a simple FRR BGP peer in a container.
- **Validation tests**: `grout` field rejected when grout disabled;
  verify field validation ranges for MTU, RXQueues, QSize.

## Alternatives

### Alternative 1: Use CNIDevice with SR-IOV CNI

Use existing `CNIDevice` mode with an `sriov` CNI plugin config.

**Why not chosen:** The SR-IOV CNI moves a kernel netdev into a
container namespace — it does not hand off to grout's DPDK port creation.
IPAM via CNI is meaningless for DPDK-bound interfaces (no kernel netdev
to assign the IP to).

### Alternative 2: New GroutDevice union variant

Add a `GroutDevice` discriminator to `UnderlayInterface` with its own
selectors (PCI address, PF+VF index, netlink name).

**Why not chosen:** The DPDK port is still a network device identified
by its netlink name — the same selection mechanism `NetworkDevice` already
uses. Adding a separate union variant duplicates device selection logic
and forces operators to change `type:` when toggling between TAP and
DPDK mode. An optional field on `NetworkDevice` is simpler: operators
add or remove `grout: {}` without restructuring the spec.

### Alternative 3: Inline IPAM in the API spec

Carry IP addresses in the `Grout` struct (e.g. an `addresses` list)
instead of reading them from the kernel.

**Why not chosen:** The device must have a kernel netdev with an IP
address *before* the controller binds it — reading the IP from the
existing configuration is simpler, avoids duplication between the host
config and the Underlay spec, and guarantees that the address is
restored correctly on teardown.
