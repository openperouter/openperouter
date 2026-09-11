#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# Collects diagnostic logs from the QEMU VM and clab FRR containers.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="${SCRIPT_DIR}/../../.."

# shellcheck disable=SC1091
source "${SCRIPT_DIR}/qemu-common.sh"
CLAB_NAME="${CLAB_NAME:-kind}"
LOG_DIR="${KIND_EXPORT_LOGS:-/tmp/kind_logs}"
KUBECONFIG="${KUBECONFIG_PATH:-${REPO_ROOT}/bin/kubeconfig}"
QEMU_CONTAINER="clab-${CLAB_NAME}-pe-kind-control-plane"

mkdir -p "${LOG_DIR}/qemu-vm"

echo "Collecting QEMU VM logs..."

# VM serial console log (inside the clab container)
if docker inspect "${QEMU_CONTAINER}" &>/dev/null; then
    docker cp "${QEMU_CONTAINER}:/var/log/serial.log" "${LOG_DIR}/qemu-vm/serial.log" 2>/dev/null || true
fi

# In-VM logs (best effort)
if ssh_vm true 2>/dev/null; then
    echo "  journalctl..."
    ssh_vm "sudo journalctl --no-pager -l" > "${LOG_DIR}/qemu-vm/journalctl.log" 2>/dev/null || true

    echo "  k3s logs..."
    ssh_vm "sudo journalctl -u k3s --no-pager -l" > "${LOG_DIR}/qemu-vm/k3s.log" 2>/dev/null || true

    echo "  dmesg..."
    ssh_vm "sudo dmesg" > "${LOG_DIR}/qemu-vm/dmesg.log" 2>/dev/null || true

    echo "  PCI devices..."
    ssh_vm "sudo lspci -vvv" > "${LOG_DIR}/qemu-vm/lspci.log" 2>/dev/null || true
    ssh_vm "sudo lspci -k" > "${LOG_DIR}/qemu-vm/lspci-k.log" 2>/dev/null || true

    echo "  SR-IOV info..."
    # This script is intentionally single-quoted so expansion happens in the VM.
    # shellcheck disable=SC2016
    ssh_vm 'for d in /sys/class/net/*/device/sriov_numvfs; do
        iface=$(basename $(dirname $(dirname "$d")))
        echo "=== $iface ==="
        echo "sriov_numvfs: $(cat $d)"
        cat $(dirname $d)/sriov_totalvfs 2>/dev/null && echo "sriov_totalvfs: $(cat $(dirname $d)/sriov_totalvfs)" || true
    done' > "${LOG_DIR}/qemu-vm/sriov-info.log" 2>/dev/null || true

    echo "  ip addr/route..."
    ssh_vm "ip addr show" > "${LOG_DIR}/qemu-vm/ip-addr.log" 2>/dev/null || true
    ssh_vm "ip route show" > "${LOG_DIR}/qemu-vm/ip-route.log" 2>/dev/null || true

    # kubectl logs for openperouter pods
    if [[ -f "${KUBECONFIG}" ]]; then
        echo "  openperouter pod logs..."
        export KUBECONFIG
        KUBECTL="${KUBECTL:-kubectl}"
        for pod in $(${KUBECTL} -n openperouter-system get pods -o name 2>/dev/null || true); do
            pod_name=$(basename "${pod}")
            ${KUBECTL} -n openperouter-system logs "${pod}" --all-containers --ignore-errors \
                > "${LOG_DIR}/qemu-vm/pod-${pod_name}.log" 2>/dev/null || true
        done

        echo "  kubectl describe pods..."
        ${KUBECTL} -n openperouter-system describe pods \
            > "${LOG_DIR}/qemu-vm/describe-pods.log" 2>/dev/null || true

        echo "  cluster-info dump..."
        ${KUBECTL} cluster-info dump --output-directory="${LOG_DIR}/qemu-vm/cluster-state" \
            --all-namespaces 2>/dev/null || true
    fi
else
    echo "  WARNING: VM not reachable via SSH, skipping in-VM log collection."
fi

# Clab FRR container logs
echo "Collecting clab FRR logs..."
for node in leafkind1 leafkind2 spine leafA leafB leafSRV6; do
    container="clab-${CLAB_NAME}-${node}"
    if docker inspect "${container}" &>/dev/null; then
        docker logs "${container}" > "${LOG_DIR}/qemu-vm/${node}.log" 2>&1 || true
        docker exec "${container}" vtysh -c "show bgp summary" \
            > "${LOG_DIR}/qemu-vm/${node}-bgp-summary.log" 2>/dev/null || true
        docker exec "${container}" vtysh -c "show running-config" \
            > "${LOG_DIR}/qemu-vm/${node}-running-config.log" 2>/dev/null || true
    fi
done

echo "Logs collected in ${LOG_DIR}/qemu-vm/"
