#!/bin/bash
# Deploy containerlab topology
set -euo pipefail
set -x

SCRIPT_DIR="$(dirname "$(readlink -f "$0")")"
REPO_DIR="$(readlink -f "$SCRIPT_DIR/../..")"
source "${SCRIPT_DIR}/../common.sh"

deploy_containerlab() {
    echo "Deploying containerlab topology..."

    pushd "${SCRIPT_DIR}/.."

    if [[ $CONTAINER_ENGINE == "docker" ]]; then
        docker run --rm --privileged \
            --network host \
            -e "QEMU_SSH_PORT=${QEMU_SSH_PORT:-}" \
            -e "QEMU_K8S_PORT=${QEMU_K8S_PORT:-}" \
            -v /var/run/docker.sock:/var/run/docker.sock \
            -v /var/run/netns:/var/run/netns \
            -v /etc/hosts:/etc/hosts \
            -v /var/lib/docker/containers:/var/lib/docker/containers \
            --pid="host" \
            -v "$REPO_DIR:$REPO_DIR" \
            -w "$REPO_DIR/clab" \
            "ghcr.io/srl-labs/clab:$CLAB_VERSION" /usr/bin/clab deploy --reconfigure --topo "$CLAB_TOPOLOGY"
    else
        # We weren't able to run clab with podman in podman, installing it and running it
        # from the host.
        if ! command -v clab >/dev/null 2>&1; then
            echo "Clab is not installed, please install it first following https://containerlab.dev/install/"
            exit 1
        fi
        sudo env \
            "QEMU_SSH_PORT=${QEMU_SSH_PORT:-}" \
            "QEMU_K8S_PORT=${QEMU_K8S_PORT:-}" \
            clab deploy --reconfigure --topo $CLAB_TOPOLOGY $RUNTIME_OPTION
    fi
    # The containerlab setup will set the `default` and `all` rp_filter to 0:
    # https://github.com/srl-labs/containerlab/blob/d682499c5ce9854da85cde7bad02db375ff46308/runtime/docker/docker.go#L714
    # Even though each docker container also has a `default` rp_filter setting, new namespaces
    # created from within the container such as the `perouter` namespace inherit directly from
    # this setting on the host and not from the setting as seen inside the docker container.
    sudo sysctl -w net.ipv4.conf.default.rp_filter=1
    sudo sysctl -w net.ipv4.conf.all.rp_filter=1

    popd
}

deploy_containerlab
