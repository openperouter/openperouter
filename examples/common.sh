#!/bin/bash

KIND_BIN="${KIND_BIN:-kind}"

apply_manifests_with_retries() {
  local manifests=("$@")
  for manifest in "${manifests[@]}"; do
    attempt=1
    max_attempts=50
    until kubectl apply -f "${CURRENT_PATH}/${manifest}"; do
      if (( attempt >= max_attempts )); then
        echo "Failed to apply ${manifest} after ${max_attempts} attempts."
        exit 1
      fi
      attempt=$((attempt+1))
      sleep 5
    done
  done
}

wait_for_pods() {
  local namespace=$1
  local selector=$2

  echo "waiting for pods $namespace - $selector to be created"
  timeout 5m bash -c "until [[ -n \$(kubectl get pods -n $namespace -l $selector 2>/dev/null) ]]; do sleep 5; done"
  echo "waiting for pods $namespace to be ready"
  timeout 5m bash -c "until kubectl -n $namespace wait --for=condition=Ready --all pods --timeout 2m; do sleep 5; done"
  echo "pods for $namespace are ready"
}
