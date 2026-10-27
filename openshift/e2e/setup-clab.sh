#!/bin/bash
# Sets up the containerlab fabric and wires it to an OCP cluster deployed via dev-scripts.
#
# Prerequisites:
#   - OCP cluster running via dev-scripts
#   - Extra networks configured via setup_extra_networks.sh (toswitch1, toswitch2)
#   - OpenPerOuter operator deployed on the cluster
#   - curl available (used to install containerlab when missing)
#   - Docker Engine available
#
# Produces:
#   - Running clab topology wired to extra network bridges
#   - nodelink.json for the test suite
#
# Usage:
#   export KUBECONFIG=/root/dev-scripts/ocp/ostest/auth/kubeconfig
#   ./openshift/e2e/setup-clab.sh
#   cd e2etests && CONTAINER_RUNTIME=docker go test -v ./suite/ \
#     --nodelink-config=../openshift/e2e/nodelink.json \
#     --frrk8s-namespace=openshift-frr-k8s --openperouter-namespace=openshift-openperouter

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
NODELINK_OUT="${SCRIPT_DIR}/nodelink.json"


CLI="sudo docker"

if ! command -v containerlab >/dev/null 2>&1; then
    echo "containerlab not found; installing it"
    command -v curl >/dev/null 2>&1 || {
        echo "curl is required to install containerlab" >&2
        exit 1
    }
    bash -c "$(curl -fsSL https://get.containerlab.dev)"
fi

echo "=== Step 1: Generate peerLeaf FRR configs ==="
cd "${REPO_ROOT}/clab/tools"
go build -o generate_leaf_config/generate_leaf generate_leaf_config/common.go generate_leaf_config/generate_leaf.go
go build -o generate_leaf_config/generate_leafkind generate_leaf_config/common.go generate_leaf_config/generate_leafkind.go

rm -f ../leafA/frr.conf
./generate_leaf_config/generate_leaf \
    -leaf leafA -neighbor 192.168.1.0 -network 100.64.0.1/32 \
    -template generate_leaf_config/frr_template/frr.conf.template

rm -f ../leafB/frr.conf
./generate_leaf_config/generate_leaf \
    -leaf leafB -neighbor 192.168.1.2 -network 100.64.0.2/32 \
    -template generate_leaf_config/frr_template/frr.conf.template

rm -f ../singlecluster/leafkind1/frr.conf
./generate_leaf_config/generate_leafkind \
    -leaf singlecluster/leafkind1 -asn 64512 -spine-ip 192.168.1.4 \
    -ipv4-listen-range 192.168.11.0/24 -ipv6-listen-range 2001:db8:11::/64 \
    -isis-net 49.0001.0000.0000.0004.00 \
    -toswitch-interface toswitch1 \
    -template generate_leaf_config/frr_template/leafkind.conf.template

rm -f ../singlecluster/leafkind2/frr.conf
./generate_leaf_config/generate_leafkind \
    -leaf singlecluster/leafkind2 -asn 64513 -spine-ip 192.168.1.6 \
    -ipv4-listen-range 192.168.12.0/24 -ipv6-listen-range 2001:db8:12::/64 \
    -isis-net 49.0001.0000.0000.0005.00 \
    -toswitch-interface toswitch2 \
    -template generate_leaf_config/frr_template/leafkind.conf.template

echo "=== Step 2: Enable Docker Engine ==="
systemctl enable --now docker

echo "=== Step 3: Deploy clab topology ==="
cd "${REPO_ROOT}"
containerlab deploy --runtime docker \
    --topo "${SCRIPT_DIR}/ocp.clab.yml" --reconfigure

echo "=== Step 4: Assign IPs to clab containers ==="
cd "${REPO_ROOT}/clab"
go run tools/assign_ips/assign_ips.go \
    -file "${SCRIPT_DIR}/ip_map_ocp.txt" -engine "${CLI}"

# Match leafkind bridge-facing MTU to the libvirt bridge (1500)
${CLI} exec clab-kind-leafkind1 ip link set dev toswitch1 mtu 1500
${CLI} exec clab-kind-leafkind2 ip link set dev toswitch2 mtu 1500

echo "=== Step 5: Run container setup scripts ==="
for c in leafA leafB leafSRV6 hostA_red hostA_blue hostA_default hostB_red hostB_blue hostSRV6_red hostSRV6_blue; do
    container="clab-kind-${c}"
    echo "Setting up container: ${c}"

    if timeout 10s ${CLI} exec "${container}" test -f /setup.sh 2>/dev/null; then
        timeout 2m ${CLI} exec "${container}" bash -x /setup.sh || {
            rc=$?
            echo "Setup failed or timed out for ${container} (exit code ${rc})" >&2
            exit "${rc}"
        }
    else
        rc=$?
        if [ "${rc}" -ne 1 ]; then
            echo "Could not inspect ${container} for /setup.sh (exit code ${rc})" >&2
            exit "${rc}"
        fi
    fi
done

NODES=$(oc get nodes -l kubernetes.io/os=linux -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}')

echo "=== Step 6: Create ${NODELINK_OUT} ==="
NODE_INDEX=0
NODES_JSON=""
for node in ${NODES}; do
    TS1_V4="192.168.11.$((100 + NODE_INDEX))"
    TS2_V4="192.168.12.$((100 + NODE_INDEX))"
    TS1_V6="2001:db8:11::$((100 + NODE_INDEX))"
    TS2_V6="2001:db8:12::$((100 + NODE_INDEX))"
    NODE_INDEX=$((NODE_INDEX + 1))

    if [ -n "${NODES_JSON}" ]; then
        NODES_JSON="${NODES_JSON},"
    fi
    NODES_JSON="${NODES_JSON}
    \"${node}\": {
      \"ipForKindLeaf\": \"${TS1_V4}\",
      \"ipForKindLeaf2\": \"${TS2_V4}\",
      \"ipv6ForKindLeaf\": \"${TS1_V6}\",
      \"ipv6ForKindLeaf2\": \"${TS2_V6}\",
      \"ifaceForKindLeaf\": \"toswitch1\",
      \"ifaceForKindLeaf2\": \"toswitch2\",
      \"leafIfaceForKindLeaf\": \"toswitch1\",
      \"leafIfaceForKindLeaf2\": \"toswitch2\"
    }"
done

cat > "${NODELINK_OUT}" << TOPOEOF
{
  "nodes": {${NODES_JSON}
  }
}
TOPOEOF

echo ""
echo "=== Setup complete ==="
echo "Node links config: ${NODELINK_OUT}"
echo ""
cat "${NODELINK_OUT}"
