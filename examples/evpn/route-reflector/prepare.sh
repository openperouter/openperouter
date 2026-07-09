#!/bin/bash
set -euo pipefail
set -x
CURRENT_PATH=$(dirname "$0")

source "${CURRENT_PATH}/../../common.sh"

DEMO_MODE=true make deploy
KUBECONFIG="$(pwd)/bin/kubeconfig"
export KUBECONFIG

apply_manifests_with_retries openpe.yaml workload.yaml
