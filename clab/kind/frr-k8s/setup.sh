#!/bin/bash
set -x

MULTUS_VERSION=${MULTUS_VERSION:-"v4.2.1"}
CNI_PLUGINS_VERSION=${CNI_PLUGINS_VERSION:-"v1.7.1"}
KIND_CLUSTER_NAME=${KIND_CLUSTER_NAME:-"pe-kind"}

retry_cmd() {
  local cmd=${1}
  local max_retries=10
  local i=0
  while ! ${cmd}; do
    i=$((i+1))
    if [ ${i} -ge ${max_retries} ]; then
      echo "Running command failed. Aborting."
      exit 1
    fi
    echo "Running command failed. Retrying."
    sleep 2
  done
}

kubectl apply -k $(dirname ${BASH_SOURCE[0]})
retry_cmd "kubectl apply -f https://raw.githubusercontent.com/k8snetworkplumbingwg/multus-cni/refs/tags/${MULTUS_VERSION}/deployments/multus-daemonset.yml"

sleep 2s
echo "Waiting for frr-k8s-system pods to be ready"
kubectl -n frr-k8s-system wait --for=condition=Ready --all pods --timeout 300s

echo "Waiting for multus pods to be ready"
kubectl -n kube-system wait --for=condition=Ready --all pods --timeout 300s

TEMP_GOBIN=$(mktemp -d)
GOBIN=$TEMP_GOBIN go install github.com/containernetworking/plugins/plugins/main/macvlan@${CNI_PLUGINS_VERSION}
GOBIN=$TEMP_GOBIN go install github.com/containernetworking/plugins/plugins/main/bridge@${CNI_PLUGINS_VERSION}
GOBIN=$TEMP_GOBIN go install github.com/containernetworking/plugins/plugins/ipam/static@${CNI_PLUGINS_VERSION}

CNI_PATH="/opt/cni/bin"

KIND_BIN=${KIND:-kind}
KIND_NODES=$(${KIND_BIN} get nodes --name "$KIND_CLUSTER_NAME")

CONTAINER_CLI=${CONTAINER_ENGINE_CLI:-docker}

for NODE in $KIND_NODES; do
  ${CONTAINER_CLI} cp $TEMP_GOBIN/macvlan $NODE:$CNI_PATH/
  ${CONTAINER_CLI} cp $TEMP_GOBIN/bridge $NODE:$CNI_PATH/
  ${CONTAINER_CLI} cp $TEMP_GOBIN/static $NODE:$CNI_PATH/
done

