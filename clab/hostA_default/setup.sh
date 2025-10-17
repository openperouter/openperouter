#!/bin/bash
#

# set the default gw via eth1
ip r del default
ip r add default via 192.168.22.1
