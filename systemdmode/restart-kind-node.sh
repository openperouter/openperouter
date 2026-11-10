#!/usr/bin/env bash
# SPDX-License-Identifier:Apache-2.0
set -euo pipefail

runtime=$1
node=$2
repo_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
monitor="$repo_dir/clab/tools/check_veths/bin/check_veths"
pid_file="$repo_dir/clab/tools/check_veths/bin/check_veths.pid"

[[ -x "$monitor" ]] || { echo "Missing link monitor: $monitor" >&2; exit 1; }
sudo -n true

# The monitor caches container PIDs and namespace handles. Stop it before the
# restart, then start a fresh process against the new node namespace.
if [[ -f "$pid_file" ]]; then
    pid=$(cat "$pid_file")
    if [[ "$(ps -p "$pid" -o comm= || true)" == "check_veths" ]]; then
        sudo -n kill -TERM "$pid"
        for ((i=0; i<20; i++)); do
            if ! sudo -n kill -0 "$pid" 2>/dev/null; then break; fi
            sleep 0.5
        done
        if sudo -n kill -0 "$pid" 2>/dev/null; then
            echo "check_veths did not stop" >&2
            exit 1
        fi
    fi
fi

start_monitor() {
    local log_file
    log_file=$(mktemp /tmp/bootstrap-check-veths.XXXXXX.log)
    sudo -n sh -c 'log=$1; shift; nohup "$@" > "$log" 2>&1 < /dev/null &' sh \
        "$log_file" "$monitor" -f "$repo_dir/clab/singlecluster/check-veths.yaml" -p "$pid_file"
    echo "Link monitor log: $log_file"
}
trap start_monitor EXIT
"$runtime" restart --timeout 60 "$node"
