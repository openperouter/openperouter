# QEMU Containerlab Architecture

## Overview

QEMU runs inside the `pe-kind-control-plane` Containerlab node. The node
uses `docker.io/qemux/qemu:7.50`, passes through `/dev/kvm` and
`/dev/net/tun`, and mounts `clab/qemu/vm` at `/vm`. Its
`vm/entrypoint.sh` bridges four Containerlab interfaces to TAP devices,
then launches QEMU with four `igb` NICs and one virtio management NIC.

The VM uses a Fedora Cloud Base image, a per-start `overlay.qcow2`, and a
cloud-init ISO. `vm/setup.sh` reboots the guest once for cloud-init changes,
configures the two underlay interfaces, installs k3s, and deploys FRR-k8s,
the CNI plugins, and Multus. SSH and the Kubernetes API are exposed through
the management NIC.

## NIC Naming Convention

Each QEMU NIC is connected to a clab interface via a Linux bridge
inside the container:

```
Clab interface          Bridge          TAP              Guest (igb)
──────────────────────────────────────────────────────────────────────
 toswitch1       ◄──►  toswitch1_br  ◄──►  toswitch1_t  │  toswitch1
```

The guest NIC is renamed from its kernel name (e.g. `ens4`) to match
the Containerlab interface name. `vm/entrypoint.sh` generates MAC-based udev
rules and passes them through QEMU fw_cfg; cloud-init writes them to
`/etc/udev/rules.d/70-persistent-net.rules`.

## NICs

| # | Clab interface | Bridge (container) | TAP (container) | Guest NIC   | MAC               |
|---|----------------|--------------------|-----------------|-------------|-------------------|
| 1 | toswitch1      | toswitch1_br       | toswitch1_t     | toswitch1   | 52:54:00:ab:cd:01 |
| 2 | toswitch2      | toswitch2_br       | toswitch2_t     | toswitch2   | 52:54:00:ab:cd:02 |
| 3 | toleafkind1    | toleafkind1_br     | toleafkind1_t   | toleafkind1 | 52:54:00:ab:cd:03 |
| 4 | toleafkind2    | toleafkind2_br     | toleafkind2_t   | toleafkind2 | 52:54:00:ab:cd:04 |

## Containerlab topology

```
┌──────────────── pe-kind-control-plane (clab container) ─────────────┐
│                                                                     │
│   toswitch1 ──bridge── toswitch1_t ─┐                               │
│   toswitch2 ──bridge── toswitch2_t ─┤  QEMU VM (igb NICs)           │
│   toleafkind1──bridge──toleafkind1_t┤  + virtio mgmt NIC            │
│   toleafkind2──bridge──toleafkind2_t┘  (hostfwd :2222→:22,          │
│                                          :6443→:6443)               │
└───┼────────────┼───────────┼─────────────┼──────────────────────────┘
    │            │           │             │            clab links
    ▼            ▼           ▼             ▼
 leafkind1-sw  leafkind2-sw leafkind1    leafkind2
 (bridge)      (bridge)     (FRR)        (FRR)
```

The topology also contains the `leafA`, `leafB`, and `leafSRV6` FRR leaves,
seven agnhost test hosts, `spine`, and two Linux bridge nodes. Its complete
link list is:

```
leafA:eth1              ──► spine:eth1
leafB:eth1              ──► spine:eth2
leafSRV6:eth1           ──► spine:ethsrv6
leafkind1:eth1          ──► spine:eth3
leafkind2:eth1          ──► spine:eth4
leafA:ethred            ──► hostA_red:eth1
leafA:ethdefault        ──► hostA_default:eth1
leafA:ethblue           ──► hostA_blue:eth1
leafB:ethred            ──► hostB_red:eth1
leafB:ethblue           ──► hostB_blue:eth1
leafSRV6:ethred         ──► hostSRV6_red:eth1
leafSRV6:ethblue        ──► hostSRV6_blue:eth1
leafkind1:toswitch1     ──► leafkind1-sw:leaf1
leafkind2:toswitch2     ──► leafkind2-sw:leaf2
leafkind1:tokindctrlpl  ──► pe-kind-control-plane:toleafkind1
leafkind2:tokindctrlpl  ──► pe-kind-control-plane:toleafkind2
pe-kind-control-plane:toswitch1 ──► leafkind1-sw:kindctrlpl1
pe-kind-control-plane:toswitch2 ──► leafkind2-sw:kindctrlpl2
```

The topology file is [kind.clab.yml](kind.clab.yml). The generated leaf
configuration files used by it are under `clab/singlecluster/` and
`clab/leaf*/`; `deploy-clab.sh` regenerates them before deployment.

## Underlay addresses

The two leafkind switches and the VM use the addresses configured by
`ip_map.txt` and `vm/setup.sh`:

| Interface pair | Leaf address | VM address |
|----------------|--------------|------------|
| `leafkind1:toswitch1` / VM `toswitch1` | `192.168.11.2/24`, `2001:db8:11::2/64` | `192.168.11.3/24`, `2001:db8:11::3/64` |
| `leafkind2:toswitch2` / VM `toswitch2` | `192.168.12.2/24`, `2001:db8:12::2/64` | `192.168.12.3/24`, `2001:db8:12::3/64` |

## Port Mappings

| Host port | Container port | QEMU hostfwd | VM port | Purpose      |
|-----------|----------------|--------------|---------|--------------|
| 2222      | 2222           | :2222→:22    | 22      | SSH          |
| 6443      | 6443           | :6443→:6443  | 6443    | Kubernetes   |

## Scripts and generated files

| Script             | Purpose                                                   |
|--------------------|-----------------------------------------------------------|
| `deploy-clab.sh` | Deploys the topology, assigns Containerlab IPs, and runs container setup |
| `vm/entrypoint.sh` | Creates the overlay, bridges, TAPs, and launches QEMU |
| `vm/setup.sh` | Waits for SSH, reboots for cloud-init changes, and bootstraps the guest |
| `vm/load-image.sh` | Imports a container image into k3s via SCP |
| `vm/prepare-vm-image.sh` | Downloads and resizes the Fedora base image to 20 GiB |
| `vm/prepare-vm-iso.sh` | Generates the SSH key and cloud-init ISO |
| `vm/collect-logs.sh` | Collects VM, Kubernetes, and FRR diagnostics |
| `vm/qemu-common.sh` | Shared SSH key, port, and VM helper functions |

`vm/cloud-init/{meta-data,user-data}` supplies the guest cloud-init data.
The Makefile creates `vm/fedora-cloud.qcow2`, `vm/cloud-init.iso`, and the
SSH key; the running container creates `vm/overlay.qcow2` and
`vm/serial.log`. These runtime artifacts are ignored by Git.
