#!/bin/bash
set -euo pipefail


export CONTAINER_ENGINE=${CONTAINER_ENGINE:-"docker"}
CONTAINER_ENGINE_CLI="docker"
export KUBECONFIG_PATH=${KUBECONFIG_PATH:-"$(pwd)/kubeconfig"}
export KIND=${KIND:-"kind"}
export KUBECTL=${KUBECTL:-"kubectl"}
CLAB_VERSION="0.74.1"
PID_FILE="tools/check_veths/bin/check_veths.pid"

export KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME:-pe-kind}"

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

PLATFORM="linux/$($(which go) env GOARCH)"

export CONTAINER_ENGINE_CLI

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
    local save_opts=()

    # When docker uses the containerd image store, "docker save" writes an OCI
    # archive whose index points at the whole multi-platform manifest list,
    # while only the platform we pulled has its blobs present locally. "kind
    # load image-archive" then runs "ctr images import --all-platforms", which
    # tries to resolve every referenced manifest and fails with
    #   ctr: content digest sha256:...: not found
    # Restricting the archive to the platform we actually pulled keeps the
    # index single-manifest and imports cleanly.
    if [[ $CONTAINER_ENGINE == "docker" ]] && docker save --help 2>&1 | grep -q -- '--platform'; then
        save_opts=(--platform "${PLATFORM}")
    fi

    rm -f ${temp_file}
    ${CONTAINER_ENGINE_CLI} save "${save_opts[@]}" -o ${temp_file} ${image_tag}
    ${KIND_COMMAND} load image-archive ${temp_file} --name ${KIND_CLUSTER_NAME}
    load_image_to_podman_on_nodes ${image_tag} ${file_name}
}

load_image_to_kind() {
    local image_tag=$1
    local file_name=$2
    ${CONTAINER_ENGINE_CLI} image pull --platform ${PLATFORM} ${image_tag}
    load_local_image_to_kind ${image_tag} ${file_name}
}
