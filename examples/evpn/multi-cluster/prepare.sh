#!/bin/bash
set -euo pipefail
set -x
CURRENT_PATH=$(dirname "$0")

source "${CURRENT_PATH}/../../common.sh"

DEMO_MODE=true make deploy-multi

# Function to pre-provision dedicated migration network
provision_migration_network() {
    local kubeconfig="$1"
    local cluster_name="$2"

    echo "Pre-provisioning dedicated migration network using kubeconfig: ${kubeconfig} for cluster: ${cluster_name}"

    # Create kubevirt namespace if it doesn't exist
    KUBECONFIG="$kubeconfig" kubectl create namespace kubevirt --dry-run=client -o yaml | KUBECONFIG="$kubeconfig" kubectl apply -f -

    # Apply the cluster-specific dedicated migration network
    case "$cluster_name" in
        "pe-kind-a")
            KUBECONFIG="$kubeconfig" kubectl apply -f "${CURRENT_PATH}/cluster-a-dedicated-migration-network.yaml" || true
            ;;
        "pe-kind-b")
            KUBECONFIG="$kubeconfig" kubectl apply -f "${CURRENT_PATH}/cluster-b-dedicated-migration-network.yaml" || true
            ;;
        *)
            echo "Unknown cluster: $cluster_name, skipping migration network provisioning..."
            return 1
            ;;
    esac

    echo "Migration network provisioned successfully for cluster: ${cluster_name}"
}

# Function to install Whereabouts CNI plugin
install_whereabouts() {
    local kubeconfig="$1"

    echo "Installing Whereabouts CNI plugin using kubeconfig: ${kubeconfig}"

    # Install Whereabouts IPAM CNI plugin
    KUBECONFIG="$kubeconfig" kubectl apply -f https://raw.githubusercontent.com/k8snetworkplumbingwg/whereabouts/refs/heads/master/doc/crds/daemonset-install.yaml
    KUBECONFIG="$kubeconfig" kubectl apply -f https://raw.githubusercontent.com/k8snetworkplumbingwg/whereabouts/refs/heads/master/doc/crds/whereabouts.cni.cncf.io_ippools.yaml
    KUBECONFIG="$kubeconfig" kubectl apply -f https://raw.githubusercontent.com/k8snetworkplumbingwg/whereabouts/refs/heads/master/doc/crds/whereabouts.cni.cncf.io_overlappingrangeipreservations.yaml

    # Wait for whereabouts to be ready
    KUBECONFIG="$kubeconfig" kubectl rollout status daemonset/whereabouts -n kube-system --timeout=5m

    echo "Whereabouts CNI plugin installed successfully"
}

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

    # Configure the migration network
    KUBECONFIG="$kubeconfig" kubectl patch -n kubevirt kubevirt kubevirt --type merge --patch '{"spec": {"configuration": {"migrations": {"network": "migration-network"}}}}'

    # Wait for KubeVirt to be available
    KUBECONFIG="$kubeconfig" kubectl wait --for=condition=Available kubevirt/kubevirt -n kubevirt --timeout=10m
}

# Function to create migration bridge on all nodes
create_migration_bridge() {
    local kubeconfig="$1"
    local nodes=$(KUBECONFIG="$kubeconfig" kubectl get nodes -o jsonpath='{.items[*].metadata.name}')

    echo "Creating migration bridge on all nodes using kubeconfig: ${kubeconfig}"
    for node in $nodes; do
        echo "Creating migration bridge on node: $node"

        # Execute commands on the node via docker exec (since we're using kind)
        docker exec "$node" bash -c "
            ip link add name br-migration type bridge
            ip link set br-migration up
            if ! ip link show migration >/dev/null 2>&1; then
                exit 1
            fi
            # Attach migration interface to the bridge
            ip link set migration master br-migration
            ip link set migration up
            echo 'Migration bridge created and interface attached on node $node'
        "
    done
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

        # Install Whereabouts CNI plugin before KubeVirt since we want the
        # KubeVirt installation to know which migration network to use.
        # KubeVirt's dedicated migration network requires whereabouts IPAM.
        install_whereabouts "$kubeconfig"

        # Pre-provision dedicated migration network before KubeVirt installation
        provision_migration_network "$kubeconfig" "$cluster_name"

        # Install KubeVirt on this cluster
        install_kubevirt "$kubeconfig"

        # Create migration bridge on all nodes - this bridge is connected to
        # the outside using the "migration" veth, whose other leg is attached
        # to the "migration-net" bridge in the container hypervisor
        create_migration_bridge "$kubeconfig"

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
