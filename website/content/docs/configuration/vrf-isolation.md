---
title: "Tenant VRF defaults and SRv6 routing"
description: "Unreachable defaults and scoped underlay lookups for Linux tenant VRFs"
toc: true
---

OpenPERouter installs IPv4 and IPv6 unreachable defaults in Linux tenant VRFs
managed by L3VNI (VXLAN/EVPN), and in SRv6 L3VPN VRFs with `routingIsolation`
configured. An unknown tenant destination stops in that table instead of
falling through to main. Working FRR routes, including defaults, still win.

## Configuration

EVPN protection is automatic. SRv6 needs an explicit encapsulation source and
underlay exceptions because its outer packet may perform another route lookup
in the tenant VRF. An unreachable default can otherwise block that lookup.
SRv6 on a node does not disable protection in other EVPN VRFs.

```yaml
# Underlay.spec.srv6, alongside locator and transport configuration
encapsulationSource: 2001:db8:ffff::1
```

```yaml
# L3VPN.spec
vrf: red
routingIsolation:
  underlayTable: 254
  remoteLocatorPrefixes:
    - fc00:20::/48
```

`encapsulationSource` is the actual namespace SRv6 tunnel source, set through
SEG6 generic netlink and read back. It is not inferred from router ID, VTEP IP
or node address. The generated FRR encapsulation source matches this setting,
so zebra retains it after restart; BGP update-source continues to use the tunnel
endpoint independently. Use a source valid for the underlay and configure normal
underlay addressing/routing independently. The source is namespace-wide;
selected protected L3VPNs must share it and the underlay table. Use per-node
Underlays/node selectors when sources differ. The source is used only with
`routingIsolation`: after resource filtering, reconciliation omits the explicit
source when no isolated L3VPN remains, allowing withdrawal and isolation cleanup.
The stored Underlay is unchanged; a surviving isolated VPN still requires a valid
explicit source. Admission permits creating the Underlay and its VPNs separately.

`underlayTable` is explicit: main is 254, but per-VRF SID operation can use a
dedicated table. It must not be owned by any existing VRF (including a removed tenant), default (253), local
(255), or unspecified (0). It is excluded when pending tenant VRF table IDs
are allocated. Actual VRF tables are read from their Linux devices, independently
of VNI. Locator prefixes are explicit canonical non-default IPv6 prefixes;
FRR locator discovery is not performed. Prefixes overlapping IPv6 link-local
or multicast space are rejected, including broader prefixes with a unicast base.

Sharing an L3VNI and L3VPN tenant VRF requires `routingIsolation` and compatible
BGP ASNs on both configurations. Generated FRR configuration and Linux tests
support simultaneous EVPN/VXLAN and VPN/SRv6 directions in that VRF.
Legacy SRv6 without `routingIsolation` keeps its previous behavior, without
these guards/exceptions, and cannot share a guarded L3VNI VRF. Legacy L3VPNs
and isolated L3VPNs using a dedicated underlay table cannot coexist in one
namespace: the shared `iif lo` lookup cannot identify the originating VPN.
Use main-table exceptions, isolate every SRv6 VPN in that namespace, or use
separate namespaces. Generic VRFs,
underlay VRFs, passthrough and Grout do not acquire guards from this mechanism.

## Rule semantics and isolation boundary

An exception matches all of:

- The configured outer source `/128`.
- A configured locator destination prefix.
- Ingress tenant VRF, or `lo` for locally originated encapsulation.

It looks up the configured underlay table before IPv6 l3mdev lookup. A matching
terminal unreachable action follows the lookup. Packet tests demonstrate that,
without this action, an outer packet can take an unrelated working tenant
default when the selected underlay table has no route. The terminal action
prevents that fallback; it has the same narrow selectors as its lookup.

**Locator prefixes define where an exception applies. They are not a firewall
allowlist.** There is no general source-only deny or policy forbidding other
SRv6 destinations. Destinations outside those prefixes follow ordinary Linux
routing. Configure the prefixes to cover the locators used by the transport.

**Source/destination rules do not authenticate packet origin.** A tenant that
can forge the infrastructure source can match an exception and reach underlay
routing. This implementation provides no anti-spoofing filtering and makes no
claim to protect against that case. If tenant isolation relies on the source
being exclusive to the PE, existing network controls must enforce that at all
client inputs: local ports, VXLAN inner traffic and SRv6-decapsulated traffic.
IPv4 rp_filter alone does not establish IPv6 source authenticity. Processes
inside the router namespace are also within the operational trust boundary.

