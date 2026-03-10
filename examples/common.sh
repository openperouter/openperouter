#!/bin/bash

KIND_BIN="${KIND_BIN:-kind}"
CONTAINER_ENGINE=${CONTAINER_ENGINE:-"docker"}
CONTAINER_ENGINE_CLI="docker"
KIND=${KIND:-"kind"}
KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME:-pe-kind}"
KIND_COMMAND=$KIND

if [[ $CONTAINER_ENGINE == "podman" ]]; then
    CONTAINER_ENGINE_CLI="sudo podman"
fi

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

load_local_image_to_kind() {
    local image_tag="$1"
    local file_name="$2"
    local platform="$3"
    local temp_file="/tmp/${file_name}.tar"
    rm -f "${temp_file}" || sudo rm -f "${temp_file}" || true
    ${CONTAINER_ENGINE_CLI} save --platform "${platform}" -o "${temp_file}" "${image_tag}"
    ${KIND_COMMAND} load image-archive "${temp_file}" --name "${KIND_CLUSTER_NAME}"
}

load_image_to_kind() {
    local image_tag="$1"
    local file_name="$2"
    local platform="$3"
    ${CONTAINER_ENGINE_CLI} image pull --platform "${platform}" "${image_tag}"
    load_local_image_to_kind "${image_tag}" "${file_name}" "${platform}"
}
