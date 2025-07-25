#!/bin/bash
set -euo pipefail
set -x
CURRENT_PATH=$(dirname "$0")

source "${CURRENT_PATH}/../common.sh"

DEMO_MODE=true make deploy
export KUBECONFIG=$(pwd)/bin/kubeconfig

apply_manifests_with_retries openpe.yaml workload.yaml cni-plugins.yaml
