#!/bin/bash
# Deploy containerlab topology
set -euo pipefail
set -x

source "$(dirname $(readlink -f $0))/../common.sh"

deploy_containerlab() {
    echo "Deploying containerlab topology..."

    pushd "$(dirname $(readlink -f $0))/.."

    # Pre-deployment diagnostics
    echo "=== Pre-deployment diagnostics ==="
    echo "Available Docker images:"
    docker images | grep -E "(kind-node|openperouter)" || echo "No kind-node or openperouter images found"

    echo ""
    echo "Existing Docker containers:"
    docker ps -a | grep -E "(kind|pe-)" || echo "No existing kind containers"

    echo ""
    echo "Docker info:"
    docker info | grep -E "(Server Version|Storage Driver|Logging Driver)"

    # Check if NODE_IMAGE is set and verify it exists
    if [[ -n "${NODE_IMAGE:-}" ]]; then
        echo ""
        echo "=== Verifying NODE_IMAGE: ${NODE_IMAGE} ==="
        if docker images "${NODE_IMAGE}" --format "{{.Repository}}:{{.Tag}}" | grep -q "${NODE_IMAGE}"; then
            echo "✓ Image ${NODE_IMAGE} exists locally"
            docker images "${NODE_IMAGE}"
        else
            echo "Image not found locally, attempting to pull..."
            if docker pull "${NODE_IMAGE}"; then
                echo "✓ Successfully pulled ${NODE_IMAGE}"
                docker images "${NODE_IMAGE}"
            else
                echo "ERROR: Image ${NODE_IMAGE} does not exist locally and pull failed"
                echo "Please build the image locally or ensure it's available in the registry"
                exit 1
            fi
        fi
    else
        echo ""
        echo "WARNING: NODE_IMAGE environment variable is not set, using default kind node image"
    fi

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
            ghcr.io/srl-labs/clab:0.67.0 /usr/bin/clab deploy --reconfigure --topo $CLAB_TOPOLOGY

        CLAB_EXIT_CODE=$?
        echo "Containerlab exit code: $CLAB_EXIT_CODE"
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

    if [[ $CLAB_EXIT_CODE -ne 0 ]]; then
        echo "Containerlab deployment failed with exit code $CLAB_EXIT_CODE"
        exit $CLAB_EXIT_CODE
    fi
}

deploy_containerlab
