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


# Leaf - host leg
ip link add green type vrf table 1101
ip link set eth3 master green
ip addr add 192.168.10.2/24 dev eth3

ip link set green up
ip link add br200 type bridge
ip link set br200 master green addrgenmode none
ip link set br200 addr aa:bb:cc:00:00:67
ip link add vni200 type vxlan local 100.64.0.1 dstport 4789 id 200 nolearning
ip link set vni200 master br200 addrgenmode none
ip link set vni200 type bridge_slave neigh_suppress on learning off
ip link set vni200 up
ip link set br200 up
