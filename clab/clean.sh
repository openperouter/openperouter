#!/bin/bash
set -euo pipefail

pushd "$(dirname $(readlink -f $0))"
source common.sh

CLAB_TOPOLOGY="${CLAB_TOPOLOGY:-singlecluster/kind.clab.yml}"

if [[ $# -gt 0 ]]; then
    CLUSTER_ARRAY=("$@")
elif [[ -n "${CLUSTER_NAMES:-}" ]]; then
    read -ra CLUSTER_ARRAY <<< "$CLUSTER_NAMES"
else
    CLUSTER_ARRAY=("pe-kind")
fi

# Initialize kubeconfig files array based on cluster mode
if [[ ${#CLUSTER_ARRAY[@]} -gt 1 ]]; then
    CLUSTER_MODE="multi"
    KUBECONFIG_FILES=()
    for cluster_name in "${CLUSTER_ARRAY[@]}"; do
        KUBECONFIG_FILES+=("${KUBECONFIG_PATH}-${cluster_name}")
    done
    KUBECONFIG_FILES+=("${KUBECONFIG_PATH}")
else
    CLUSTER_MODE="single"
    KUBECONFIG_FILES=("${KUBECONFIG_PATH}")
fi

echo "=== Starting ${CLUSTER_MODE} cluster cleanup ==="
echo "CLAB_TOPOLOGY: $CLAB_TOPOLOGY"
echo "CONTAINER_ENGINE: $CONTAINER_ENGINE"
echo "CLUSTER_NAMES: ${CLUSTER_ARRAY[*]}"

echo "=== Destroying containerlab topology ==="
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
        ghcr.io/srl-labs/clab:${CLAB_VERSION} /usr/bin/clab destroy --cleanup --topo $CLAB_TOPOLOGY || true
else
    # Use clab from the host with podman
    if ! command -v clab >/dev/null 2>&1; then
        echo "Warning: clab is not installed, skipping containerlab cleanup"
    else
        sudo clab destroy --cleanup --topo $CLAB_TOPOLOGY $RUNTIME_OPTION || true
    fi
fi

echo "=== Deleting kind cluster(s) ==="
for cluster_name in "${CLUSTER_ARRAY[@]}"; do
    echo "Deleting cluster: ${cluster_name}"
    ${KIND_COMMAND} delete cluster --name ${cluster_name} || true
done

echo "=== Cleaning up bridge interfaces ==="
for iface in $(ip link show | awk -F': ' '/^[0-9]+: leafkind/ {print $2}' | cut -d'@' -f1); do
    if [[ -d "/sys/class/net/${iface}" ]]; then
        echo "Removing bridge: ${iface}"
        sudo ip link delete ${iface} 2>/dev/null || true
    fi
done

echo "=== Cleaning up kubeconfig files ==="
for kubeconfig_file in "${KUBECONFIG_FILES[@]}"; do
    rm -f "${kubeconfig_file}" || true
    echo "Removed: ${kubeconfig_file}"
done

echo "=== Cleaning up local registry ==="
${CONTAINER_ENGINE_CLI} stop kind-registry 2>/dev/null || true
${CONTAINER_ENGINE_CLI} rm kind-registry 2>/dev/null || true

echo "=== ${CLUSTER_MODE^} cluster cleanup completed ==="
echo "All resources have been cleaned up"

popd
