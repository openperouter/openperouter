#!/bin/bash
#

# set the default gw via eth1
ip r del default
ip r add default via 192.169.21.1

# set the IPv6 default gw via eth1
ip -6 r del default
ip -6 r add default via 2001:db8:169:21::1
