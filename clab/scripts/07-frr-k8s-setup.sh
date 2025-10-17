#!/bin/bash
# Setup frr-k8s on clusters
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

setup_frr_k8s() {
    echo "Setting up frr-k8s on clusters: ${CLUSTER_NAMES[*]}"

    # Run frr-k8s setup script only if CALICO_MODE is false
    if [[ "${CALICO_MODE:-false}" != "true" ]]; then
        for cluster_name in "${CLUSTER_NAMES[@]}"; do
            echo "Setting up frr-k8s on cluster ${cluster_name}"

            if [[ ${#CLUSTER_NAMES[@]} -eq 1 ]]; then
                cluster_kubeconfig="${KUBECONFIG_PATH}"
            else
                cluster_kubeconfig="${KUBECONFIG_PATH}-${cluster_name}"
            fi

            if [[ -f "$cluster_kubeconfig" ]]; then
                export KUBECONFIG="$cluster_kubeconfig"
                export KIND_CLUSTER_NAME="${cluster_name}"

                pushd "$(dirname $(readlink -f $0))/../kind/frr-k8s"
                KIND_CLUSTER_NAME=${cluster_name} ./setup.sh
                popd
            else
                echo "Warning: Kubeconfig for cluster ${cluster_name} not found at $cluster_kubeconfig"
            fi
        done
    else
        echo "Skipping frr-k8s setup (CALICO_MODE is enabled)"
    fi
}

# Apply registry configuration after clusters are created
source "$(dirname $(readlink -f $0))/01-registry.sh"
apply_registry_config "${CLUSTER_NAMES[@]}"

setup_frr_k8s