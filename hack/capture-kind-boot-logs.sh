#!/usr/bin/env bash
set -euo pipefail

# Containerlab's embedded Kind deletes failed nodes before kind export logs can run.
# Follow their output as soon as they appear so boot failures survive that cleanup.
log_dir="${1:?Usage: $0 <log-directory>}"
mkdir -p "$log_dir"
declare -A captured=()
followers=()

cleanup() {
    if ((${#followers[@]})); then
        kill "${followers[@]}" 2>/dev/null || true
        wait "${followers[@]}" 2>/dev/null || true
    fi
}
trap cleanup EXIT
trap 'exit 0' TERM INT

while true; do
    nodes=$(docker ps -a --filter label=io.x-k8s.kind.cluster --format '{{.ID}} {{.Names}}')
    while read -r id name; do
        if [[ -z "$id" || -n "${captured[$id]:-}" ]]; then
            continue
        fi
        captured[$id]=1
        docker logs --timestamps --follow "$id" > "$log_dir/$name-boot.log" 2>&1 &
        followers+=("$!")
    done <<< "$nodes"
    sleep 0.5
done
