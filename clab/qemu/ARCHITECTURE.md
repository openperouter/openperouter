# QEMU Containerlab Architecture

## Overview

The Containerlab nodes `pe-kind-control-plane` and `pe-kind-worker` each
run a Fedora Cloud VM using `docker.io/qemux/qemu:7.50` with access to
`/dev/kvm` and `/dev/net/tun`. Both mount `clab/qemu/vm` at `/vm` and
share the base image and cloud-init ISO. Each creates its own writable
overlay and serial log.

`vm/run_vm.sh` bridges the Containerlab interfaces to guest NICs through
TAP devices. Each guest has four underlay NICs, a dedicated `cluster`
NIC for k3s and Flannel traffic, and a QEMU user-network NIC for outbound
access and host port forwarding. `vm/setup.sh` runs from the host to
configure both VMs, start the server and join the worker, and install
FRR-k8s, the CNI plugins, and Multus.

## NIC Naming Convention

Each QEMU NIC is connected to a clab interface via a Linux bridge
inside the container:

```
Clab interface          Bridge          TAP              Guest (igb/virtio)
──────────────────────────────────────────────────────────────────────
 toswitch1       ◄──►  toswitch1_br  ◄──►  toswitch1_t  │  toswitch1
```

The guest NIC is renamed from its kernel name (e.g. `ens4`) to match
the Containerlab interface name. `vm/run_vm.sh` generates MAC-based udev
rules and passes them through QEMU fw_cfg; cloud-init writes them to
`/etc/udev/rules.d/70-persistent-net.rules`.

## NICs

| # | Clab interface | Bridge (container) | TAP (container) | Guest NIC   | MAC               |
|---|----------------|--------------------|-----------------|-------------|-------------------|
| 1 | toswitch1      | toswitch1_br       | toswitch1_t     | toswitch1   | 52:54:00:ab:cd:01 |
| 2 | toswitch2      | toswitch2_br       | toswitch2_t     | toswitch2   | 52:54:00:ab:cd:02 |
| 3 | toleafkind1    | toleafkind1_br     | toleafkind1_t   | toleafkind1 | 52:54:00:ab:cd:03 |
| 4 | toleafkind2    | toleafkind2_br     | toleafkind2_t   | toleafkind2 | 52:54:00:ab:cd:04 |
| 5 | cluster        | cluster_br         | cluster_t       | cluster     | 52:54:00:ab:cd:05 |

The tables show control-plane MAC addresses. The worker uses
`52:54:00:ab:ce` with the same suffixes.

## Emulated SR-IOV VFs

`vm/create-vfs.sh` emulates six SR-IOV VFs of the `toleafkind1` PF. Each VF
is a virtio NIC whose TAP joins `toleafkind1_br`, which has VLAN filtering
enabled and acts as the NIC embedded switch between the PF uplink and its
VFs. The guest names VF `<index>` `toleafkind1v<index>`.

| VF | Guest NIC     | MAC               | Bridge VLANs             |
|----|---------------|-------------------|--------------------------|
| 0  | toleafkind1v0 | 52:54:00:ab:cd:06 | 33, 44 tagged (trunk)    |
| 1  | toleafkind1v1 | 52:54:00:ab:cd:07 | 33, 44 tagged (trunk)    |
| 2  | toleafkind1v2 | 52:54:00:ab:cd:08 | 33 untagged (access)     |
| 3  | toleafkind1v3 | 52:54:00:ab:cd:09 | 33 untagged (access)     |
| 4  | toleafkind1v4 | 52:54:00:ab:cd:0a | 44 untagged (access)     |
| 5  | toleafkind1v5 | 52:54:00:ab:cd:0b | 44 untagged (access)     |

The emulated VFs are not linked to the PF in sysfs, so the QEMU lane deploys
the `config/grout-qemu` layer, which sets the test-only
`TEST_FLAG_DO_NOT_USE_EMULATED_SRIOV_VFS` environment variable on the
controller. With the variable set, VF `<index>` of PF `<pfName>` in
an SR-IOV L2Trunk resolves to the netdev `<pfName>v<index>`, instead of the
PF's `virtfn<index>` sysfs link.

## Containerlab topology

