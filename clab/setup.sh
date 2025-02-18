#!/bin/bash
set -euo pipefail

pushd "$(dirname $(readlink -f $0))"
KUBECONFIG_PATH=${KUBECONFIG_PATH:-"$(pwd)/kubeconfig"}
KIND_BIN=${KIND_BIN:-"kind"}
CLAB_VERSION=0.64.0

KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME:-pe-kind}"

clusters=$("${KIND_BIN}" get clusters)
for cluster in $clusters; do
  if [[ $cluster == "$KIND_CLUSTER_NAME" ]]; then
    echo "Cluster ${KIND_CLUSTER_NAME} already exists"
    exit 0
  fi
done

if [[ ! -d "/sys/class/net/leaf2-switch" ]]; then
	sudo ip link add name leaf2-switch type bridge
fi

if [[ $(cat /sys/class/net/leaf2-switch/operstate) != "up" ]]; then
sudo ip link set dev leaf2-switch up
fi

docker run --rm -it --privileged \
    --network host \
    -v /var/run/docker.sock:/var/run/docker.sock \
    -v /var/run/netns:/var/run/netns \
    -v /etc/hosts:/etc/hosts \
    -v /var/lib/docker/containers:/var/lib/docker/containers \
    --pid="host" \
    -v $(pwd):$(pwd) \
    -w $(pwd) \
    ghcr.io/srl-labs/clab:$CLAB_VERSION /usr/bin/clab deploy --reconfigure --topo kind.clab.yml

docker image pull quay.io/metallb/frr-k8s:main
docker image pull quay.io/frrouting/frr:9.0.0
docker image pull quay.io/frrouting/frr:9.0.2
docker image pull gcr.io/kubebuilder/kube-rbac-proxy:v0.13.1
kind load docker-image quay.io/frrouting/frr:9.0.0 --name pe-kind
kind load docker-image quay.io/frrouting/frr:9.0.2 --name pe-kind
kind load docker-image gcr.io/kubebuilder/kube-rbac-proxy:v0.13.1 --name pe-kind
kind load docker-image quay.io/metallb/frr-k8s:main --name pe-kind

docker cp kind/setup.sh pe-kind-control-plane:/setup.sh
docker cp kind/setupworker.sh pe-kind-worker:/setupworker.sh
docker exec pe-kind-control-plane /setup.sh
docker exec pe-kind-worker /setupworker.sh

kind --name pe-kind get kubeconfig > $KUBECONFIG_PATH
export KUBECONFIG=$KUBECONFIG_PATH
kind/frr-k8s/setup.sh

sleep 4s
docker exec clab-kind-leaf1 /setup.sh
docker exec clab-kind-leaf2 /setup.sh
docker exec clab-kind-spine /setup.sh
docker exec clab-kind-HOST1 /setup.sh
popd
