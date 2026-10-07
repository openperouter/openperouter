#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

join() {
  local IFS="$1"
  shift
  echo "$*"
}

echo "=== Run e2e tests ==="

pushd "$SCRIPT_DIR"/../..

# The following lists tests that are (currently) incompatible with the
# downstream setup.
EXCLUDE_ARRAY=(
  'Routes.between.bgp.and.the.fabric.underlay.address.family.Unnumbered'
  'DHCP.underlay.lifecycle.removes.the.DHCP.interfaces.when.the.underlay.is.deleted'
  'reflects.type-2.routes.between.client.nodes.via.the.route.reflector'
  'assigns.each.underlay.port.a.unique.IPv6.link-local'
)

SKIP="$(join '|' "${EXCLUDE_ARRAY[@]}")"

CONTAINER_RUNTIME=docker make e2etests \
       TEST_ARGS="--nodelink-config=${SCRIPT_DIR}/nodelink.json \
               --frrk8s-namespace=openshift-frr-k8s \
               --openperouter-namespace=openshift-openperouter" \
       KUBECONFIG_PATH="${KUBECONFIG}" \
       GINKGO_ARGS="--junit-report=junit_e2e.xml \
               --label-filter=\!'systemdmode' \
               --skip='${SKIP}'"

popd
