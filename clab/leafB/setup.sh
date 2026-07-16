#!/bin/bash
#

# this is to avoid to loose the ipv6 address after enslaving to the vrf
sysctl -w net.ipv6.conf.all.keep_addr_on_down=1

# VTEP IP
ip addr add 100.64.0.2/32 dev lo

# L3 VRF
ip link add red type vrf table 1100

# Leaf - host leg
ip link set ethred master red

ip link set red up
ip link add br100 type bridge
ip link set br100 master red addrgenmode none
ip link set br100 addr aa:bb:cc:00:00:67
ip link add vni100 type vxlan local 100.64.0.2 dstport 4789 id 100 nolearning
ip link set vni100 master br100 addrgenmode none
ip link set vni100 type bridge_slave neigh_suppress on learning off
ip link set vni100 up
ip link set br100 up

# L3 VRF
ip link add blue type vrf table 1101

# Leaf - host leg
ip link set ethblue master blue

ip link set blue up
ip link add br200 type bridge
ip link set br200 master blue addrgenmode none
ip link set br200 addr aa:bb:cc:00:00:68
ip link add vni200 type vxlan local 100.64.0.2 dstport 4789 id 200 nolearning
ip link set vni200 master br200 addrgenmode none
ip link set vni200 type bridge_slave neigh_suppress on learning off
ip link set vni200 up
ip link set br200 up

# IPv6 VTEP IP
ip addr add fd00:64::2/128 dev lo

# L3 VRF (no host leg -- used for IPv6 VTEP testing only)
ip link add green type vrf table 1102

ip link set green up
ip link add br300 type bridge
ip link set br300 master green addrgenmode none
ip link set br300 addr aa:bb:cc:00:00:6a
ip link add vni300 type vxlan local fd00:64::2 dstport 4789 id 300 nolearning
ip link set vni300 master br300 addrgenmode none
ip link set vni300 type bridge_slave neigh_suppress on learning off
ip link set vni300 up
ip link set br300 up
