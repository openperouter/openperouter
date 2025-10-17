#!/bin/bash
# Setup kubeconfig files for clusters
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

setup_kubeconfigs() {
    echo "Setting up kubeconfig files for clusters: ${CLUSTER_NAMES[*]}"

    for cluster_name in "${CLUSTER_NAMES[@]}"; do
        echo "Setting up kubeconfig for cluster ${cluster_name}"

        if [[ ${#CLUSTER_NAMES[@]} -eq 1 ]]; then
            cluster_kubeconfig="${KUBECONFIG_PATH}"
        else
            cluster_kubeconfig="${KUBECONFIG_PATH}-${cluster_name}"
        fi

        ${KIND_COMMAND} --name "${cluster_name}" get kubeconfig > "$cluster_kubeconfig"
        echo "Kubeconfig for ${cluster_name} saved to $cluster_kubeconfig"
    done

    # For multi-cluster, set default kubeconfig to first cluster for compatibility
    if [[ ${#CLUSTER_NAMES[@]} -gt 1 ]]; then
        first_cluster="${CLUSTER_NAMES[0]}"
        cp "${KUBECONFIG_PATH}-${first_cluster}" "$KUBECONFIG_PATH"
        echo "Default kubeconfig set to cluster ${first_cluster}"
    fi

    export KUBECONFIG="$KUBECONFIG_PATH"
}

setup_kubeconfigs