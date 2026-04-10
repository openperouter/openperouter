#!/bin/bash
# Setup veth monitoring
set -euo pipefail

source "$(dirname $(readlink -f $0))/../common.sh"

# Get cluster names from arguments
CLUSTER_NAMES=("$@")

if [[ ${#CLUSTER_NAMES[@]} -eq 0 ]]; then
    echo "Usage: $0 <cluster_name> [cluster_name2] ..."
    echo "Example: $0 pe-kind"
    echo "Example: $0 pe-kind-a pe-kind-b"
    exit 1
fi

setup_veth_monitoring() {
    echo "Setting up veth monitoring for clusters: ${CLUSTER_NAMES[*]}"

    pushd "$(dirname $(readlink -f $0))/.."

    # Check if monitoring is already running to avoid duplicates
    if pgrep -f check_veths.go | xargs -r ps -p | grep -q pe-kind; then
        echo "Veth monitoring already running"
    else
        echo "Starting veth monitoring"

        CHECK_VETHS_LOG="/tmp/check_veths.log"
        if [[ ${#CLUSTER_NAMES[@]} -eq 1 && "${CLUSTER_NAMES[0]}" == "pe-kind" ]]; then
            # Single cluster mode
            cat <<'EOF' | sudo -E "$(which go)" run tools/check_veths/*.go 2>&1 | awk '{print strftime("%Y-%m-%dT%H:%M:%S"), $0; fflush()}' > "$CHECK_VETHS_LOG" &
interfaces:
- left:
    container: "clab-kind-leafkind"
    name: "tokindworker"
  right:
    container: "pe-kind-worker"
    name: "toleafkind"
- left:
    container: "clab-kind-leafkind"
    name: "tokindctrlpl"
  right:
    container: "pe-kind-control-plane"
    name: "toleafkind"
- left:
    bridge: "leafkind-switch"
    name: "kindctrlpl"
  right:
    container: "pe-kind-control-plane"
    name: "toswitch"
    ips: ["192.168.11.3/24", "2001:db8:11::3/64"]
- left:
    bridge: "leafkind-switch"
    name: "kindworker"
  right:
    container: "pe-kind-worker"
    name: "toswitch"
    ips: ["192.168.11.4/24", "2001:db8:11::4/64"]
EOF
        else
            # Multi-cluster mode
            cat <<'EOF' | sudo -E "$(which go)" run tools/check_veths/*.go 2>&1 | awk '{print strftime("%Y-%m-%dT%H:%M:%S"), $0; fflush()}' > "$CHECK_VETHS_LOG" &
interfaces:
- left:
    bridge: "leafkind-switch"
    name: "kindctrlpla
  right:
    container: "pe-kind-a-control-plane"
    name: "toswitch"
    ips": ["192.168.11.3/24", "2001:db8:11::3/64"]
- left:
    bridge: "leafkind-switch"
    name: "kindworkera"
  right:
    container: "pe-kind-a-worker"
    name: "toswitch"
    ips: ["192.168.11.4/24", "2001:db8:11::4/64"]
- left:
    bridge: "leafkind-switch"
    name: "kindctrlplb"
  right:
    container: "pe-kind-b-control-plane"
    name: "toswitch"
    ips: ["192.168.12.3/24", "2001:db8:12::3/64"]
- left:
    bridge: "leafkind-switch"
    name: "kindworkerb"
  right:
    container: "pe-kind-b-worker"
    name: "toswitch"
    ips: ["192.168.12.4/24", "2001:db8:12::4/64"]
EOF
        fi
    fi

    # Give some time for the monitoring to start
    sleep 4s

    popd
}

setup_veth_monitoring
