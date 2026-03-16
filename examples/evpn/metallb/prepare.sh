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

image_tag="nginx:1.25"
platform="linux/$($(which go) env GOARCH)"
echo "Loading $image_tag (platform $platform) to cluster $KIND_CLUSTER_NAME"
load_image_to_kind "${image_tag}" nginx "${platform}"

apply_manifests_with_retries metallb.yaml openpe.yaml workload.yaml