```
┌──── pe-kind-control-plane ────┐              ┌────── pe-kind-worker ───────┐
│ Fedora VM (k3s server)        │              │ Fedora VM (k3s agent)      │
│ cluster → TAP → bridge       ├──────────────┤ bridge ← TAP ← cluster    │
│ SSH :2222, API :6443          │   clab link  │ SSH :2223                  │
└───────────────────────────────┘              └────────────────────────────┘

Underlay wiring in each Containerlab node

Guest NIC     TAP             Container bridge   Clab interface  Clab peer
toswitch1  ── toswitch1_t  ── toswitch1_br    ── toswitch1    ── leafkind1-sw (bridge)
toswitch2  ── toswitch2_t  ── toswitch2_br    ── toswitch2    ── leafkind2-sw (bridge)
toleafkind1 ─ toleafkind1_t ─ toleafkind1_br  ── toleafkind1  ── leafkind1 (FRR)
toleafkind2 ─ toleafkind2_t ─ toleafkind2_br  ── toleafkind2  ── leafkind2 (FRR)
```

The `cluster` link connects the two VMs directly for k3s API and Flannel
traffic. The control plane uses `172.30.250.1/30` and `2001:db8:250::1/64`.
The worker uses `172.30.250.2/30` and `2001:db8:250::2/64`.
Kubernetes uses the IPv4 addresses as node IPs.

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
leafkind1:tokindworker ──► pe-kind-worker:toleafkind1
leafkind2:tokindworker ──► pe-kind-worker:toleafkind2
pe-kind-worker:toswitch1 ──► leafkind1-sw:kindwrk1sw1
pe-kind-worker:toswitch2 ──► leafkind2-sw:kindwrk1sw2
pe-kind-control-plane:cluster ──► pe-kind-worker:cluster
```

The topology file is [kind.clab.yml](kind.clab.yml). The generated leaf
configuration files used by it are under `clab/singlecluster/` and
`clab/leaf*/`; `deploy-clab.sh` regenerates them before deployment.

## Underlay addresses

`ip_map.txt` configures the leaf addresses. `vm/setup.sh` configures
the guest addresses using the node names and addresses in the standard
end-to-end test topology.

| Interface | Leaf | Control plane | Worker |
|-----------|------|---------------|--------|
| `toswitch1` | `192.168.11.2/24`, `2001:db8:11::2/64` | `192.168.11.3/24`, `2001:db8:11::3/64` | `192.168.11.4/24`, `2001:db8:11::4/64` |
| `toswitch2` | `192.168.12.2/24`, `2001:db8:12::2/64` | `192.168.12.3/24`, `2001:db8:12::3/64` | `192.168.12.4/24`, `2001:db8:12::4/64` |

## Port Mappings

| Default host port | Container port | VM port | Purpose |
|-------------------|----------------|---------|---------|
| 2222 | 2222 | 22 | Control-plane SSH |
| 2223 | 2222 | 22 | Worker SSH |
| 6443 | 6443 | 6443 | Kubernetes API |

## Scripts and generated files

| Script             | Purpose                                                   |
|--------------------|-----------------------------------------------------------|
| `deploy-clab.sh` | Deploys the topology, assigns Containerlab IPs, and runs container setup |
| `vm/run_vm.sh` | Creates the overlay, bridges, TAPs, and launches QEMU |
| `vm/setup.sh` | Waits for SSH, reboots for cloud-init changes, and bootstraps both guests |
| `vm/load-image.sh` | Imports a container image into both guests via SCP |
| `vm/prepare-vm-image.sh` | Downloads and resizes the Fedora base image to 20 GiB |
| `vm/prepare-vm-iso.sh` | Generates the SSH key and cloud-init ISO |
| `vm/qemu-common.sh` | Shared SSH key, port, and VM helper functions |

`vm/cloud-init/{meta-data,user-data}` supplies the guest cloud-init data.
The Makefile creates `vm/fedora-cloud.qcow2`, `vm/cloud-init.iso`, and the
SSH key; each running container creates `vm/overlay-<node-name>.qcow2` and
`vm/serial-<node-name>.log`. These runtime artifacts are ignored by Git.
