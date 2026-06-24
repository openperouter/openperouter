#!/bin/sh
# Dev-env DHCP server for the NAD-backed underlay with dhcp IPAM.
#
# Attaches to the leafkind1-sw bridge (the shared 192.168.11.0/24 underlay
# segment) and serves leases to the macvlan underlay interfaces created in the
# router netns of each kind node. The pool avoids the statically-addressed hosts
# on the segment (leaf .2, and the .250 we take here); the leaf's BGP listen
# range (192.168.11.0/24) accepts whatever address a node leases.
#
# Note: this installs dnsmasq via apk at start (needs outbound internet from the
# node). Swap to a baked dnsmasq image for an offline/repeatable setup.
set -ex

apk add --no-cache dnsmasq iproute2

# containerlab wires eth1 shortly after the container starts; wait for it.
for _ in $(seq 1 30); do
  ip link show eth1 >/dev/null 2>&1 && break
  sleep 1
done

ip addr replace 192.168.11.250/24 dev eth1
ip link set eth1 up

# -d: run in foreground and log to stderr (visible via `docker logs`).
# --port=0: DHCP only, no DNS. --dhcp-option=3: send no gateway (on-link /24).
# Short 2m lease makes renew/restart behaviour easy to observe in the PoC.
exec dnsmasq -d --log-dhcp \
  --interface=eth1 --bind-interfaces --port=0 \
  --dhcp-authoritative \
  --dhcp-range=192.168.11.100,192.168.11.150,255.255.255.0,2m \
  --dhcp-option=3
