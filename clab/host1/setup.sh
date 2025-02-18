#!/bin/bash
#

ip addr add 192.168.10.1/24 dev eth1

# set the default gw via eth1
ip r del default
ip r add default via 192.168.10.2
