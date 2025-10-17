#!/bin/bash
# Execute setup scripts in containers
set -euo pipefail
set -x

source "$(dirname $(readlink -f $0))/../common.sh"

# Get cluster names from arguments
CLUSTER_NAMES=("$@")

if [[ ${#CLUSTER_NAMES[@]} -eq 0 ]]; then
    echo "Usage: $0 <cluster_name> [cluster_name2] ..."
    echo "Example: $0 pe-kind"
    echo "Example: $0 pe-kind-a pe-kind-b"
    exit 1
fi

setup_containers() {
    echo "Executing setup scripts in containers for clusters: ${CLUSTER_NAMES[*]}"

    # Setup common leaf containers (always present)
    ${CONTAINER_ENGINE_CLI} exec clab-kind-leafA /setup.sh
    ${CONTAINER_ENGINE_CLI} exec clab-kind-leafB /setup.sh

    # Setup host containers (always present)
    ${CONTAINER_ENGINE_CLI} exec clab-kind-hostA_red /setup.sh
    ${CONTAINER_ENGINE_CLI} exec clab-kind-hostA_blue /setup.sh
    ${CONTAINER_ENGINE_CLI} exec clab-kind-hostA_default /setup.sh
    ${CONTAINER_ENGINE_CLI} exec clab-kind-hostB_red /setup.sh
    ${CONTAINER_ENGINE_CLI} exec clab-kind-hostB_blue /setup.sh

    # Setup cluster-specific leaf containers
    for cluster_name in "${CLUSTER_NAMES[@]}"; do
        if [[ ${#CLUSTER_NAMES[@]} -eq 1 ]]; then
            # Single cluster mode - try leafkind container
            if ${CONTAINER_ENGINE_CLI} exec clab-kind-leafkind test -f /setup.sh 2>/dev/null; then
                echo "Setting up leafkind container"
                ${CONTAINER_ENGINE_CLI} exec clab-kind-leafkind /setup.sh
            fi
        else
            # Multi-cluster mode - try cluster-specific leafkind containers
            cluster_suffix="${cluster_name##*-}"
            container_name="clab-kind-leafkind-${cluster_suffix}"
            if ${CONTAINER_ENGINE_CLI} exec ${container_name} test -f /setup.sh 2>/dev/null; then
                echo "Setting up ${container_name} container"
                ${CONTAINER_ENGINE_CLI} exec ${container_name} /setup.sh
            fi
        fi
    done
}

setup_containers