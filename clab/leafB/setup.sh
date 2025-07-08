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
