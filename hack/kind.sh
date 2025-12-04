#!/usr/bin/env bash
set -o errexit

KIND_BIN="${KIND_BIN:-kind}"
KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME:-perouter}"
IP_FAMILY="${IP_FAMILY:-ipv4}"
NODE_IMAGE="${NODE_IMAGE:-quay.io/openperouter/kind-node-ovs:v1.31.4}"

clusters=$("${KIND_BIN}" get clusters)
for cluster in $clusters; do
  if [[ $cluster == "$KIND_CLUSTER_NAME" ]]; then
    echo "Cluster ${KIND_CLUSTER_NAME} already exists"
    exit 0
  fi
done

# create registry container unless it already exists
running="$($CONTAINER_ENGINE inspect -f '{{.State.Running}}' "kind-registry" 2>/dev/null || true)"
if [ "${running}" != 'true' ]; then
  $CONTAINER_ENGINE run \
    -d --restart=always -p "5000:5000" --name "kind-registry" \
    registry:2
fi

# create a cluster with the local registry enabled in containerd
KIND_CONFIG_NAME="hack/kind/config_with_registry.yaml"
"${KIND_BIN}" create cluster --image "${NODE_IMAGE}" --name "${KIND_CLUSTER_NAME}" --config=${KIND_CONFIG_NAME}

# connect the registry to the cluster network
$CONTAINER_ENGINE network connect "kind" "kind-registry" || true

# Document the local registry
# https://github.com/kubernetes/enhancements/tree/master/keps/sig-cluster-lifecycle/generic/1755-communicating-a-local-registry
kubectl apply -f hack/kind/registry_configmap.yaml

kubectl label node "$KIND_CLUSTER_NAME"-worker "$KIND_CLUSTER_NAME"-worker2 node-role.kubernetes.io/worker=worker
