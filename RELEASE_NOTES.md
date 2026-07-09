## Release v0.2.0

### New Features
- Add AddressFamilies to Neighbor struct (#494, @andreaskaris)
- Add an optional knob to delay the start of the controller systemd quadlet to start the reconciliation loop after user provided conditions are satisfied. (#487, @fedepaol)
- Add configurable import / export route-targets to l3vni (#197, @k-akashi)
- Add generate-all make target to run all code and manifest generation in a single command (#299, @qinqon)
- Add support for BGP `remote-as external`, `remote-as internal` and for iBGP with ASNs.
  For underlay, this enables more flexible BGP peering scenarios where
  the exact remote ASN is unknown respectively it simplifies the L3VPN
  configuration.
  
  Potentially breaking API change due to removal of unused field Neighbor.HostASN. (#260, @andreaskaris)
- Add support for IPv6 and unnumbered BGP underlay sessions with ToR switches
  Complete rewrite of check_veths in golang (#286, @andreaskaris)
- Allow deriving the node index from a network interface address in systemd mode, enabling the same node-config.yaml to be deployed across all nodes. (#472, @yahlifried)
- Allow ipv6 vteps for evpn / vxlan. (#514, @fedepaol)
- Allow moving multiple nics in the OpenPERouter network namespace and allow setting multiple neighbors for the underlay. (#307, @fedepaol)
- Api: Introduce node router status API. 
  Enable inspecting router configuration status on a spesific node via NodeRouterConfigurationStatus CRD. (#355, @ormergi)
- Automatically set veth MTU to underlay NIC MTU minus 50 bytes to
  account for VXLan encapsulation overhead, preventing frame
  fragmentation at MTU boundaries. (#304, @qinqon)
- Bump base FRR version to 10.6.0 (#295, @zeeke)
- Bump to a newer version of containerlab for lab deployment and use new group topology element. (#274, @andreaskaris)
- DPDK/grout support for Underlay and L3Passthrough (#338, @zeeke)
- Have configurable vtysh timeout, defined from the helm / operator config. (#189, @maiqueb)
- Implementation of SRv6 / ISIS enhancement proposal (#485, @andreaskaris)
- Introduce per-resource configuration resilience.
  
  Invalid L3VNI resources cause their VRF to be skipped along with dependent L2VNIs.
  Invalid L2VNIs are isolated and do not affect other resources.
  VRF subnet overlaps are detected and all affected resources are skipped.
  FRR reload failures are reported as FrrConfigurationFailed with automatic retry.
  
  All failures are reported via RouterNodeConfigurationStatus with detailed conditions. (#423, @RamLavi)
- Make the router resilient to data plane crashes. (#317, @maiqueb)
- Mirror configuration consumed by static files to k8s resources for better visibility. (#469, @fedepaol)
- The controller now bundles CNI plugin binaries (macvlan, ipvlan, static, dhcp) and exposes a libcni-based invoker, preparing for direct underlay interface provisioning in the router netns without Multus. (#544, @maiqueb)

### Bug fixes
- Add liveness probe to FRR container to restart the pod when FRR daemons crash and cannot be recovered by watchfrr. (#455, @andreaskaris)
- Bridge refresher: don't ping ipv6 lla neighbros, listen for neighbor events instead of polling for stale entries (#312, @fedepaol)
- Correct the router component default container name (#319, @maiqueb)
- Don't fail when only rawConfig is provided. (#311, @fedepaol)
- Fix VRF route import failures caused by namespace loopback (lo) being down. The VTEP IP is now assigned directly to lo instead of a separate lound dummy interface. (#467, @andreaskaris)
- Fix start race condition where k8s api is available already, the static controller dies but the health port is not free yet, causing the k8sapicontroller to die because the port is not ready. (#325, @fedepaol)
- Fix underlay interface not being moved back to the default network namespace on underlay deletion. The interface is now restored with its original IP addresses and link-up state. (#442, @andreaskaris)
- Fix vulnerability GO-2026-5026 (#460, @andreaskaris)
- Fix: add unreachable routes to prevent VRF escape (#242, @fdomain)
- In order to be able to deploy the lab on aarch64,
  dynamically set the image architecture in common.sh. (#515, @andreaskaris)
- Monitor the frr container in systemd mode, if it restarts we reconfigure it. (#479, @fedepaol)
- Reject l2gatewayip on disconnected L2VNI (#332, @RamLavi)
- Validate static configuration files with CEL rules and apply defaults. (#310, @fedepaol)

### Other (Cleanup or Flake)
- API types updated to comply with Kubernetes API conventions via kube-api-linter. Breaking changes: integer fields use int32/int64 instead of uint, duration fields replaced with seconds integers, optional fields use pointers. (#313, @qinqon)
- API: Rename `EVPNConfig` to `TunnelEndpointConfig` in the Underlay CRD to better reflect its purpose and in preparation for SRv6 implementation. (#471, @andreaskaris)
- Add RouteTarget type for L3VNI route targets. Also uses for L3VPN route targets in the SRv6 PR. (#518, @andreaskaris)
- Add exit after router bgp in FRR configuration (#302, @andreaskaris)
- Allow L2VNIs without a VRF to operate as pure L2 east-west overlays. (#346, @RamLavi)
- Bump the fsnotify, grpc, net-attach-def-lib, sys, and opencontainers runtime spec dependencies (#529, @maiqueb)
- Cleanup of E2E Leaf modification logic (#327, @andreaskaris)
- Cleanup of iBGP E2E test (#404, @andreaskaris)
- Collect more logs on E2E failure (#308, @andreaskaris)
- Do not set AddrGenModeNone on L2 VNI bridges. (#351, @maiqueb)
- Fix duplicate address-family blocks in FRR passthrough configuration by removing unused neighborenableipfamily template calls. Remove IPFamily from frr_test.go for l3vni and passthrough input as it is never set by production code. (#474, @andreaskaris)
- Fix flake in RawFRRConfig E2E test "should order multiple raw config snippets by priority" (#403, @andreaskaris)
- Fix generation of operator/config/webhook/webhook/manifests.yaml (#393, @andreaskaris)
- Fix several issues with the resource dump on failure: (#456, @andreaskaris)
- Fix typo webook to webhook in cmd/nodemarker/main.go (#476, @andreaskaris)
- For consistency, replace all occurrences of ptr.To with new() (#445, @andreaskaris)
- Generation of coredumps when FRR processes crash in CI. (#457, @andreaskaris)
- Improve logging in testFileIsValid when FRR tests fail (#480, @andreaskaris)
- Minor cleanup of L3Passthrough E2E test (#318, @andreaskaris)
- Pre-delete all interfaces in the router netns as part of the recovery procedure. This will greatly reduce the time it take the kernel to async delete the network namespace via `cleanup_net()` (#463, @maiqueb)
- Remove unused file clab/spine/daemons (#301, @andreaskaris)
- Remove vtepInterface field from EVPNConfig. vtepCIDR is now the only (required) way to configure the VTEP source. (#461, @qinqon)
- Run go fmt in github CI (#429, @andreaskaris)
- Switch FRR logging from file to stdout, simplifying the container entrypoint and enabling native kubectl logs support. (#330, @qinqon)
- The Underlay `spec.nics` field is replaced by `spec.interfaces`, a discriminated union. Use `interfaces: [{type: NetworkDevice, networkDevice: {interfaceName: <nic>}}]` instead of `nics: [<nic>]`. action required (#517, @qinqon)
- This change adds BGP summary and ip address info to debug dump in case of failed tests.
  It also uses current spec report full text to identify multiple failing tests more easily and to avoid overwriting existing tests. (#293, @andreaskaris)
- Tweak the ConnectTime of the FRR K8s side to 1 second in E2E tests. (#294, @andreaskaris)

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
