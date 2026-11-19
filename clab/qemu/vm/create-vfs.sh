# SPDX-License-Identifier: Apache-2.0
#
# Sourced by run_vm.sh. Emulates SR-IOV VFs of the toleafkind1 PF: each VF is
# a virtio NIC whose TAP joins toleafkind1_br, so the VLAN-filtering bridge
# acts as the NIC embedded switch between the PF uplink and its VFs.
#
# The guest names VF <index> "toleafkind1v<index>", the name the controller
# resolves VF <index> of PF toleafkind1 to when
# TEST_FLAG_DO_NOT_USE_EMULATED_SRIOV_VFS is set.
#
#   VF 0, 1   trunk ports, VLANs 33 and 44 tagged
#   VF 2, 3   access ports, VLAN 33 untagged
#   VF 4, 5   access ports, VLAN 44 untagged
#
# Uses and updates the slot, QEMU_NIC_ARGS and UDEV_RULES variables of
# run_vm.sh.

readonly EMULATED_PF=toleafkind1
readonly EMULATED_PF_BRIDGE="${EMULATED_PF}_br"
readonly EMULATED_VF_COUNT=6

ip link set "${EMULATED_PF_BRIDGE}" type bridge vlan_filtering 1
bridge vlan add vid 33 dev "${EMULATED_PF_BRIDGE}" self
bridge vlan add vid 44 dev "${EMULATED_PF_BRIDGE}" self

for ((vf_index = 0; vf_index < EMULATED_VF_COUNT; vf_index++)); do
    nic="${EMULATED_PF}v${vf_index}"
    tap="${nic}_t"
    mac=$(printf "52:54:00:ab:cd:%02x" "${slot}")

    ip tuntap add dev "${tap}" mode tap
    ip link set "${tap}" master "${EMULATED_PF_BRIDGE}"
    ip link set "${tap}" up

    # grout drives the trunk VF through vfio, so the NIC must translate DMA
    # through the vIOMMU, see the intel-iommu device in run_vm.sh.
    QEMU_NIC_ARGS+=(
        -device "pcie-root-port,id=rp${slot},slot=${slot}"
        -netdev "tap,id=${tap},ifname=${tap},script=no,downscript=no"
        -device "virtio-net-pci,bus=rp${slot},netdev=${tap},mac=${mac},iommu_platform=on,ats=on"
    )
    echo "SUBSYSTEM==\"net\", ACTION==\"add\", ATTR{address}==\"${mac}\", NAME=\"${nic}\"" >> "${UDEV_RULES}"
    echo "Emulated VF ${vf_index} of ${EMULATED_PF}: ${nic} <-> ${tap} (mac ${mac})"

    slot=$((slot + 1))
done

for vf_index in 0 1; do
    tap="${EMULATED_PF}v${vf_index}_t"
    bridge vlan del vid 1 dev "${tap}"
    bridge vlan add vid 33 dev "${tap}"
    bridge vlan add vid 44 dev "${tap}"
done

for vf_index in 2 3; do
    tap="${EMULATED_PF}v${vf_index}_t"
    bridge vlan del vid 1 dev "${tap}"
    bridge vlan add vid 33 dev "${tap}" pvid untagged
done

for vf_index in 4 5; do
    tap="${EMULATED_PF}v${vf_index}_t"
    bridge vlan del vid 1 dev "${tap}"
    bridge vlan add vid 44 dev "${tap}" pvid untagged
done
