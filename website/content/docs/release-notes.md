---
weight: 100
title: "Release Notes"
description: "OpenPERouter release notes"
icon: "article"
date: "2025-06-15T15:03:22+02:00"
lastmod: "2025-06-15T15:03:22+02:00"
toc: true
---

## Release Notes

## Release v0.1.0

### New Features
- Add NodeSelector field to Underlay, L2VNI, L3VNI and L3Passthrough. (#164, @qinqon)
- Add support for the `ovs-bridge` hostmaster type. (#108, @maiqueb)
- Add vtepInterface field to EVPNConfig to allow using an existing interface as VTEP source instead of creating a loopback from vtepCIDR (#214, @qinqon)
- Added a non supported - experimentation only way to inject custom frr configuration. This can be used both to quickly workaround issues and to prototype new features. (#247, @fedepaol)
- Adds probes to the controller, nodemarker, and router components. (#149, @maiqueb)
- Api: refactor L2VNI HostMaster to enable different configurations per bridge type (#176, @maiqueb)
- Be more memory efficient, by only caching the required data for the controller operation (#200, @maiqueb)
- Compatibility with OpenShift clusters (#168, @zeeke)
- Fix SELinux volume write permission on router pods (#269, @zeeke)
- Handle changes in the static configuration files when running in host mode. (#226, @fedepaol)
- Have a way to consume a file based static configuration when running in systemd mode. This is useful to setup the basic connectivity required to the cluster to come up. Additional configuration created via the k8s api will be merged with the static configuration, allowing the creation of secondary network at day 2. (#201, @fedepaol)
- Provide a mechanism to run OpenPERouter on the host as systemd unit via podman. (#158, @fedepaol)
- Support dual stack for l2gatewayips field (#137, @qinqon)

### Bug fixes
- Api: rename l2vni hostmaster type from bridge to linux-bridge (#175, @maiqueb)
- Bump go.opentelemetry.io/otel/sdk to 1.40 to address GO-2026-4394 (#241, @maiqueb)
- Cleanup unamanged ovs bridge veths (#239, @maiqueb)
- Enable the `accept_untracked_na` sysctl in the router network namespaces, to learn MAC mobility events from unsolicited NA messages. (#207, @maiqueb)
- Enable the arp_accept sysctl in the router network namespaces, to learn MAC mobility events from GARPs. (#202, @maiqueb)
- Fix VNI resource cleanup when underlay is deleted along with VNIs (#238, @maiqueb)
- Fix controller container being killed by health check in systemd/host mode when transitioning to K8s API reconciler. (#249, @qinqon)
- Fix ovs missing row errors by using generated code from ovs schema. (#237, @qinqon)
- Fix the metallb example not starting for wrong common.sh path. (#208, @fedepaol)
- Generate linux bridge and ovs bridge manifests with CEL expressions (#219, @qinqon)
- Handle STALE neighbor entries by pinging the corresponding IPs. This still refreshes silent neighbors while forcing really STALE entries to get garbage collected. (#275, @fedepaol)
- Idle workloads become unreachable due to EVPN Type-2 route withdrawal when neighbor entries expire; proactively keep neighbor entries alive via periodic ARP probes. (#211, @maiqueb)
- Only set the L2VNI bridge MAC address when needed, thus preventing type 2 route withdrawal, in turn causing N/S traffic to break permanently (#232, @maiqueb)
- Support L3VNI on CRI-O by enabling always ipv4 ip forwarding (#212, @qinqon)
- Sysctl: accept_untracked_na is now skipped with a warning on kernels < 5.18 instead of blocking host configuration. (#231, @qinqon)
- Update Go from 1.24.0 to 1.24.9 and refresh dependency tree to resolve security vulnerabilities (#166, @qinqon)
- It is now possible to attach multiple L2VNIs to the same IP-VRF for as long as their subnets do not overlap with the L3VNI and other L2VNIs in the same VRF. (#265, @andreaskaris)

### Other (Cleanup or Flake)
- Delete all unused bridges in a single OVSDB transaction. Do not dettach ports of managed bridges before deletion. (#240, @maiqueb)
- Log non recoverable errors in underlay reconciler (#270, @zeeke)
- Modernize golang code. (#268, @qinqon)

## Release v0.0.5

### Bug fixes
- Fix flag `--metrics-bind-address` being ignored on controller and nodemarker binaries (#148, @fdomain)
- Fix: allow omitting underlay NIC configuration when using Multus. (#155, @fdomain)
- Re-introduce the "redistribute-connected-from-default" flag when generating FRR configurations for the KinD leaves (#151, @maiqueb)

## Release v0.0.4

### New Features
- Add a multi-cluster demo setup (#126, @maiqueb)
- Allow pods to run on master nodes or not. (#122, @fedepaol)
- Allow the creation of a "passthrough" veth where the traffic is not being encapsulated but just re-routed by the router.
  This might come handy for those scenarios where we want the host to reach the "flat" network without having to establish an additional bgp session. (#117, @fedepaol)
- Api: make L3VNI VRF field mandatory (#135, @qinqon)
- Enforce the session with the host / with the TOR to be ebgp. (#95, @fedepaol)
- Optional hostsession in the L3VNI CRD, now it's not mandatory to setup a session if a l3vni serves as L3 wrapper of a L2 VNI. (#102, @fedepaol)

### Bug fixes
- Fix cr based validation of the nic name in the underlay crd. (#130, @fedepaol)
- Make gateway ip and local cidrs immutable. (#94, @fedepaol)
- Vlan sub-interfaces can now be selected as underlay NICs. (#128, @maiqueb)

## Release v0.0.1

Fix the website publish job!

## Release v0.0.0

First release!
