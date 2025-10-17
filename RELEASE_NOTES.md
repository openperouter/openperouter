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
