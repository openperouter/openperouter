#!/bin/sh
set -eu

apk add --no-cache dnsmasq

while ! ip link show eth1 >/dev/null 2>&1; do
  sleep 0.1
done

ip addr add 192.168.11.1/24 dev eth1
ip link set eth1 up

exec dnsmasq \
  --no-daemon \
  --interface=eth1 \
  --bind-interfaces \
  --dhcp-range=192.168.11.100,192.168.11.150,255.255.255.0,2m \
  --dhcp-option=option:router,192.168.11.1 \
  --no-resolv \
  --no-hosts \
  --log-dhcp
