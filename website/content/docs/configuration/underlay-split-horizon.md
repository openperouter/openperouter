---
weight: 44
title: "Underlay Fabric Transit Protection"
description: "Prevent ToR-learned routes from being advertised back to the fabric"
icon: "article"
date: "2026-11-12"
lastmod: "2026-11-12"
toc: true
---

OpenPERouter prevents routes learned from one external Underlay neighbor from being advertised to another external Underlay neighbor by default. This prevents a node with multiple Top-of-Rack (ToR) uplinks from providing unintended fabric transit. Learned routes remain available for local forwarding, and eligible locally originated routes remain advertised.

Protection applies in every address family enabled on the external neighbors: IPv4 unicast, IPv6 unicast, L2VPN EVPN, IPv4 VPN, and IPv6 VPN. A neighbor is external when it uses `type: External` or its numeric ASN differs from the Underlay ASN. Addressed, interface-based, and dynamic listen-range neighbors use the same rules.

## Default behavior

Protection applies automatically to every Underlay, including Underlays with RR clients. No additional field or per-neighbor configuration is required.

## Route reflectors and internal peers

Configuring `spec.routeReflector` does not disable external-to-external protection. The per-address-family `routeReflectorClient` property controls iBGP reflection. Client-learned routes may be reflected to clients and non-clients; non-client-learned routes may be reflected to clients. Non-client-to-non-client reflection remains prohibited.

Transit rejection applies only to external neighbors. Internal peers use a stripping policy and retain existing reflection eligibility and loop prevention. See the [route reflector guide]({{< ref "route-reflector.md" >}}) for configuration.

Eligible internal-to-external and external-to-internal advertisements remain available in every family. The fabric marker is removed on every permitted BGP egress. A route received through an internal peer is therefore not blocked merely because it originated in another fabric elsewhere. Route reflectors enable outbound policy on reflected routes so the marker stays local to each router.

## Generated policy and custom configuration

OpenPERouter adds the reserved standard community `65432:10` to routes received from external Underlay neighbors, preserving existing communities. External outbound policy rejects routes carrying that marker. Every permitted BGP egress removes only the reserved marker, including advertisements to internal Underlay peers and tenant/host peers. Other standard communities and extended communities, including route targets, are preserved. Locally originated routes remain eligible for export, and next hops are not changed by this policy.

The community `65432:10`, community-list `openpe-fabric`, and route maps `openpe-fabric-in`, `openpe-fabric-out`, and `openpe-fabric-strip` are reserved for managed policy. Do not use the reserved community for application policy. A `RawFRRConfig` that redefines these maps or replaces their neighbor bindings overrides the native protection. Arbitrary raw route maps are not automatically composed with managed policy.
