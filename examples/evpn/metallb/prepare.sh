#!/bin/bash
set -euo pipefail
set -x
CURRENT_PATH=$(dirname "$0")

source "${CURRENT_PATH}/../../common.sh"

DEMO_MODE=true make deploy
export KUBECONFIG=$(pwd)/bin/kubeconfig

helm repo add metallb https://metallb.github.io/metallb

kubectl apply -f - <<EOF
apiVersion: v1
kind: Namespace
metadata:
  name: metallb-system
  labels:
    pod-security.kubernetes.io/enforce: privileged
    pod-security.kubernetes.io/audit: privileged
    pod-security.kubernetes.io/warn: privileged
EOF

# deploy metallb with frr-k8s as external backend
helm install metallb metallb/metallb --namespace metallb-system --set frrk8s.external=true --set frrk8s.namespace=frr-k8s-system --set speaker.ignoreExcludeLB=true --set speaker.frr.enabled=false --set frr-k8s.prometheus.serviceMonitor.enabled=false

wait_for_pods metallb-system app.kubernetes.io/name=metallb

docker image pull nginx:1.25
${KIND_BIN} --name pe-kind load docker-image nginx:1.25

apply_manifests_with_retries metallb.yaml openpe.yaml workload.yaml

