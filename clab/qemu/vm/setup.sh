#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# Bootstraps k3s, FRR-k8s, Multus, and the required CNI plugins in the VM.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# shellcheck disable=SC1091
source "${SCRIPT_DIR}/qemu-common.sh"

readonly SSH_WAIT_SECONDS=300
readonly K3S_VERSION="${K3S_VERSION:-v1.36.4+k3s1}"
readonly MULTUS_VERSION="${MULTUS_VERSION:-v4.2.1}"
readonly CNI_PLUGINS_VERSION="${CNI_PLUGINS_VERSION:-v1.9.2-0.20260803142000-012159164d7f}"
readonly K8S_PORT="${QEMU_K8S_PORT:-6443}"


apt-get update
DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends openssh-client ca-certificates git

curl -Lo /usr/local/bin/kubectl https://dl.k8s.io/release/v1.37.0/bin/linux/amd64/kubectl
chmod +x /usr/local/bin/kubectl


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

echo "=== Bootstrapping QEMU VM cluster ==="

chmod 600 "${SSH_KEY}"
wait_for_ssh "to become SSH-reachable"

echo "Waiting for cloud-init to complete..."
# Exit code 2 means "done with recoverable warnings" (e.g. hostnamectl failing
# because systemd-hostnamed isn't up yet during early boot). The hostname is
# still set correctly via /etc/hostname, so treat it as success.
ssh_vm "sudo cloud-init status --wait --long" || {
    rc=$?
    if [[ $rc -ne 2 ]]; then
        exit $rc
    fi
    echo "cloud-init completed with recoverable warnings (exit code 2), continuing."
}

# cloud-init adds the IOMMU kernel arguments and persistent NIC names. They take
# effect only after this first reboot.
echo "Rebooting the VM to apply its kernel and NIC configuration..."
ssh_vm "sudo reboot" || true
sleep 10
wait_for_ssh "to return after reboot"

echo "Configuring VM underlay interfaces..."
ssh_vm sudo bash -s <<'EOF'
set -euo pipefail

configure_interface() {
    local interface=$1
    local ipv4_address=$2
    local ipv6_address=$3

    nmcli device set "${interface}" managed no 2>/dev/null || true
    ip address replace "${ipv4_address}" dev "${interface}"
    ip -6 address replace "${ipv6_address}" dev "${interface}"
    ip link set "${interface}" up
}

configure_interface toswitch1 192.168.11.3/24 2001:db8:11::3/64
configure_interface toswitch2 192.168.12.3/24 2001:db8:12::3/64
EOF

echo "Installing k3s ${K3S_VERSION}..."
ssh_vm "curl -sfL https://get.k3s.io | INSTALL_K3S_VERSION='${K3S_VERSION}' INSTALL_K3S_EXEC='--disable=traefik' K3S_KUBECONFIG_MODE=644 sh -"

echo "Waiting for the k3s node to become ready..."
for attempt in $(seq 1 60); do
    if ssh_vm "sudo k3s kubectl get nodes" 2>/dev/null | grep -q " Ready"; then
        break
    fi
    if ((attempt == 60)); then
        echo "ERROR: k3s did not become ready within 300s." >&2
        exit 1
    fi
    sleep 5
done

echo "Writing kubeconfig to ${KUBECONFIG_PATH}..."
mkdir -p "$(dirname "${KUBECONFIG_PATH}")"
ssh_vm "sudo cat /etc/rancher/k3s/k3s.yaml" \
    | sed "s|https://127.0.0.1:6443|https://127.0.0.1:${K8S_PORT}|" \
    > "${KUBECONFIG_PATH}"

export KUBECONFIG="${KUBECONFIG_PATH}"
KUBECTL="${KUBECTL:-kubectl}"

echo "Deploying FRR-k8s..."
"${KUBECTL}" apply -k "/frr-k8s-manifests"

echo "Installing CNI plugins in the VM..."
ssh_vm sudo bash -s -- "${CNI_PLUGINS_VERSION}" <<'EOF'
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

echo "Deploying Multus ${MULTUS_VERSION}..."
"${KUBECTL}" apply -f "https://raw.githubusercontent.com/k8snetworkplumbingwg/multus-cni/refs/tags/${MULTUS_VERSION}/deployments/multus-daemonset.yml"

echo "Waiting for FRR-k8s and Multus..."
"${KUBECTL}" -n frr-k8s-system wait --for=condition=Ready --all pods --timeout=300s
"${KUBECTL}" -n kube-system wait --for=condition=Ready pods -l name=multus --timeout=300s

echo "=== QEMU VM cluster bootstrap complete ==="
