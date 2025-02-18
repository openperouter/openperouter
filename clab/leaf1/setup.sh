#!/bin/bash
#

# VTEP IP
ip addr add 100.64.0.1/32 dev lo

# Leaf - spine leg
ip addr add 192.168.1.1/24 dev eth1

# L3 VRF
ip link add red type vrf table 1100

# Leaf - host leg
ip link set eth2 master red
ip addr add 192.168.10.2/24 dev eth2

ip link set red up
ip link add br100 type bridge
ip link set br100 master red addrgenmode none
ip link set br100 addr aa:bb:cc:00:00:65
ip link add vni100 type vxlan local 100.64.0.1 dstport 4789 id 100 nolearning
ip link set vni100 master br100 addrgenmode none
ip link set vni100 type bridge_slave neigh_suppress on learning off
ip link set vni100 up
ip link set br100 up
