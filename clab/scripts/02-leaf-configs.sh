#!/bin/bash
# Generate leaf configurations
set -euo pipefail

source "$(dirname $(readlink -f $0))/../common.sh"

generate_leaf_configs() {
    echo "Generating leaf configurations..."

    pushd "$(dirname $(readlink -f $0))/../tools"

    # Build the generators locally in generate_leaf_config directory
    echo "Building config generators..."
    go build -o generate_leaf_config/generate_leaf generate_leaf_config/common.go generate_leaf_config/generate_leaf.go
    go build -o generate_leaf_config/generate_leafkind generate_leaf_config/common.go generate_leaf_config/generate_leafkind.go

    # Build the command with redistribute parameter (disabled by default)
    local redistribute_args=()
    if [[ "${DEMO_MODE:-false}" == "true" ]]; then
        echo "Enabling redistribute connected from VRFs (demo mode)"
        redistribute_args+=(-redistribute-connected-from-vrfs -redistribute-connected-from-default)
    else
        echo "Disabling redistribute connected from VRFs (default)"
    fi

    # Generate configs for original leafs only
    if [[ "${SRV6:-false}" == "true" ]]; then
        generate_leaf_configs_srv6 "${redistribute_args[@]}"
    else
        generate_leaf_configs_evpn "${redistribute_args[@]}"
    fi

    # Generate configs for leafkind switches
    # leafkind1: ASN 64512, spine at 192.168.1.4, listen ranges 192.168.11.0/24 (IPv4) and 2001:db8:11::/64 (IPv6)
    rm -f ../singlecluster/leafkind1/frr.conf
    ./generate_leaf_config/generate_leafkind \
        -leaf singlecluster/leafkind1 -asn 64512 -spine-ip 192.168.1.4 \
        -ipv4-listen-range 192.168.11.0/24 -ipv6-listen-range 2001:db8:11::/64 \
        -template generate_leaf_config/frr_template/leafkind.conf.template

    # leafkind2: ASN 64513, spine at 192.168.1.6, listen ranges 192.168.12.0/24 (IPv4) and 2001:db8:12::/64 (IPv6)
    rm -f ../singlecluster/leafkind2/frr.conf
    ./generate_leaf_config/generate_leafkind \
        -leaf singlecluster/leafkind2 -asn 64513 -spine-ip 192.168.1.6 \
        -ipv4-listen-range 192.168.12.0/24 -ipv6-listen-range 2001:db8:12::/64 \
        -template generate_leaf_config/frr_template/leafkind.conf.template

    popd
}

generate_leaf_configs_srv6() {
    rm -f ../leafA/frr.conf
    ./generate_leaf_config/generate_leaf \
        -leaf leafA \
        -router-id 100.64.0.1 \
        -update-source "2001:db8:1234::1" \
        -srv6-prefix fd00:0:10::/48 \
        -isis-net "49.0001.0000.0000.0001.00" \
        "$@" \
        -template generate_leaf_config/frr_template/frr.srv6.conf.template

    rm -f ../leafB/frr.conf
    ./generate_leaf_config/generate_leaf \
        -leaf leafB \
        -router-id 100.64.0.2 \
        -update-source "2001:db8:1234::2" \
        -srv6-prefix fd00:0:11::/48 \
        -isis-net "49.0001.0000.0000.0002.00" \
        "$@" \
        -template generate_leaf_config/frr_template/frr.srv6.conf.template
}

generate_leaf_configs_evpn() {
    # leafA neighbors with spine at 192.168.1.0 and advertises 100.64.0.1/32
    rm -f ../leafA/frr.conf
    ./generate_leaf_config/generate_leaf \
        -leaf leafA -neighbor 192.168.1.0 -network 100.64.0.1/32 "$@" \
        -template generate_leaf_config/frr_template/frr.conf.template

    # leafB neighbors with spine at 192.168.1.2 and advertises 100.64.0.2/32
    rm -f ../leafB/frr.conf
    ./generate_leaf_config/generate_leaf \
        -leaf leafB -neighbor 192.168.1.2 -network 100.64.0.2/32 "$@" \
        -template generate_leaf_config/frr_template/frr.conf.template
}

generate_leaf_configs
