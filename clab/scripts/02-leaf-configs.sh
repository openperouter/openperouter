#!/bin/bash
# Generate leaf configurations
set -euo pipefail

source "$(dirname $(readlink -f $0))/../common.sh"

generate_leaf_configs() {
    echo "Generating leaf configurations..."

    pushd "$(dirname $(readlink -f $0))/../tools"

    # Build the command with redistribute parameter (disabled by default)
    REDISTRIBUTE_FLAG=""
    if [[ "${DEMO_MODE:-false}" == "true" ]]; then
        REDISTRIBUTE_FLAG="-redistribute-connected-from-vrfs"
        echo "Enabling redistribute connected from VRFs (demo mode)"
    else
        echo "Disabling redistribute connected from VRFs (default)"
    fi

    # Generate configs for original leafs only
    # leafA neighbors with spine at 192.168.1.0 and advertises 100.64.0.1/32
    sudo rm ../leafA/frr.conf || true
    go run generate_leaf_config/generate_leaf_config.go \
        -leaf leafA -neighbor 192.168.1.0 -network 100.64.0.1/32 $REDISTRIBUTE_FLAG \
        -template generate_leaf_config/frr_template/frr.conf.template

    # leafB neighbors with spine at 192.168.1.2 and advertises 100.64.0.2/32
    sudo rm ../leafB/frr.conf || true
    go run generate_leaf_config/generate_leaf_config.go \
        -leaf leafB -neighbor 192.168.1.2 -network 100.64.0.2/32 $REDISTRIBUTE_FLAG \
        -template generate_leaf_config/frr_template/frr.conf.template

    popd
}

generate_leaf_configs