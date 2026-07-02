#!/bin/bash
#

# add_vrf adds the given VRF and sets net.vrf.strict_mode to 1 right after adding it.
# Note: the sysctl must be set again after bringing up VRFs according to https://onvox.net/2024/12/16/srv6-frr/
# If net.vrf.strict_mode is not set to 1, the uDT46 routes will be marked as rejected with (B>r).
# Further investigation is needed if this is required every time, or after bringing
# up the first VRF only (the setting isn't accessible when no VRFs were ever configured).
add_vrf() {
    local vrf_name
    local table_number
    vrf_name="$1"
    table_number="$2"
    ip link add "${vrf_name}" type vrf table "${table_number}"
    sysctl -w net.vrf.strict_mode=1
}

# this is to avoid to loose the ipv6 address after enslaving to the vrf
sysctl -w net.ipv6.conf.all.keep_addr_on_down=1
sysctl -w net.ipv6.seg6_flowlabel=1
sysctl -w net.ipv6.conf.all.seg6_enabled=1

# VTEP IP
ip addr add 100.65.0.1/32 dev lo

# L3 VRF
add_vrf red 1100

# Leaf - host leg
ip link set ethred master red

ip link set red up
ip link add br100 type bridge
ip link set br100 master red addrgenmode none
ip link set br100 addr aa:bb:cc:00:00:69
ip link set br100 up

# L3 VRF
add_vrf blue 1101

# Leaf - host leg
ip link set ethblue master blue

ip link set blue up
ip link add br200 type bridge
ip link set br200 master blue addrgenmode none
ip link set br200 addr aa:bb:cc:00:00:70
ip link set br200 up

ip address add dev lo 2001:db8:1234::1/128
