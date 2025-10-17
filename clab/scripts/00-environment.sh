#!/bin/bash
# Environment setup and validation
set -euo pipefail

source "$(dirname $(readlink -f $0))/../common.sh"

# Get cluster names from arguments
CLUSTER_NAMES=("$@")

if [[ ${#CLUSTER_NAMES[@]} -eq 0 ]]; then
    echo "Usage: $0 <cluster_name> [cluster_name2] ..."
    echo "Example: $0 pe-kind"
    echo "Example: $0 pe-kind-a pe-kind-b"
    exit 1
fi

# Check prerequisites
check_prerequisites() {
    echo "Checking prerequisites..."

    # Check if containerlab is available
    if [[ $CONTAINER_ENGINE == "docker" ]]; then
        if ! command -v docker >/dev/null 2>&1; then
            echo "Docker is not available"
            exit 1
        fi
    else
        if ! command -v clab >/dev/null 2>&1; then
            echo "Clab is not installed, please install it first following https://containerlab.dev/install/"
            exit 1
        fi
    fi

    # Check if kind is available
    if ! command -v $KIND >/dev/null 2>&1; then
        echo "Kind is not available at $KIND"
        exit 1
    fi

    echo "Prerequisites check passed"
}

# Create bridge interfaces for the provided clusters
create_bridges() {
    echo "Creating bridge interfaces for clusters: ${CLUSTER_NAMES[*]}"

    for cluster_name in "${CLUSTER_NAMES[@]}"; do
        # For single cluster, use the traditional bridge name
        if [[ ${#CLUSTER_NAMES[@]} -eq 1 && "$cluster_name" == "pe-kind" ]]; then
            bridge_name="leafkind-switch"  # 15 chars exactly
        else
            # Use cluster suffix to keep bridge name short (under 15 chars)
            # Extract last part after final dash (e.g., pe-kind-a -> a)
            suffix="${cluster_name##*-}"
            bridge_name="leafkind-sw-${suffix}"  # e.g., leafkind-sw-a (13 chars)
        fi

        if [[ ! -d "/sys/class/net/${bridge_name}" ]]; then
            echo "Creating bridge ${bridge_name} for cluster ${cluster_name}"
            sudo ip link add name ${bridge_name} type bridge
        fi

        if [[ $(cat /sys/class/net/${bridge_name}/operstate) != "up" ]]; then
            echo "Bringing up bridge ${bridge_name}"
            sudo ip link set dev ${bridge_name} up
        fi
    done

    echo "Bridge interfaces created"
}

check_prerequisites
create_bridges