For ordinary tenant sources that do not match an exception, unknown destinations
remain blocked by the tenant unreachable default. Higher-priority administrator
rules can deliberately alter this behavior and are outside this guarantee.

## Supported SRv6 modes

The following modes are exercised with Linux 6.8 and FRR 10.6.0:

| SID allocation | Underlay table | Status |
| --- | --- | --- |
| Per-VRF End.DT46 | main | Supported |
| Per-VRF End.DT46 | Dedicated table | Supported |
| Per-family uDT4/uDT6, legacy End.DT6 | main | Supported |
| Per-family uDT4/uDT6, legacy End.DT6 | Dedicated table | Rejected |

Both H.Encaps and H.Encaps.Red are tested for ordinary IPv4/IPv6 tenant traffic
and IPv6 decapsulation followed by re-encapsulation. FRR 10.6 represents legacy
End.DT6 using `SEG6_LOCAL_TABLE`; it preserves the underlay ingress device.
A repeated encapsulation then does not match the VRF/lo exception, and uses
main. A real-packet negative control demonstrates delivery through main even
when the selected dedicated table has no locator route. This combination is
rejected by API validation and by inspection of actual kernel SID actions.
Use per-VRF End.DT46 for a dedicated table. Main does not need a separate
locator-allowlist restriction, so legacy per-family allocation is allowed there.

Re-test these behaviors when changing kernel or FRR versions. A cached LWT
underlay destination can mask the original guard conflict; the regression
control fully restarts its FRR instances with guards installed before checking
an uncached lookup. A general IPv6 route-cache flush or a quick route
withdraw/re-advertisement did not reliably reset retained LWT/nexthop state. Production reconciliation does not force ports
or VRFs down/up to invalidate caches.

## Ownership, lifecycle and FRR

