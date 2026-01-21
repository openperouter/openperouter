#!/bin/bash
set -euo pipefail

pushd "$(dirname $(readlink -f $0))"
source common.sh

if [[ "${CLAB_TOPOLOGY}" == "" ]]; then 
	echo "missing CLAB_TOPOLOGY env var to clean"
	exit 1
fi

if [[ $# -lt 1 ]]; then
	echo "missing cluster name arg to clean"
	exit 1
fi

CLUSTER_ARRAY=("$@")

echo "=== Starting cluster cleanup ==="
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
rm -f "${KUBECONFIG_PATH}*" || true

echo "=== Cleaning up local registry ==="
${CONTAINER_ENGINE_CLI} stop kind-registry 2>/dev/null || true
${CONTAINER_ENGINE_CLI} rm kind-registry 2>/dev/null || true

echo "=== cluster cleanup completed ==="
echo "All resources have been cleaned up"

popd
