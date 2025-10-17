#!/bin/bash
# Container registry setup
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

setup_registry() {
    echo "Setting up container registry..."

    # Create registry container unless it already exists
    running="$($CONTAINER_ENGINE inspect -f '{{.State.Running}}' "kind-registry" 2>/dev/null || true)"
    if [ "${running}" != 'true' ]; then
        echo "Creating kind-registry container"
        $CONTAINER_ENGINE run \
            -d --restart=always -p "5000:5000" --name "kind-registry" \
            registry:2
    else
        echo "Registry container already running"
    fi
}

connect_registry_to_networks() {
    echo "Connecting registry to kind networks..."

    # Connect to the default kind network for compatibility
    $CONTAINER_ENGINE network connect "kind" "kind-registry" || true
}

apply_registry_config() {
    echo "Applying registry configuration to clusters: ${CLUSTER_NAMES[*]}"

    for cluster_name in "${CLUSTER_NAMES[@]}"; do
        echo "Applying registry config to cluster ${cluster_name}"

        # Determine kubeconfig path
        if [[ ${#CLUSTER_NAMES[@]} -eq 1 ]]; then
            KUBECONFIG_PATH_CLUSTER="${KUBECONFIG_PATH}"
        else
            KUBECONFIG_PATH_CLUSTER="${KUBECONFIG_PATH}-${cluster_name}"
        fi

        if [[ -f "${KUBECONFIG_PATH_CLUSTER}" ]]; then
            export KUBECONFIG="${KUBECONFIG_PATH_CLUSTER}"
            kubectl apply -f "$(dirname $(readlink -f $0))/../kind-registry_configmap.yaml"
        else
            echo "Warning: Kubeconfig not found at ${KUBECONFIG_PATH_CLUSTER}"
        fi
    done
}

setup_registry
connect_registry_to_networks
# Note: apply_registry_config is now called after cluster creation in 07-frr-k8s-setup.sh
