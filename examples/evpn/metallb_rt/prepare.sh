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


apply_manifests_with_retries metallb.yaml openpe.yaml workload.yaml


for LEAF in clab-kind-leafA clab-kind-leafB; do  
  docker exec "$LEAF" vtysh -c "conf t" \
    -c 'router bgp 64520 vrf red' \
    -c 'address-family l2vpn evpn' \
    -c 'route-target import 65000:100' \
    -c 'route-target export 65000:100' \
    -c 'exit-address-family' \
    -c 'exit' \
    -c 'router bgp 64520 vrf blue' \
    -c 'address-family l2vpn evpn' \
    -c 'route-target import 65000:200' \
    -c 'route-target export 65000:200' \
    -c 'exit-address-family'
done