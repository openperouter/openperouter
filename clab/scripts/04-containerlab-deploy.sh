#!/bin/bash
# Deploy containerlab topology
set -euo pipefail
set -x

source "$(dirname $(readlink -f $0))/../common.sh"

deploy_containerlab() {
    echo "Deploying containerlab topology..."

    pushd "$(dirname $(readlink -f $0))/.."

    if [[ $CONTAINER_ENGINE == "docker" ]]; then
        docker run --rm --privileged \
            --network host \
            -v /var/run/docker.sock:/var/run/docker.sock \
            -v /var/run/netns:/var/run/netns \
            -v /etc/hosts:/etc/hosts \
            -v /var/lib/docker/containers:/var/lib/docker/containers \
            --pid="host" \
            -v $(pwd):$(pwd) \
            -w $(pwd) \
            "ghcr.io/srl-labs/clab:$CLAB_VERSION" /usr/bin/clab deploy --reconfigure --topo "$CLAB_TOPOLOGY"
    else
        # We weren't able to run clab with podman in podman, installing it and running it
        # from the host.
        if ! command -v clab >/dev/null 2>&1; then
            echo "Clab is not installed, please install it first following https://containerlab.dev/install/"
            exit 1
        fi
        sudo clab deploy --reconfigure --topo $CLAB_TOPOLOGY $RUNTIME_OPTION
    fi

    popd
}

deploy_containerlab
