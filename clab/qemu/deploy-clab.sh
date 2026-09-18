#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# Deploys the containerlab topology for the QEMU E2E environment.
# Runs the subset of clab/setup.sh that applies without a kind cluster:
#   02-leaf-configs, 04-containerlab-deploy, 08-ip-assignment, 09-container-setup
# The topology's exec field runs vm/setup.sh after the QEMU node starts.
# Kind-only steps are skipped (00-environment, 01-registry, 03-kind-configs,
# 05-load-images, 06-kubeconfig, 07-frr-k8s, 10-veth-monitoring); the QEMU VM
# handles k3s, FRR-k8s, Multus, and PE underlay IPs in vm/setup.sh.
# Idempotent for topology deploy; IP assignment is re-run if clab is already up.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CLAB_DIR="${SCRIPT_DIR}/.."
CLAB_NAME="${CLAB_NAME:-kind}"

# shellcheck disable=SC1091
source "${CLAB_DIR}/common.sh"

if sudo containerlab inspect --name "${CLAB_NAME}" &>/dev/null 2>&1; then
    echo "Containerlab topology '${CLAB_NAME}' is already deployed, skipping deploy."
else
    # containerlab's bridge nodes attach to pre-existing Linux bridges; unlike
    # regular nodes, containerlab does not create these interfaces for us.
    for bridge in leafkind1-sw leafkind2-sw; do
        if [[ ! -d "/sys/class/net/${bridge}" ]]; then
            echo "Creating bridge ${bridge}"
            sudo ip link add name "${bridge}" type bridge
        fi
        sudo ip link set dev "${bridge}" up
    done

    # Generate leaf FRR configs
    "${CLAB_DIR}/scripts/02-leaf-configs.sh"

    # Deploy containerlab topology
    export CLAB_TOPOLOGY="qemu/kind.clab.yml"
    "${CLAB_DIR}/scripts/04-containerlab-deploy.sh"
fi

# Assign fabric IPs to clab containers (PE IPs come from vm/setup.sh).
# Re-run even when the topology is already up so a previous qemu deploy
# that skipped this step still gets leafkind1 192.168.11.2/24.
echo "=== IP assignment ==="
IP_MAP_FILE=qemu/ip_map.txt "${CLAB_DIR}/scripts/08-ip-assignment.sh" pe-kind

echo "=== Container setup ==="
"${CLAB_DIR}/scripts/09-container-setup.sh" pe-kind
