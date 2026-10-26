#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# Entrypoint for the pe-kind-control-plane clab container.
# Waits for clab interfaces, bridges each to a TAP device, and launches
# QEMU with igb and virtio NICs backed by those TAPs.

set -euo pipefail
set -x

readonly NICS=(toswitch1 toswitch2 toleafkind1 toleafkind2)
# NICs emulated as virtio-net, the others are igb. Mixing the two covers both
# DPDK drivers grout binds the underlay to.
readonly VIRTIO_NICS=(toswitch2 toleafkind2)
VM_CPUS="${VM_CPUS:-4}"
VM_MEM="${VM_MEM:-6144}"
SSH_PORT="${SSH_PORT:-2222}"
K8S_PORT="${K8S_PORT:-6443}"
VM_BASE_IMAGE="${VM_BASE_IMAGE:-/vm/fedora-cloud.qcow2}"
VM_OVERLAY="/vm/overlay.qcow2"
CLOUD_INIT_ISO="${CLOUD_INIT_ISO:-/vm/cloud-init.iso}"
MAX_WAIT="${MAX_WAIT:-120}"
# Queue pairs and RX ring size of the underlay virtio NICs. A virtio NIC has a
# single queue pair unless multiqueue is enabled, so grout ports with more than
# one RX queue or a ring larger than the device one can't be created.
NIC_QUEUES="${NIC_QUEUES:-4}"
NIC_RX_QUEUE_SIZE="${NIC_RX_QUEUE_SIZE:-1024}"

is_virtio_nic() {
    local nic
    for nic in "${VIRTIO_NICS[@]}"; do
        if [ "${nic}" = "$1" ]; then
            return 0
        fi
    done
    return 1
}

# Create a fresh overlay so the base image stays pristine.
# Every container restart gets a clean VM.
rm -f "${VM_OVERLAY}"
qemu-img create -f qcow2 -b "${VM_BASE_IMAGE}" -F qcow2 "${VM_OVERLAY}"

for nic in "${NICS[@]}"; do
    echo "Waiting for interface ${nic}..."
    elapsed=0
    while [ ! -d "/sys/class/net/${nic}" ]; do
        sleep 1
        elapsed=$((elapsed + 1))
        if [ "$elapsed" -ge "$MAX_WAIT" ]; then
            echo "ERROR: interface ${nic} did not appear within ${MAX_WAIT}s" >&2
            exit 1
        fi
    done
done

# For each NIC eth0, there is a bridge and a tap device:
#
#        eth0 <---------------> eth0_br <---------> eth0_tap <----------> eth0 
#  <veth created by clab>      <bridge>             <atp>          <igb or virtio nic in QEMU VM>
#

QEMU_NIC_ARGS=()

# Generate udev rules so the VM renames NICs to match the names we use here.
UDEV_RULES="/tmp/70-persistent-net.rules"
: > "${UDEV_RULES}"

slot=1
for nic in "${NICS[@]}"; do
    tap="${nic}_t"
    br="${nic}_br"
    mac=$(printf "52:54:00:ab:cd:%02x" "${slot}")

    if is_virtio_nic "${nic}"; then
        # QEMU opens the TAP with one queue per virtio queue pair, which needs
        # a multi_queue TAP.
        ip tuntap add dev "${tap}" mode tap multi_queue
        QEMU_NIC_ARGS+=(
            -device "pcie-root-port,id=rp${slot},slot=${slot}"
            -netdev "tap,id=${tap},ifname=${tap},script=no,downscript=no,queues=${NIC_QUEUES}"
            -device "virtio-net-pci,bus=rp${slot},netdev=${tap},mac=${mac},iommu_platform=on,ats=on,mq=on,vectors=$((2 * NIC_QUEUES + 2)),rx_queue_size=${NIC_RX_QUEUE_SIZE}"
        )
    else
        ip tuntap add dev "${tap}" mode tap
        QEMU_NIC_ARGS+=(
            -device "pcie-root-port,id=rp${slot},slot=${slot}"
            -netdev "tap,id=${tap},ifname=${tap},script=no,downscript=no"
            -device "igb,bus=rp${slot},netdev=${tap},mac=${mac}"
        )
    fi
    ip link add "${br}" type bridge
    ip link set "${nic}" master "${br}"
    ip link set "${tap}" master "${br}"
    ip link set "${br}" up
    ip link set "${nic}" up
    ip link set "${tap}" up

    echo "Bridge ${br}: ${nic} <-> ${tap} (mac ${mac})"

    echo "SUBSYSTEM==\"net\", ACTION==\"add\", ATTR{address}==\"${mac}\", NAME=\"${nic}\"" >> "${UDEV_RULES}"

    slot=$((slot + 1))
done

echo "Launching QEMU with ${#NICS[@]} NICs (virtio: ${VIRTIO_NICS[*]})..."
MGMT_NETDEV="user,id=mgmt,hostfwd=tcp::${SSH_PORT}-:22,hostfwd=tcp::${K8S_PORT}-:6443"

# DPDK maps its buffers with IOVA=VA through vfio, so the virtio NICs it
# drives must translate DMA through the vIOMMU (iommu_platform, plus ats with
# device-iotlb). Without it QEMU reads the IOVAs as guest-physical addresses
# and grout hangs waiting on the virtio control queue.
exec qemu-system-x86_64 \
    -machine q35,kernel-irqchip=split \
    -device intel-iommu,intremap=on,caching-mode=on,device-iotlb=on \
    -enable-kvm \
    -cpu host \
    -smp "${VM_CPUS}" \
    -m "${VM_MEM}" \
    -drive file="${VM_OVERLAY}",if=virtio,format=qcow2 \
    -cdrom "${CLOUD_INIT_ISO}" \
    -netdev "${MGMT_NETDEV}" \
    -device virtio-net-pci,netdev=mgmt \
    "${QEMU_NIC_ARGS[@]}" \
    -fw_cfg "name=opt/udev-nic-rules,file=${UDEV_RULES}" \
    -smbios "type=1,serial=ds=nocloud;h=`hostname`;i=`hostname`" \
    -display none \
    -serial file:/vm/serial.log \
    -monitor unix:/tmp/monitor.sock,server,nowait