The guard identity is the actual tenant table and address family, default
prefix, type `unreachable`, metric **4278198272** (`0xff002000`) and protocol
**242**. The metric follows the [Linux VRF documentation](https://docs.kernel.org/networking/vrf.html).
FRR 10.6 treats protocol 242 as an external kernel route and interprets the
metric as administrative distance **255**, metric **8192**. More specific
routes and ordinary working defaults take precedence. After withdrawal the
guard remains. No staticd or FRR Null0 configuration is required.

Guards use exclusive netlink `RouteAdd`, never `RouteReplace`. An EEXIST race
is accepted only after checking the complete ownership tuple. Foreign routes,
including defaults at the same metric, are not overwritten or deleted.
A conflict returns an error. No new tenant ports are attached after a failed
provisioning step; existing interfaces are not forcibly disabled. A guard
provisioning failure retains the desired existing EVPN links in cleanup keep
sets, while preventing new attachments to that VRF. Namespace-wide policy errors
remain fatal; a guard error in one VRF does not stop healthy VRFs. Other obsolete/failed attachments
keep their normal cleanup semantics.

Rule ownership uses protocol 242, IPv6 source `/128`, non-default destination
prefix, ingress selector, action and table. Do not use this protocol/tuple for
administrator rules. Desired rules are deduplicated, including shared `iif lo`
rules; a shared rule remains until its last configuration is removed.
Adjacent priority pairs are allocated deterministically starting at **1**,
before the actual l3mdev rule. Matching existing pairs retain their priorities,
including shared local-output rules. New managed VRFs carry the
`openperouter:tenant-vrf` link alias. An obsolete owner of a requested underlay
table can be removed before retry only when this alias confirms ownership; desired
or unmarked owners remain conflicts. Older VRFs without the marker must first be
removed through normal configuration cleanup. A foreign priority conflict,
missing l3mdev rule for nonempty desired state, or insufficient space returns
an error. Foreign rules are never moved or overwritten. Before introducing
guards on an already forwarding legacy VPN, reconciliation validates priorities
and installs and verifies its exceptions and encapsulation source. A priority
conflict therefore leaves its existing forwarding unchanged. Planning may
create missing VRFs, but does not admit their tenant interfaces.

Normal configuration reconciliation adds missing owned objects and removes
only obsolete owned rules. Terminal actions are installed before lookups;
obsolete lookups are removed before their terminal actions. Failures are
reported, and retries operate on actual kernel state. Updates do not perform
forced down/up, and healthy idempotent reconciliation does not rewrite working
routes or rules. An empty desired set removes owned exceptions and resets the
namespace tunnel source only if it still equals an owned previous source.
Empty-state cleanup ignores foreign rules, even those using protocol 242 outside
our ownership predicate; installing new rules still reports those collisions.
An empty initial set requires neither SEG6, nftables nor an existing l3mdev
rule. There is no separate periodic repair controller; external changes are
repaired on the next normal configuration reconciliation.

IPv6 guards are skipped when IPv6 is unavailable or disabled throughout the
namespace, including defaults for future interfaces. Individual interface
overrides are checked: neither value of `all.disable_ipv6` alone establishes
IPv6 availability; the default and individual device settings decide it. IPv4
continues working when IPv6 is disabled. Failure to read the settings is an
error. nftables is not a dependency.

Generated tenant BGP configuration uses explicit network statements/host BGP
sessions and EVPN unicast advertisement, without kernel redistribution. Guards
therefore do not originate in BGP VPN/EVPN; legitimate advertisements remain.
No existing redistribution is globally disabled. Custom RawFRRConfig that adds
kernel redistribution must selectively exclude the guards itself.

## Validation

Unit tests cover ownership, collisions, metric encoding, IPv6 availability,
validation, deterministic rule priorities, interrupted dump retries and empty-state
cleanup. Guard removal obtains both family snapshots before deleting any route.
A retained VRF sharing the table keeps its guards when another VRF is deleted.
A guard dump/removal error stops final uplink restoration, allowing teardown to
retry on the next reconciliation. Guard checks reuse a family snapshot within
each setup pass; an exclusive-add collision always triggers a fresh read. A Linux
regression calls `KernelDatapathConfigurator.Configure` in clean isolated
namespaces and verifies the first EVPN VRF, repeated configuration and disabled
IPv6. It reproduces the previous missing-l3mdev failure through the main path.

The privileged hostnetwork suite keeps real FRR tests for IPv4/IPv6 forwarding,
unknown tenant traffic with a working main control path, specific/default
routes, withdrawal, idempotence and full FRR restart. It checks actual
advertised routes with positive controls for legitimate prefixes. A third
EVPN-only PE proves simultaneous VXLAN and SRv6 directions in one VRF, with
per-destination wire capture, independent withdrawals and recovery. Separate
packet controls reproduce the original guard conflict, verify the need for
terminal failure, and establish the legacy dedicated-table limitation.

Build the Linux binary, then run in an isolated FRR container:

```sh
GOOS=linux CGO_ENABLED=0 go test -tags=runasroot -c ./internal/hostnetwork -o /tmp/hostnetwork.test
docker run --rm --privileged --network none \
  -v /tmp/hostnetwork.test:/tests/hostnetwork.test:ro \
  --entrypoint /tests/hostnetwork.test quay.io/frrouting/frr:10.6.0 \
  -test.v -ginkgo.focus='Minimal SRv6 workaround|SRv6 dataplane|Simultaneous EVPN|SRv6 exception|EVPN VRF default guards'
```

The cluster suite `Tenant VRF routing isolation` uses the disposable Kind and
ContainerLab environment from `make deploy`. It creates the Underlay, L3VNI,
L3VPN and L2VNI resources through Kubernetes and sends IPv4/IPv6 packets from
a tenant pod. Tenant resources select its node to give the fabric one return
path to that LAN. Each transport case starts with fresh router pods; this suite
does not establish recovery of a long-lived zebra after VRF recreation.
EVPN-only, guarded SRv6 and a shared tenant VRF cover working
specific/default routes, independent withdrawals, locator selector updates and
legitimate exports without guard advertisements. A separate namespace attached
to the router supplies a reachable main-table control destination; pass-through
TC counters on its veth detect leaked requests even when replies could not return
to the pod.
The fixture temporarily disables strict IPv4 reverse-path filtering on the
disposable leaf's `all` and tenant VRF settings to accept asymmetric VPN traffic,
then restores their previous values. It does not change host sysctls.
The suite cleans router resources and must run in a disposable test cluster.

```sh
make e2etests GINKGO_ARGS="--label-filter='!systemdmode && !grout-only' --focus='Tenant VRF routing isolation'"
```

A skip caused by missing FRR/kernel capabilities is not validation. These tests
establish only the exercised Linux/FRR topologies; full Kubernetes/hostmode e2e,
other kernels and independent zebra-only restart behavior require separate
checks. No availability claim is based on recovery after an artificial VRF
interface down/up cycle.
