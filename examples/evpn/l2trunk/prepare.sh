#!/bin/bash
set -euo pipefail
set -x
CURRENT_PATH=$(dirname "$0")

source "${CURRENT_PATH}/../../common.sh"

CONTAINER_ENGINE="${CONTAINER_ENGINE:-docker}"
CLUSTER_NAME="${CLUSTER_NAME:-pe-kind}"
TRUNK_BRIDGE="br-trunk"
TRUNK_TAGS="100 200"

DEMO_MODE=true make -C "${REPO_ROOT}" deploy

# The trunk's host leg is attached to an externally owned bridge: create the
# bridge on every node first. Pods attach to it with the bridge CNI plugin,
# which gives each pod port the pvid of its tag.
for node in $(${KIND_BIN} get nodes --name "${CLUSTER_NAME}"); do
    ${CONTAINER_ENGINE} exec "${node}" sh -c "
        ip link show ${TRUNK_BRIDGE} >/dev/null 2>&1 || ip link add ${TRUNK_BRIDGE} type bridge
        ip link set ${TRUNK_BRIDGE} type bridge vlan_filtering 1 vlan_default_pvid 0
        ip link set ${TRUNK_BRIDGE} up
    "
done

apply_manifests_with_retries openpe.yaml

# The trunk host leg does not exist until the controller reconciles the
# L2Trunk. Admitting the tenant tags there is the bridge owner's job.
for _ in $(seq 1 60); do
    TRUNK_ID=$(kubectl get l2trunks.network.openperouter.io workloads -n openperouter-system \
        -o jsonpath='{.metadata.labels.network\.openperouter\.io/trunk-id}' 2>/dev/null || true)
    if [[ -n "${TRUNK_ID}" ]]; then
        break
    fi
    sleep 2
done
if [[ -z "${TRUNK_ID:-}" ]]; then
    echo "the L2Trunk did not get a trunk id label in time"
    exit 1
fi

for node in $(${KIND_BIN} get nodes --name "${CLUSTER_NAME}"); do
    for tag in ${TRUNK_TAGS}; do
        ${CONTAINER_ENGINE} exec "${node}" sh -c \
            "bridge vlan add dev host-t-${TRUNK_ID} vid ${tag} 2>/dev/null || true"
    done
done

apply_manifests_with_retries workload.yaml
