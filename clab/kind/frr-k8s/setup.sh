#!/bin/bash

MULTUS_VERSION=${MULTUS_VERSION:-"v4.2.1"}
MVLAN_VERSION=${MVLAN_VERSION:-"v1.7.1"}
kubectl apply -k $(dirname ${BASH_SOURCE[0]})
sleep 2s
kubectl -n frr-k8s-system wait --for=condition=Ready --all pods --timeout 300s

kubectl apply -f https://raw.githubusercontent.com/k8snetworkplumbingwg/multus-cni/refs/tags/${MULTUS_VERSION}/deployments/multus-daemonset.yml


TEMP_GOBIN=$(mktemp -d)
GOBIN=$TEMP_GOBIN go install github.com/containernetworking/plugins/plugins/main/macvlan@${MVLAN_VERSION}
GOBIN=$TEMP_GOBIN go install github.com/containernetworking/plugins/plugins/ipam/static@${MVLAN_VERSION}

CNI_PATH="/opt/cni/bin"

KIND_NODES=$(kind get nodes --name pe-kind)

for NODE in $KIND_NODES; do
  docker cp $TEMP_GOBIN/macvlan $NODE:$CNI_PATH/
  docker cp $TEMP_GOBIN/static $NODE:$CNI_PATH/
done

