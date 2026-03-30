#!/bin/bash

# Avoid losing IPv6 address after enslaving to VRF
sysctl -w net.ipv6.conf.all.keep_addr_on_down=1

# VTEP IP
ip addr add 100.64.0.1/32 dev lo

# VRF: blue
ip link add blue type vrf table 1100
ip link set ethblue master blue
ip link set blue up

ip link add br1 type bridge
ip link set br1 master blue addrgenmode none
ip link set br1 addr 02:22:35:69:61:43
ip link add vni200 type vxlan local 100.64.0.1 dstport 4789 id 200 nolearning
ip link set vni200 master br1 addrgenmode none
ip link set vni200 type bridge_slave neigh_suppress on learning off
ip link set vni200 up
ip link set br1 up

# VRF: red
ip link add red type vrf table 1101
ip link set ethred master red
ip link set red up

ip link add br2 type bridge
ip link set br2 master red addrgenmode none
ip link set br2 addr 02:5c:4d:3c:ac:aa
ip link add vni100 type vxlan local 100.64.0.1 dstport 4789 id 100 nolearning
ip link set vni100 master br2 addrgenmode none
ip link set vni100 type bridge_slave neigh_suppress on learning off
ip link set vni100 up
ip link set br2 up
