#!/bin/bash
#
# DNM debugging helper.
#
# Symbolizes the FRR core dumps collected by clab/setup.sh, writing a
# backtrace next to every core and echoing it to the job log so that the crash
# can be read without downloading and matching artifacts by hand.
#
# The cores are produced by the kernel core_pattern pipe set up in
# clab/setup.sh, and land in ${KIND_EXPORT_LOGS}/core_dumps as
# core.<mangled-exe-path>.<pid>.<host>.<signal>.<time>.
#
# Requires the router image to be built with FRR_VARIANT=debug, otherwise the
# binaries carry no symbols and the backtrace is only a list of addresses.

set -euo pipefail

KIND_EXPORT_LOGS="${1:-/tmp/kind_logs}"
ROUTER_IMAGE="${2:-quay.io/openperouter/router:main}"
CONTAINER_ENGINE="${CONTAINER_ENGINE:-docker}"

CORE_DIR="${KIND_EXPORT_LOGS}/core_dumps"
OUT_DIR="${KIND_EXPORT_LOGS}/core_backtraces"

if [ ! -d "${CORE_DIR}" ]; then
  echo "No core dump directory at ${CORE_DIR}, nothing to symbolize."
  exit 0
fi

shopt -s nullglob
cores=("${CORE_DIR}"/core.*)
if [ ${#cores[@]} -eq 0 ]; then
  echo "No core dumps in ${CORE_DIR}."
  exit 0
fi

if ! "${CONTAINER_ENGINE}" image inspect "${ROUTER_IMAGE}" >/dev/null 2>&1; then
  echo "Router image ${ROUTER_IMAGE} is not available locally, skipping symbolization."
  exit 0
fi

mkdir -p "${OUT_DIR}"
sudo chmod -R a+r "${CORE_DIR}"

echo "Found ${#cores[@]} core dump(s) in ${CORE_DIR}."

if ! "${CONTAINER_ENGINE}" run --rm --entrypoint sh "${ROUTER_IMAGE}" \
  -c "command -v gdb >/dev/null" 2>/dev/null; then
  echo "::warning::${ROUTER_IMAGE} has no gdb, rebuild it with FRR_VARIANT=debug to symbolize cores."
  exit 0
fi

for core in "${cores[@]}"; do
  name="$(basename "${core}")"

  # core.!usr!lib!frr!zebra.40301.pe-kind-worker.6.1785742859 -> /usr/lib/frr/zebra
  mangled="$(echo "${name}" | cut -d. -f2)"
  binary="${mangled//!//}"

  echo "::group::backtrace ${name} (${binary})"

  "${CONTAINER_ENGINE}" run --rm \
    -v "${CORE_DIR}:/cores:ro" \
    --entrypoint gdb "${ROUTER_IMAGE}" \
    -batch -q \
    -ex "set pagination off" \
    -ex "thread apply all bt full" \
    -ex "info registers" \
    -ex "info sharedlibrary" \
    "${binary}" "/cores/${name}" \
    >"${OUT_DIR}/${name}.bt" 2>&1 || true

  cat "${OUT_DIR}/${name}.bt"
  echo "::endgroup::"

  if ! grep -qE '^#[0-9]+ +(0x[0-9a-f]+ in )?[a-zA-Z_][a-zA-Z0-9_]* \(' "${OUT_DIR}/${name}.bt"; then
    echo "::warning::No frame in ${name} resolved to a symbol. The core was most" \
      "likely produced by a stock FRR build, rebuild the router image with FRR_VARIANT=debug."
  fi
done

echo "Backtraces written to ${OUT_DIR}."
