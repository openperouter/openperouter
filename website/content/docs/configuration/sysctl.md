---
weight: 55
title: "Sysctl Configuration"
description: "Kernel sysctl settings managed by OpenPERouter"
icon: "article"
date: "2026-02-16T10:00:00+02:00"
lastmod: "2026-02-16T10:00:00+02:00"
toc: true
---

OpenPERouter automatically configures several kernel sysctl settings inside
the router's network namespace. These settings are applied every time the
controller reconciles the network configuration and are required for
correct traffic forwarding and fast failover.

No manual intervention is needed — the controller sets them for you. This
page documents what each setting does, why it is required, and which
kernel versions are needed.

## Common Sysctls

The following sysctls are always configured regardless of the overlay
technology in use.

### IP Forwarding

| Sysctl | Value |
|--------|-------|
| `net.ipv4.conf.all.forwarding` | `1` |
| `net.ipv6.conf.all.forwarding` | `1` |

IP forwarding must be enabled for the router namespace to forward traffic
between interfaces. Without these settings, packets received on one
interface cannot be routed to another and the router cannot function.

### ARP Accept (IPv4)

| Sysctl | Value |
|--------|-------|
| `net.ipv4.conf.all.arp_accept` | `1` |
| `net.ipv4.conf.default.arp_accept` | `1` |

Enabling `arp_accept` allows the kernel to create neighbor table entries
from received Gratuitous ARP packets. This is critical for fast EVPN
MAC/IP (Type-2) route advertisement, especially during virtual machine
live migrations. Without it, the new host may not learn the migrated VM's
MAC address promptly, causing traffic black-holing until the next regular
ARP exchange.

The `all` variant applies to every existing interface; the `default`
variant ensures that any interface created after the sysctl is set inherits
the same behavior.

### Accept Untracked NA (IPv6)

| Sysctl | Value |
|--------|-------|
| `net.ipv6.conf.all.accept_untracked_na` | `1` |
| `net.ipv6.conf.default.accept_untracked_na` | `1` |

`accept_untracked_na` is the IPv6 counterpart of `arp_accept`. It lets
the kernel create neighbor entries from unsolicited Neighbor Advertisement
(NA) packets. This is required for fast EVPN MAC/IP route advertisement
with IPv6 addresses, following the same rationale as `arp_accept` for
IPv4.

#### Kernel Requirement

The `accept_untracked_na` sysctl was introduced in **Linux kernel 5.18**.
On older kernels the corresponding `/proc/sys/` file does not exist.

#### Behavior on Older Kernels

When the controller detects that the proc file for `accept_untracked_na`
is missing, it **skips the setting with a warning** instead of failing.
The controller and the rest of the sysctl configuration continue to work
normally.

However, running on a kernel older than 5.18 has the following
consequence:

- **IPv6 layer 2 traffic might be impacted / mac learning might be
  slower.** Because the kernel cannot learn the migrated VM's IPv6 address
  from unsolicited NA packets, the EVPN Type-2 route for the new location
  is not advertised promptly. Traffic directed at the VM's IPv6 address
  may be black-holed until regular Neighbor Discovery catches up.

IPv4 traffic is **not affected** by this limitation since the `arp_accept`
sysctl is available on all supported kernel versions.

If you run EVPN workloads that rely on IPv6 and require fast failover
during live migrations, ensure your nodes run **kernel 5.18 or later**.

## SRv6 Sysctls

The following sysctls are only configured when SRv6 is enabled on the
Underlay.

### SRv6 Segment Routing (seg6)

| Sysctl | Value |
|--------|-------|
| `net.ipv6.conf.all.seg6_enabled` | `1` |
| `net.ipv6.seg6_flowlabel` | `1` |

`seg6_enabled` enables Segment Routing over IPv6 (SRv6) on all network
interfaces, allowing them to process and forward packets with SRv6
segment routing headers. This is required for the kernel to accept and
process SRv6 encapsulated traffic.

`seg6_flowlabel` set to `1` instructs the kernel to compute the IPv6 flow
label using `seg6_make_flowlabel()`. This improves ECMP load balancing
for SRv6 traffic by generating flow labels that reflect the inner packet
headers.

### VRF Strict Mode

| Sysctl | Value |
|--------|-------|
| `net.vrf.strict_mode` | `1` |

VRF strict mode ensures that each VRF is associated with a unique routing
table. Without this setting, multiple VRFs could inadvertently share a
routing table, causing BGP routes to be rejected (visible as `B>r` in FRR
output). This sysctl is set each time a new VRF is created for an L3VPN.

### Disable Reverse Path Filter

| Sysctl | Value |
|--------|-------|
| `net.ipv4.conf.<vrf>.rp_filter` | `0` |

Reverse path filtering (`rp_filter`) is disabled on each VRF interface
created for SRv6 L3VPNs. This is necessary because SRv6 decapsulated
traffic may have source addresses that do not match the local routing
table in the VRF, causing the kernel to silently drop legitimate packets.

## Summary Table

| Sysctl | Purpose | When | Min Kernel | Failure Mode on Old Kernel |
|--------|---------|------|------------|---------------------------|
| `net.ipv4.conf.all.forwarding` | IPv4 packet forwarding | Always | any | N/A |
| `net.ipv6.conf.all.forwarding` | IPv6 packet forwarding | Always | any | N/A |
| `net.ipv4.conf.all.arp_accept` | Learn MACs from Gratuitous ARP | Always | any | N/A |
| `net.ipv4.conf.default.arp_accept` | Inherit `arp_accept` on new interfaces | Always | any | N/A |
| `net.ipv6.conf.all.accept_untracked_na` | Learn MACs from unsolicited NA | Always | 5.18 | Skipped with warning; IPv6 MAC learning slower |
| `net.ipv6.conf.default.accept_untracked_na` | Inherit `accept_untracked_na` on new interfaces | Always | 5.18 | Skipped with warning; IPv6 MAC learning slower |
| `net.ipv6.conf.all.seg6_enabled` | Enable SRv6 on all interfaces | SRv6 | N/A | N/A |
| `net.ipv6.seg6_flowlabel` | SRv6 flow label for ECMP | SRv6 | N/A | N/A |
| `net.vrf.strict_mode` | Unique routing table per VRF | SRv6 | N/A | N/A |
| `net.ipv4.conf.<vrf>.rp_filter` | Allow SRv6 decapsulated traffic | SRv6 | any | N/A |
