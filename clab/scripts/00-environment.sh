#!/bin/bash
# Environment setup and validation
set -euo pipefail

source "$(dirname $(readlink -f $0))/../common.sh"

# Get bridge names from arguments
BRIDGE_NAMES=("$@")

if [[ ${#BRIDGE_NAMES[@]} -eq 0 ]]; then
    echo "Usage: $0 <bridge_name> [bridge_name2] ..."
    echo "Example: $0 leafkind-switch"
    echo "Example: $0 leafkind-sw-a leafkind-sw-b"
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

# Create bridge interfaces with the provided names
create_bridges() {
    echo "Creating bridge interfaces: ${BRIDGE_NAMES[*]}"

    for bridge_name in "${BRIDGE_NAMES[@]}"; do
        if [[ ! -d "/sys/class/net/${bridge_name}" ]]; then
            echo "Creating bridge ${bridge_name}"
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
