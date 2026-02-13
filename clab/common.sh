#!/bin/bash
set -euo pipefail


CONTAINER_ENGINE=${CONTAINER_ENGINE:-"docker"}
CONTAINER_ENGINE_CLI="docker"
KUBECONFIG_PATH=${KUBECONFIG_PATH:-"$(pwd)/kubeconfig"}
KIND=${KIND:-"kind"}
CLAB_VERSION="0.67.0"

KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME:-pe-kind}"

RUNTIME_OPTION=""
KIND_COMMAND=$KIND

if [[ $CONTAINER_ENGINE == "podman" ]]; then
    RUNTIME_OPTION="--runtime podman"
    CONTAINER_ENGINE_CLI="sudo podman"
    KIND_COMMAND="sudo KIND_EXPERIMENTAL_PROVIDER=podman $KIND"
    if ! systemctl is-enabled --quiet podman.socket || ! systemctl is-active --quiet podman.socket; then
        echo "Enabling and starting podman.socket service..."
        sudo systemctl enable podman.socket
        sudo systemctl start podman.socket
    fi
fi

load_image_to_podman_on_nodes() {
    local image_tag=$1
    local file_name=$2
    local temp_file="/tmp/${file_name}.tar"

    # Load to podman on each node
    local nodes=$(${KIND_COMMAND} get nodes --name ${KIND_CLUSTER_NAME})
    for node in $nodes; do
        echo "Copying ${image_tag} to node: $node"
        ${CONTAINER_ENGINE_CLI} cp ${temp_file} ${node}:/root/${file_name}.tar

        echo "Loading image into podman on node: $node"
        ${CONTAINER_ENGINE_CLI} exec ${node} podman load -i /root/${file_name}.tar
        ${CONTAINER_ENGINE_CLI} exec ${node} rm /root/${file_name}.tar
    done
}

load_local_image_to_kind() {
    local image_tag=$1
    local file_name=$2
    local temp_file="/tmp/${file_name}.tar"
    sudo rm -f ${temp_file} || true
    # Use kind load docker-image to avoid multi-platform manifest issues
    ${KIND_COMMAND} load docker-image ${image_tag} --name ${KIND_CLUSTER_NAME}
    # Save image for podman loading on nodes
    ${CONTAINER_ENGINE_CLI} save -o ${temp_file} ${image_tag}
    load_image_to_podman_on_nodes ${image_tag} ${file_name}
}

load_image_to_kind() {
    local image_tag=$1
    local file_name=$2
    # Explicitly pull single platform to avoid multi-platform manifest issues with kind load
    ${CONTAINER_ENGINE_CLI} image pull --platform linux/amd64 ${image_tag}
    load_local_image_to_kind ${image_tag} ${file_name}
}
