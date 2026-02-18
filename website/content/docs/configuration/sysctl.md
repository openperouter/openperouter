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
controller reconciles the network configuration and are required for correct
traffic forwarding and fast failover during EVPN operations.

No manual intervention is needed — the controller sets them for you. This
page documents what each setting does, why it is required, and which kernel
versions are needed.

## Configured Sysctls

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
MAC/IP (Type-2) route advertisement, especially during virtual machine live
migrations. Without it, the new host may not learn the migrated VM's MAC
address promptly, causing traffic black-holing until the next regular ARP
exchange.

The `all` variant applies to every existing interface; the `default` variant
ensures that any interface created after the sysctl is set inherits the same
behavior.

### Accept Untracked NA (IPv6)

| Sysctl | Value |
|--------|-------|
| `net.ipv6.conf.all.accept_untracked_na` | `1` |
| `net.ipv6.conf.default.accept_untracked_na` | `1` |

`accept_untracked_na` is the IPv6 counterpart of `arp_accept`. It lets the
kernel create neighbor entries from unsolicited Neighbor Advertisement (NA)
packets. This is required for fast EVPN MAC/IP route advertisement with
IPv6 addresses, following the same rationale as `arp_accept` for IPv4.

#### Kernel Requirement

The `accept_untracked_na` sysctl was introduced in **Linux kernel 5.18**. On
older kernels the corresponding `/proc/sys/` file does not exist.

#### Behavior on Older Kernels

When the controller detects that the proc file for `accept_untracked_na` is
missing, it **skips the setting with a warning** instead of failing. The
controller and the rest of the sysctl configuration continue to work
normally.

However, running on a kernel older than 5.18 has the following consequence:

- **IPv6 layer 2 traffic might be impacted / mac learning might 
  be slowerdowntime.** Because the kernel cannot learn the migrated VM's IPv6
  address from unsolicited NA packets, the EVPN Type-2 route for the new
  location is not advertised promptly. Traffic directed at the VM's IPv6
  address may be black-holed until regular Neighbor Discovery catches up.

IPv4 traffic is **not affected** by this limitation since the `arp_accept`
sysctl is available on all supported kernel versions.

If you run EVPN workloads that rely on IPv6 and require fast failover
during live migrations, ensure your nodes run **kernel 5.18 or later**.

## Summary Table

| Sysctl | Purpose | Min Kernel | Failure Mode on Old Kernel |
|--------|---------|------------|---------------------------|
| `net.ipv4.conf.all.forwarding` | IPv4 packet forwarding | any | N/A |
| `net.ipv6.conf.all.forwarding` | IPv6 packet forwarding | any | N/A |
| `net.ipv4.conf.all.arp_accept` | Learn MACs from Gratuitous ARP | any | N/A |
| `net.ipv4.conf.default.arp_accept` | Inherit `arp_accept` on new interfaces | any | N/A |
| `net.ipv6.conf.all.accept_untracked_na` | Learn MACs from unsolicited NA | 5.18 | Skipped with warning; IPv6 VM migration downtime |
| `net.ipv6.conf.default.accept_untracked_na` | Inherit `accept_untracked_na` on new interfaces | 5.18 | Skipped with warning; IPv6 VM migration downtime |
