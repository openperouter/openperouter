#!/bin/bash
set -euo pipefail
set -x
CURRENT_PATH=$(dirname "$0")
REPO_ROOT=$(git -C "${CURRENT_PATH}" rev-parse --show-toplevel)

source "${CURRENT_PATH}/../../common.sh"

DEMO_MODE=true CALICO_MODE=true make -C "${REPO_ROOT}" deploy
export KUBECONFIG="${REPO_ROOT}/bin/kubeconfig"

apply_manifests_with_retries openpe.yaml 
apply_manifests_with_retries calico_config.yaml  workload.yaml

