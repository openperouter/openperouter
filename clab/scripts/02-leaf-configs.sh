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
        REDISTRIBUTE_FLAG="-redistribute-connected-from-vrfs -redistribute-connected-from-default"
        echo "Enabling redistribute connected from VRFs (demo mode)"
    else
        echo "Disabling redistribute connected from VRFs (default)"
    fi

    # Generate configs for original leafs only
    if $SRV6 ; then
        rm -f ../leafA/frr.conf || true
        go run generate_leaf_config/generate_leaf_config.go \
            -leaf leafA \
            -router-id 100.64.0.1 \
            -update-source "2001:db8:1234::1" \
            -srv6-prefix fd00:0:10::/48 \
            -isis-net "49.0001.0000.0000.0001.00" \
            $REDISTRIBUTE_FLAG \
            -template generate_leaf_config/frr_template/frr.srv6.conf.template

        rm -f ../leafB/frr.conf || true
        go run generate_leaf_config/generate_leaf_config.go \
            -leaf leafB \
            -router-id 100.64.0.2 \
            -update-source "2001:db8:1234::2" \
            -srv6-prefix fd00:0:11::/48 \
            -isis-net "49.0001.0000.0000.0002.00" \
            $REDISTRIBUTE_FLAG \
            -template generate_leaf_config/frr_template/frr.srv6.conf.template
    else
        # leafA neighbors with spine at 192.168.1.0 and advertises 100.64.0.1/32
        rm -f ../leafA/frr.conf || true
        go run generate_leaf_config/generate_leaf_config.go \
            -leaf leafA -neighbor 192.168.1.0 -network 100.64.0.1/32 $REDISTRIBUTE_FLAG \
            -template generate_leaf_config/frr_template/frr.conf.template

        # leafB neighbors with spine at 192.168.1.2 and advertises 100.64.0.2/32
        rm -f ../leafB/frr.conf || true
        go run generate_leaf_config/generate_leaf_config.go \
            -leaf leafB -neighbor 192.168.1.2 -network 100.64.0.2/32 $REDISTRIBUTE_FLAG \
            -template generate_leaf_config/frr_template/frr.conf.template
    fi

    popd
}

generate_leaf_configs
