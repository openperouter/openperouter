#!/bin/bash
# Load container images to kind clusters
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

load_images_to_clusters() {
    echo "Loading container images to clusters: ${CLUSTER_NAMES[*]}"

    # Define images to load
    local images=(
        "quay.io/frrouting/frr:9.1.0 frr9"
        "quay.io/frrouting/frr:10.2.1 frr10"
        "gcr.io/kubebuilder/kube-rbac-proxy:v0.13.1 rbacproxy"
        "quay.io/metallb/frr-k8s:v0.0.17 frrk8s"
    )

    for cluster_name in "${CLUSTER_NAMES[@]}"; do
        echo "Loading images to cluster ${cluster_name}"

        # Set cluster-specific variables
        export KIND_CLUSTER_NAME="${cluster_name}"

        # Load each image
        for image_info in "${images[@]}"; do
            local image_tag=$(echo $image_info | cut -d' ' -f1)
            local file_name=$(echo $image_info | cut -d' ' -f2)
            echo "Loading $image_tag to cluster $KIND_CLUSTER_NAME"

            if [[ ${#CLUSTER_NAMES[@]} -eq 1 ]]; then
                load_image_to_kind "$image_tag" "${file_name}"
            else
                load_image_to_kind "$image_tag" "${file_name}-${cluster_name}"
            fi
        done
    done
}

load_images_to_clusters