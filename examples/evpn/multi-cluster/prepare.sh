#!/bin/bash
set -euo pipefail
set -x
CURRENT_PATH=$(dirname "$0")

source "${CURRENT_PATH}/../../common.sh"

DEMO_MODE=true make deploy-multi

# Function to install KubeVirt on a cluster
install_kubevirt() {
    local kubeconfig="$1"

    echo "Installing KubeVirt using kubeconfig: ${kubeconfig}"

    # Install KubeVirt
    KUBECONFIG="$kubeconfig" kubectl apply -f https://github.com/kubevirt/kubevirt/releases/download/v1.6.2/kubevirt-operator.yaml
    KUBECONFIG="$kubeconfig" kubectl apply -f https://github.com/kubevirt/kubevirt/releases/download/v1.6.2/kubevirt-cr.yaml

    # Patch KubeVirt to allow scheduling on control-planes, so we can test live migration between two nodes
    KUBECONFIG="$kubeconfig" kubectl patch -n kubevirt kubevirt kubevirt --type merge --patch '{"spec": {"workloads": {"nodePlacement": {"tolerations": [{"key": "node-role.kubernetes.io/control-plane", "operator": "Exists", "effect": "NoSchedule"}]}}}}'

    # Enable the decentralized live migration feature gate (requirement for cross cluster live migration)
    KUBECONFIG="$kubeconfig" kubectl patch -n kubevirt kubevirt kubevirt --type merge --patch '{"spec": {"configuration": {"developerConfiguration": {"featureGates": [ "DecentralizedLiveMigration" ]}}}}'

    # Wait for KubeVirt to be available
    KUBECONFIG="$kubeconfig" kubectl wait --for=condition=Available kubevirt/kubevirt -n kubevirt --timeout=10m
}

# Function to apply demo manifests
apply_demo_manifests() {
    local kubeconfig="$1"
    local manifests=("${@:2}")

    echo "Applying demo manifests using kubeconfig: ${kubeconfig}"

    # Apply cluster-specific manifests
    export KUBECONFIG="$kubeconfig"
    apply_manifests_with_retries "${manifests[@]}"
}

# Process each kubeconfig file found in bin/
for kubeconfig in $(pwd)/bin/kubeconfig-*; do
    if [[ -f "$kubeconfig" ]]; then
        # Extract cluster name from kubeconfig filename to determine manifests
        cluster_name=$(basename "$kubeconfig" | sed 's/kubeconfig-//')

        # Install KubeVirt on this cluster
        install_kubevirt "$kubeconfig"

        # Determine manifests based on cluster name and apply them
        case "$cluster_name" in
            "pe-kind-a")
                apply_demo_manifests "$kubeconfig" "cluster-a-openpe.yaml" "cluster-a-workload.yaml"
                ;;
            "pe-kind-b")
                apply_demo_manifests "$kubeconfig" "cluster-b-openpe.yaml" "cluster-b-workload.yaml"
                ;;
            *)
                echo "Unknown cluster: $cluster_name, skipping manifest application..."
                continue
                ;;
        esac
    fi
done
