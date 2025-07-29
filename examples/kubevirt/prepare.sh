#!/bin/bash
set -euo pipefail
set -x
CURRENT_PATH=$(dirname "$0")

source "${CURRENT_PATH}/../common.sh"

DEMO_MODE=true make deploy
export KUBECONFIG=$(pwd)/bin/kubeconfig

kubectl apply -f https://github.com/kubevirt/kubevirt/releases/download/v1.5.2/kubevirt-operator.yaml
kubectl apply -f https://github.com/kubevirt/kubevirt/releases/download/v1.5.2/kubevirt-cr.yaml
kubectl -n kubevirt wait kv kubevirt --for=condition=Available --timeout=600s

apply_manifests_with_retries openpe.yaml workload.yaml
