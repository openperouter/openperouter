#!/bin/bash
#

# add_vrf adds the given VRF and sets net.vrf.strict_mode to 1 right after adding it.
# Note: the sysctl must be set again after bringing up VRFs: https://onvox.net/2024/12/16/srv6-frr/
# If net.vrf.strict_mode is not set to 1, the uDT46 routes will be marked as rejects (B>r).
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

# SRV6
sysctl -w net.ipv6.seg6_flowlabel=1
sysctl -w net.ipv6.conf.all.seg6_enabled=1

# VTEP IP
ip addr add 100.64.0.1/32 dev lo

# L3 VRF

add_vrf red 1100
sysctl -w net.vrf.strict_mode=1

# Leaf - host leg
ip link set ethred master red

ip link set red up
ip link add br100 type bridge
ip link set br100 master red addrgenmode none
ip link set br100 addr aa:bb:cc:00:00:65
ip link add vni100 type vxlan local 100.64.0.1 dstport 4789 id 100 nolearning
ip link set vni100 master br100 addrgenmode none
ip link set vni100 type bridge_slave neigh_suppress on learning off
ip link set vni100 up
ip link set br100 up

# L3 VRF
add_vrf blue 1101

# Leaf - host leg
ip link set ethblue master blue

ip link set blue up
ip link add br200 type bridge
ip link set br200 master blue addrgenmode none
ip link set br200 addr aa:bb:cc:00:00:66
ip link add vni200 type vxlan local 100.64.0.1 dstport 4789 id 200 nolearning
ip link set vni200 master br200 addrgenmode none
ip link set vni200 type bridge_slave neigh_suppress on learning off
ip link set vni200 up
ip link set br200 up

# SRV6
ip address add dev lo 2001:db8:1234::1/128
