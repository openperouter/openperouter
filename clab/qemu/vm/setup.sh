#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# Bootstraps k3s, FRR-k8s, Multus, and the required CNI plugins on both VMs.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# shellcheck disable=SC1091
source "${SCRIPT_DIR}/qemu-common.sh"

readonly SSH_WAIT_SECONDS=300
readonly K3S_VERSION="${K3S_VERSION:-v1.36.4+k3s1}"
readonly MULTUS_VERSION="${MULTUS_VERSION:-v4.2.1}"
readonly CNI_PLUGINS_VERSION="${CNI_PLUGINS_VERSION:-v1.9.2-0.20260803142000-012159164d7f}"
readonly K8S_PORT="${QEMU_K8S_PORT:-6443}"
readonly KUBECTL="${KUBECTL:-${SCRIPT_DIR}/../../../bin/kubectl}"
readonly CONTROL_PLANE_IP=172.30.250.1
readonly WORKER_IP=172.30.250.2
readonly CONTROL_PLANE=pe-kind-control-plane
readonly WORKER=pe-kind-worker
readonly CONTROL_PLANE_SSH_PORT="${QEMU_SSH_PORT}"

wait_for_ssh() {
    local description=$1
    local elapsed=0

    echo "Waiting for the VM ${description}..."
    until ssh_vm true 2>/dev/null; do
        if ((elapsed >= SSH_WAIT_SECONDS)); then
            echo "ERROR: VM was not SSH-reachable within ${SSH_WAIT_SECONDS}s." >&2
            return 1
        fi
        sleep 5
        elapsed=$((elapsed + 5))
        echo "  waited ${elapsed}s / ${SSH_WAIT_SECONDS}s"
    done
}

configure_guest() {
    local name=$1
    local cluster_ip=$2
    local cluster_ipv6=$3
    local underlay_host=$4
    local rc

    wait_for_ssh "${name} to become SSH-reachable"
    ssh_vm "sudo cloud-init status --wait --long" || {
        rc=$?
        # Cloud-init uses exit code 2 for recoverable warnings.
        if [[ $rc -ne 2 ]]; then
            return "$rc"
        fi
    }

    # The IOMMU arguments and persistent NIC names take effect after reboot.
    if ! ssh_vm "grep -qw intel_iommu=on /proc/cmdline"; then
        echo "Rebooting ${name} to apply its kernel and NIC configuration..."
        ssh_vm "sudo reboot" || true
        sleep 10
        wait_for_ssh "${name} to return after reboot"
    fi

    echo "Configuring ${name} network interfaces..."
    ssh_vm sudo bash -s -- "${name}" "${cluster_ip}" "${cluster_ipv6}" "${underlay_host}" <<'EOF'
set -euo pipefail
hostnamectl set-hostname "$1"
cluster_ip=$2
cluster_ipv6=$3
underlay_host=$4

configure_interface() {
    local interface=$1
    local ipv4_address=$2
    local ipv6_address=$3

    nmcli device set "${interface}" managed no 2>/dev/null || true
    ip address replace "${ipv4_address}" dev "${interface}"
    ip -6 address replace "${ipv6_address}" dev "${interface}"
    ip link set "${interface}" up
}

configure_interface cluster "${cluster_ip}/30" "${cluster_ipv6}/64"
configure_interface toswitch1 "192.168.11.${underlay_host}/24" "2001:db8:11::${underlay_host}/64"
configure_interface toswitch2 "192.168.12.${underlay_host}/24" "2001:db8:12::${underlay_host}/64"
EOF
}

echo "=== Bootstrapping QEMU VM cluster ==="
chmod 600 "${SSH_KEY}"

