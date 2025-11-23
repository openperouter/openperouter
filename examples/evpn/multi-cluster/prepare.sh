#!/bin/bash
set -euo pipefail
set -x
CURRENT_PATH=$(dirname "$0")

source "${CURRENT_PATH}/../../common.sh"

DEMO_MODE=true make deploy-multi

provision_migration_network() {
    local kubeconfig="$1"
    local cluster_name="$2"

    echo "Pre-provisioning dedicated migration network using kubeconfig: ${kubeconfig} for cluster: ${cluster_name}"

    # Create kubevirt namespace if it doesn't exist
    KUBECONFIG="$kubeconfig" kubectl create namespace kubevirt --dry-run=client -o yaml | KUBECONFIG="$kubeconfig" kubectl apply -f -

    # Apply the cluster-specific dedicated migration network
    case "$cluster_name" in
        "pe-kind-a")
            KUBECONFIG="$kubeconfig" kubectl apply -f "${CURRENT_PATH}/cluster-a-migration-l2vni.yaml" || true
            KUBECONFIG="$kubeconfig" kubectl apply -f "${CURRENT_PATH}/cluster-a-migration-nad.yaml" || true
            ;;
        "pe-kind-b")
            KUBECONFIG="$kubeconfig" kubectl apply -f "${CURRENT_PATH}/cluster-b-migration-l2vni.yaml" || true
            KUBECONFIG="$kubeconfig" kubectl apply -f "${CURRENT_PATH}/cluster-b-migration-nad.yaml" || true
            ;;
        *)
            echo "Unknown cluster: $cluster_name, skipping migration network provisioning..."
            return 1
            ;;
    esac

    echo "Migration network provisioned successfully for cluster: ${cluster_name}"
}

install_whereabouts() {
    local kubeconfig="$1"

    echo "Installing Whereabouts CNI plugin using kubeconfig: ${kubeconfig}"

    KUBECONFIG="$kubeconfig" kubectl apply -f https://raw.githubusercontent.com/k8snetworkplumbingwg/whereabouts/refs/heads/master/doc/crds/daemonset-install.yaml
    KUBECONFIG="$kubeconfig" kubectl apply -f https://raw.githubusercontent.com/k8snetworkplumbingwg/whereabouts/refs/heads/master/doc/crds/whereabouts.cni.cncf.io_ippools.yaml
    KUBECONFIG="$kubeconfig" kubectl apply -f https://raw.githubusercontent.com/k8snetworkplumbingwg/whereabouts/refs/heads/master/doc/crds/whereabouts.cni.cncf.io_overlappingrangeipreservations.yaml

    KUBECONFIG="$kubeconfig" kubectl rollout status daemonset/whereabouts -n kube-system --timeout=5m

    echo "Whereabouts CNI plugin installed successfully"
}

install_kubevirt() {
    local kubeconfig="$1"

    echo "Installing KubeVirt using kubeconfig: ${kubeconfig}"

    KUBECONFIG="$kubeconfig" kubectl apply -f https://github.com/kubevirt/kubevirt/releases/download/v1.6.2/kubevirt-operator.yaml
    KUBECONFIG="$kubeconfig" kubectl apply -f https://github.com/kubevirt/kubevirt/releases/download/v1.6.2/kubevirt-cr.yaml

    # Patch KubeVirt with:
    # - allow scheduling on control-planes
    # - enable decentralized live migration feature gate
    # - configure migration network
    KUBECONFIG="$kubeconfig" kubectl patch -n kubevirt kubevirt kubevirt --type merge --patch '{
        "spec": {
            "workloads": {
                "nodePlacement": {
                    "tolerations": [
                        {
                            "key": "node-role.kubernetes.io/control-plane",
                            "operator": "Exists",
                            "effect": "NoSchedule"
                        }
                    ]
                }
            },
            "configuration": {
                "developerConfiguration": {
                    "featureGates": ["DecentralizedLiveMigration"]
                },
                "migrations": {
                    "network": "migration-evpn"
                }
            }
        }
    }'

    KUBECONFIG="$kubeconfig" kubectl wait --for=condition=Available kubevirt/kubevirt -n kubevirt --timeout=10m
}

exchange_kubevirt_certificates() {
    local kubeconfig_a="$1"
    local kubeconfig_b="$2"

    echo "Exchanging KubeVirt certificates between clusters"

    local ca_bundle_a
    ca_bundle_a=$(KUBECONFIG="$kubeconfig_a" kubectl get configmap kubevirt-ca -n kubevirt -o jsonpath='{.data.ca-bundle}' 2>/dev/null || echo "")

    if [[ -z "$ca_bundle_a" ]]; then
        echo "Warning: Could not read kubevirt-ca configmap from cluster A, skipping certificate exchange"
        return 1
    fi

    local ca_bundle_b
    ca_bundle_b=$(KUBECONFIG="$kubeconfig_b" kubectl get configmap kubevirt-ca -n kubevirt -o jsonpath='{.data.ca-bundle}' 2>/dev/null || echo "")

    if [[ -z "$ca_bundle_b" ]]; then
        echo "Warning: Could not read kubevirt-ca configmap from cluster B, skipping certificate exchange"
        return 1
    fi

    echo "Setting cluster B's CA certificate in cluster A's kubevirt-external-ca configmap"
    KUBECONFIG="$kubeconfig_a" kubectl create configmap kubevirt-external-ca -n kubevirt --from-literal=ca-bundle="$ca_bundle_b" --dry-run=client -o yaml | \
        KUBECONFIG="$kubeconfig_a" kubectl apply -f -

    echo "Setting cluster A's CA certificate in cluster B's kubevirt-external-ca configmap"
    KUBECONFIG="$kubeconfig_b" kubectl create configmap kubevirt-external-ca -n kubevirt --from-literal=ca-bundle="$ca_bundle_a" --dry-run=client -o yaml | \
        KUBECONFIG="$kubeconfig_b" kubectl apply -f -

    echo "KubeVirt certificate exchange completed successfully"
}

apply_demo_manifests() {
    local kubeconfig="$1"
    local manifests=("${@:2}")

    echo "Applying demo manifests using kubeconfig: ${kubeconfig}"

    export KUBECONFIG="$kubeconfig"
    apply_manifests_with_retries "${manifests[@]}"
}

declare -A kubeconfigs

for kubeconfig in $(pwd)/bin/kubeconfig-*; do
    if [[ -f "$kubeconfig" ]]; then
        cluster_name=$(basename "$kubeconfig" | sed 's/kubeconfig-//')
        kubeconfigs["$cluster_name"]="$kubeconfig"

        # Install Whereabouts CNI plugin before KubeVirt since we want the
        # KubeVirt installation to know which migration network to use.
        # KubeVirt's dedicated migration network requires whereabouts IPAM.
        install_whereabouts "$kubeconfig"

        # Provision dedicated migration network before KubeVirt installation
        provision_migration_network "$kubeconfig" "$cluster_name"

        install_kubevirt "$kubeconfig"

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

# Exchange KubeVirt certificates between all clusters after everything is provisioned
if [[ -n "${kubeconfigs["pe-kind-a"]-}" && -n "${kubeconfigs["pe-kind-b"]-}" ]]; then
    echo "Exchanging KubeVirt certificates between clusters..."
    exchange_kubevirt_certificates "${kubeconfigs["pe-kind-a"]}" "${kubeconfigs["pe-kind-b"]}"
fi
