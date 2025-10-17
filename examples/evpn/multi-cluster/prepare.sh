#!/bin/bash
set -euo pipefail
set -x
CURRENT_PATH=$(dirname "$0")

source "${CURRENT_PATH}/../../common.sh"

DEMO_MODE=true make deploy-multi
export KUBECONFIG=$(pwd)/bin/kubeconfig-pe-kind-a

# install KV in the first cluster
kubectl apply -f https://github.com/kubevirt/kubevirt/releases/download/v1.5.2/kubevirt-operator.yaml
kubectl apply -f https://github.com/kubevirt/kubevirt/releases/download/v1.5.2/kubevirt-cr.yaml
# Patch KubeVirt to allow scheduling on control-planes, so we can test live migration between two nodes
kubectl patch -n kubevirt kubevirt kubevirt --type merge --patch '{"spec": {"workloads": {"nodePlacement": {"tolerations": [{"key": "node-role.kubernetes.io/control-plane", "operator": "Exists", "effect": "NoSchedule"}]}}}}'
kubectl wait --for=condition=Available kubevirt/kubevirt -n kubevirt --timeout=10m

# provision the manifests on the first cluster
apply_manifests_with_retries cluster-a-openpe.yaml cluster-a-workload.yaml

export KUBECONFIG=$(pwd)/bin/kubeconfig-pe-kind-b

kubectl apply -f https://github.com/kubevirt/kubevirt/releases/download/v1.5.2/kubevirt-operator.yaml
kubectl apply -f https://github.com/kubevirt/kubevirt/releases/download/v1.5.2/kubevirt-cr.yaml
# Patch KubeVirt to allow scheduling on control-planes, so we can test live migration between two nodes
kubectl patch -n kubevirt kubevirt kubevirt --type merge --patch '{"spec": {"workloads": {"nodePlacement": {"tolerations": [{"key": "node-role.kubernetes.io/control-plane", "operator": "Exists", "effect": "NoSchedule"}]}}}}'
kubectl wait --for=condition=Available kubevirt/kubevirt -n kubevirt --timeout=10m

apply_manifests_with_retries cluster-b-openpe.yaml cluster-b-workload.yaml