configure_guest "${CONTROL_PLANE}" "${CONTROL_PLANE_IP}" 2001:db8:250::1 3
echo "Installing k3s server ${K3S_VERSION}..."
ssh_vm "curl -sfL https://get.k3s.io | INSTALL_K3S_VERSION='${K3S_VERSION}' \
    K3S_KUBECONFIG_MODE=644 sh -s - server --disable=traefik --node-name='${CONTROL_PLANE}' \
    --node-ip='${CONTROL_PLANE_IP}' --advertise-address='${CONTROL_PLANE_IP}' --flannel-iface=cluster"

echo "Waiting for the k3s control plane..."
ssh_vm "sudo k3s kubectl wait --for=create node/${CONTROL_PLANE} --timeout=300s"
ssh_vm "sudo k3s kubectl wait --for=condition=Ready node/${CONTROL_PLANE} --timeout=300s"

agent_token=$(ssh_vm "sudo cat /var/lib/rancher/k3s/server/agent-token")
QEMU_SSH_PORT="${QEMU_WORKER_SSH_PORT}"
configure_guest "${WORKER}" "${WORKER_IP}" 2001:db8:250::2 4
echo "Installing k3s agent ${K3S_VERSION}..."
ssh_vm "curl -sfL https://get.k3s.io | INSTALL_K3S_VERSION='${K3S_VERSION}' \
    K3S_URL='https://${CONTROL_PLANE_IP}:6443' K3S_TOKEN='${agent_token}' \
    sh -s - agent --node-name='${WORKER}' --node-ip='${WORKER_IP}' --flannel-iface=cluster"

QEMU_SSH_PORT="${CONTROL_PLANE_SSH_PORT}"
echo "Waiting for the k3s worker..."
ssh_vm "sudo k3s kubectl wait --for=create node/${WORKER} --timeout=300s"
ssh_vm "sudo k3s kubectl wait --for=condition=Ready node/${WORKER} --timeout=300s"

echo "Writing kubeconfig to ${KUBECONFIG_PATH}..."
mkdir -p "$(dirname "${KUBECONFIG_PATH}")"
ssh_vm "sudo cat /etc/rancher/k3s/k3s.yaml" \
    | sed "s|https://127.0.0.1:6443|https://127.0.0.1:${K8S_PORT}|" \
    > "${KUBECONFIG_PATH}"

export KUBECONFIG="${KUBECONFIG_PATH}"

echo "Deploying FRR-k8s..."
"${KUBECTL}" apply -k "${SCRIPT_DIR}/../../kind/frr-k8s"

for port in "${CONTROL_PLANE_SSH_PORT}" "${QEMU_WORKER_SSH_PORT}"; do
    echo "Installing CNI plugins on the VM at SSH port ${port}..."
    QEMU_SSH_PORT="${port}" ssh_vm sudo bash -s -- "${CNI_PLUGINS_VERSION}" <<'EOF'
set -euo pipefail
cni_plugins_version=$1

dnf install -y golang
mkdir -p /etc/cni /opt/cni
ln -sfn /var/lib/rancher/k3s/agent/etc/cni/net.d /etc/cni/net.d
ln -sfn /var/lib/rancher/k3s/data/cni /opt/cni/bin
GOBIN=/opt/cni/bin go install "github.com/containernetworking/plugins/plugins/main/macvlan@${cni_plugins_version}"
GOBIN=/opt/cni/bin go install "github.com/containernetworking/plugins/plugins/ipam/static@${cni_plugins_version}"
# bridge plugin is already part of k3s networking stack
EOF
done

echo "Deploying Multus ${MULTUS_VERSION}..."
"${KUBECTL}" apply -f "https://raw.githubusercontent.com/k8snetworkplumbingwg/multus-cni/refs/tags/${MULTUS_VERSION}/deployments/multus-daemonset.yml"

echo "Waiting for FRR-k8s and Multus..."
"${KUBECTL}" -n frr-k8s-system rollout status daemonset/frr-k8s-daemon --timeout=300s
"${KUBECTL}" -n kube-system rollout status daemonset/kube-multus-ds --timeout=300s

echo "=== QEMU VM cluster bootstrap complete ==="
