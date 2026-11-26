#!/bin/bash
set -euo pipefail

case "$(hostname)" in
    leafkind1)
        vtep=192.0.2.11
        router_mac=02:00:00:00:00:01
        address=198.18.21.1/24
        ;;
    leafkind2)
        vtep=192.0.2.12
        router_mac=02:00:00:00:00:02
        address=198.18.22.1/24
        ;;
    *)
        echo "Unsupported leaf hostname: $(hostname)" >&2
        exit 1
        ;;
esac

# The split-horizon EVPN and SRv6 tests share this permanent VRF on both leaves.
# Tests only swap FRR configuration: EVPN binds the VRF to its L3VNI and exports
# leafkind1's connected prefix, while SRv6 exports VPN routes from the same VRF.
# Normal leafkind FRR configuration does not enable either export.
ip addr add "${vtep}/32" dev lo
ip link add sh-evpn type vrf table 1103
ip link set sh-evpn up
ip link add br4091 type bridge
ip link set br4091 master sh-evpn addrgenmode none
ip link set br4091 address "${router_mac}"
ip link add vni4091 type vxlan local "${vtep}" dstport 4789 id 4091 nolearning
ip link set vni4091 master br4091 addrgenmode none
ip link set vni4091 type bridge_slave neigh_suppress on learning off
ip link set vni4091 up
ip link set br4091 up
ip link add sh-evpn-host type dummy
ip link set sh-evpn-host master sh-evpn
ip addr add "${address}" dev sh-evpn-host
ip link set sh-evpn-host up

# SRv6 VPN SID installation requires strict mode; creating VRFs resets it.
sysctl -w net.vrf.strict_mode=1
