#!/bin/bash
# Generate kind configuration files
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

generate_kind_configs() {
    echo "Generating kind configuration files..."

    pushd "$(dirname $(readlink -f $0))/../tools"

    # Generate kind-configuration-registry.yaml from template
    KIND_CONFIG_ARGS=""
    if [[ "${CALICO_MODE:-false}" == "true" ]]; then
        KIND_CONFIG_ARGS=" -disable-default-cni"
        echo "Disabling default CNI in kind configuration files (CALICO_MODE)"
    fi

    for cluster_name in "${CLUSTER_NAMES[@]}"; do
        KIND_CONFIG_NAME="${cluster_name}-configuration-registry.yaml"

        echo "Generating kind configuration for cluster name: ${cluster_name}; KIND_CONFIG_NAME: ${KIND_CONFIG_NAME}"
        go run generate_kind_config/generate_kind_config.go \
            --template generate_kind_config/kind_template/kind-configuration-registry.yaml.template \
            $KIND_CONFIG_ARGS -cluster-name cluster_name -output ../$KIND_CONFIG_NAME $KIND_CONFIG_NAME
    done

    popd
}

setup_calico_if_enabled() {
    if [[ "${CALICO_MODE:-false}" == "true" ]]; then
        echo "Setting up Calico..."
        pushd "$(dirname $(readlink -f $0))/../calico"
        ./apply_calico.sh & # required as clab will stop earlier because the cni is not ready
        popd
    fi
}

generate_kind_configs
setup_calico_if_enabled