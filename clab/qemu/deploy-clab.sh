#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# Deploys the containerlab topology for the QEMU E2E environment.
# Runs the subset of clab/setup.sh that applies without a kind cluster:
#   02-leaf-configs, 04-containerlab-deploy, 08-ip-assignment, 09-container-setup
# vm/setup.sh runs from the host after the topology is deployed.
# Kind-only steps are skipped (00-environment, 01-registry, 03-kind-configs,
# 05-load-images, 06-kubeconfig, 07-frr-k8s, 10-veth-monitoring); the QEMU VM
# handles k3s, FRR-k8s, Multus, and PE underlay IPs in vm/setup.sh.
# Idempotent for topology deploy; IP assignment is re-run if clab is already up.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CLAB_DIR="${SCRIPT_DIR}/.."

# shellcheck disable=SC1091
source "${CLAB_DIR}/common.sh"

if ${CONTAINER_ENGINE_CLI} container inspect clab-kind-pe-kind-control-plane >/dev/null 2>&1; then
    if ! ${CONTAINER_ENGINE_CLI} container inspect clab-kind-pe-kind-worker >/dev/null 2>&1; then
        echo "ERROR: Existing kind lab has no QEMU worker. Clean it before deploying the two-node lab." >&2
        exit 1
    fi
    echo "Containerlab topology 'kind' is already deployed, skipping deploy."
else
    # Containerlab reconfigures by lab name. Other topologies also use 'kind'.
    kind_containers=$(${CONTAINER_ENGINE_CLI} ps -aq --filter label=containerlab=kind)
    for container in ${kind_containers}; do
        topology=$(${CONTAINER_ENGINE_CLI} container inspect \
            --format '{{index .Config.Labels "clab-topo-file"}}' "${container}")
        if [[ "${topology}" != "${SCRIPT_DIR}/kind.clab.yml" ]]; then
            echo "ERROR: Containerlab name 'kind' is already in use by ${topology}." >&2
            exit 1
        fi
    done

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

"${SCRIPT_DIR}/vm/setup.sh"
