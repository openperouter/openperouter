#!/bin/bash
#
# DNM debugging helper.
#
# Scans the exported kind logs for FRR daemon crashes and unexpected watchfrr
# restarts and fails the job when it finds one, printing the surrounding log
# lines.
#
# Without this a crash that watchfrr recovers from leaves no trace in the job
# result: the suite either passes, or fails much later in an unrelated spec
# because the node it happened on stayed degraded.

set -uo pipefail

KIND_EXPORT_LOGS="${1:-/tmp/kind_logs}"
CONTEXT_LINES="${CONTEXT_LINES:-25}"

# Printed by the FRR crash handler in lib/sigevent.c.
SIGNAL_PATTERN='Received signal [0-9]+ at '
# Printed by watchfrr when a daemon disappears. The "initial connection
# attempt failed" variant is normal during startup, before the daemons are up.
RESTART_PATTERN='state -> down'
STARTUP_NOISE='initial connection attempt failed'

if [ ! -d "${KIND_EXPORT_LOGS}" ]; then
  echo "No exported logs at ${KIND_EXPORT_LOGS}, skipping crash detection."
  exit 0
fi

sudo chmod -R a+r "${KIND_EXPORT_LOGS}" 2>/dev/null || true

crashed_files=""
restarted_files=""

while IFS= read -r file; do
  [ -z "${file}" ] && continue
  if grep -qE "${SIGNAL_PATTERN}" "${file}" 2>/dev/null; then
    crashed_files="${crashed_files}${file}"$'\n'
  elif grep -E "${RESTART_PATTERN}" "${file}" 2>/dev/null | grep -qv "${STARTUP_NOISE}"; then
    restarted_files="${restarted_files}${file}"$'\n'
  fi
done <<<"$(grep -rEl "${SIGNAL_PATTERN}|${RESTART_PATTERN}" "${KIND_EXPORT_LOGS}" 2>/dev/null || true)"

if [ -z "${crashed_files}" ] && [ -z "${restarted_files}" ]; then
  echo "No FRR daemon crash or unexpected restart found in ${KIND_EXPORT_LOGS}."
  exit 0
fi

dump_context() {
  local file="$1" pattern="$2"
  echo "::group::${file}"
  grep -nE -B "${CONTEXT_LINES}" -A 5 "${pattern}" "${file}" 2>/dev/null | tail -n 400
  echo "::endgroup::"
}

while IFS= read -r file; do
  [ -z "${file}" ] && continue
  dump_context "${file}" "${SIGNAL_PATTERN}"
done <<<"${crashed_files}"

while IFS= read -r file; do
  [ -z "${file}" ] && continue
  dump_context "${file}" "${RESTART_PATTERN}"
done <<<"${restarted_files}"

if [ -n "${crashed_files}" ]; then
  echo
  echo "Crash lines:"
  grep -rhE "${SIGNAL_PATTERN}" "${KIND_EXPORT_LOGS}" 2>/dev/null | sort -u
  echo
  echo "::error::An FRR daemon crashed during this run, see the groups above and the symbolized backtraces."
  exit 1
fi

echo
echo "::warning::An FRR daemon restarted during this run without leaving crash handler output."
exit 0
