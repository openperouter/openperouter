// SPDX-License-Identifier:Apache-2.0

package hostnetwork

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
)

type VNIParams struct {
	VRF       string `json:"vrf"`
	TargetNS  string `json:"targetns"`
	VTEPIP    string `json:"vtepip"`
	VNI       int    `json:"vni"`
	VXLanPort int    `json:"vxlanport"`
}

type L3VNIParams struct {
	VNIParams    `json:",inline"`
	VethHostIPv4 string `json:"vethhostipv4"`
	VethNSIPv4   string `json:"vethnsipv4"`
	VethHostIPv6 string `json:"vethhostipv6"`
	VethNSIPv6   string `json:"vethnsipv6"`
}

type L2VNIParams struct {
	VNIParams   `json:",inline"`
	L2GatewayIP *string     `json:"l2gatewayip"`
	HostMaster  *HostMaster `json:"hostmaster"`
}

type HostMaster struct {
	Name       string `json:"name,omitempty"`
	Type       string `json:"type,omitempty"`
	AutoCreate bool   `json:"autocreate,omitempty"`
}

const (
	VRFLinkType    = "vrf"
	BridgeLinkType = "bridge"
	VXLanLinkType  = "vxlan"
	VethLinkType   = "veth"
)

type NotRouterInterfaceError struct {
	Name string
}

func (e NotRouterInterfaceError) Error() string {
	return fmt.Sprintf("interface %s is not a router interface", e.Name)
}

// SetupL3VNI sets up a Layer 3 VNI in the target namespace.
// It uses setupVNI to create the necessary VRF, bridge, and
// VXLan interface, and moves the veth to the VRF corresponding
// to the L3 routing domain, exposing it to the default host namespace.
func SetupL3VNI(ctx context.Context, params L3VNIParams) error {
	if err := setupVNI(ctx, params.VNIParams); err != nil {
		return fmt.Errorf("SetupL3VNI: failed to setup VNI: %w", err)
	}
	slog.DebugContext(ctx, "setting up l3 VNI", "params", params)
	defer slog.DebugContext(ctx, "end setting up l3 VNI", "params", params)

	ns, err := netns.GetFromName(params.TargetNS)
	if err != nil {
		return fmt.Errorf("SetupVNI: Failed to get network namespace %s: %w", params.TargetNS, err)
	}
	defer func() {
		if err := ns.Close(); err != nil {
			slog.Error("failed to close namespace", "namespace", params.TargetNS, "error", err)
		}
	}()

	hostSide, peSide := vethNamesFromVNI(params.VNI)

	hostVeth, err := netlink.LinkByName(hostSide)
	if errors.As(err, &netlink.LinkNotFoundError{}) {
		return fmt.Errorf("SetupL3VNI: host veth %s does not exist, cannot setup L3 VNI", hostSide)
	}

	err = assignIPsToInterface(hostVeth, params.VethHostIPv4, params.VethHostIPv6)
	if err != nil {
		return fmt.Errorf("failed to assign IPs to host veth: %w", err)
	}

	if err := inNamespace(ns, func() error {
		peVeth, err := netlink.LinkByName(peSide)
		if err != nil {
			return fmt.Errorf("could not find peer veth %s in namespace %s: %w", peSide, params.TargetNS, err)
		}

		err = assignIPsToInterface(peVeth, params.VethNSIPv4, params.VethNSIPv6)
		if err != nil {
			return fmt.Errorf("failed to assign IPs to PE veth: %w", err)
		}

		vrf, err := netlink.LinkByName(params.VRF)
		if err != nil {
			return fmt.Errorf("could not find vrf %s in namespace %s: %w", params.VRF, params.TargetNS, err)
		}

		err = netlink.LinkSetMaster(peVeth, vrf)
		if err != nil {
			return fmt.Errorf("failed to set vrf %s as master of pe veth %s: %w", params.VRF, peVeth.Attrs().Name, err)
		}
		return nil
	}); err != nil {
		return err
	}
	return nil
}

// SetupL2VNI sets up a Layer 2 VNI in the target namespace.
// It uses setupVNI to create the necessary VRF, bridge, and
// VXLan interface, and enslaves the veth leg to the bridge,
// exposing the L2 domain to the default host namespace.
func SetupL2VNI(ctx context.Context, params L2VNIParams) error {
	if err := setupVNI(ctx, params.VNIParams); err != nil {
		return fmt.Errorf("SetupL2VNI: failed to setup VNI: %w", err)
	}
	slog.DebugContext(ctx, "setting up l2 VNI", "params", params)
	defer slog.DebugContext(ctx, "end setting up l2 VNI", "params", params)

	ns, err := netns.GetFromName(params.TargetNS)
	if err != nil {
		return fmt.Errorf("SetupVNI: Failed to get network namespace %s: %w", params.TargetNS, err)
	}
	defer func() {
		if err := ns.Close(); err != nil {
			slog.Error("failed to close namespace", "namespace", params.TargetNS, "error", err)
		}
	}()

	hostSide, peSide := vethNamesFromVNI(params.VNI)

	hostVeth, err := netlink.LinkByName(hostSide)
	if errors.As(err, &netlink.LinkNotFoundError{}) {
		return fmt.Errorf("SetupL2VNI: host veth %s does not exist, cannot setup L3 VNI", hostSide)
	}

	if params.HostMaster != nil {
		master, err := hostMaster(params.VNI, *params.HostMaster)
		if err != nil {
			return fmt.Errorf("SetupL2VNI: failed to get host master for VRF %s: %w", params.VRF, err)
		}
		if err := netlink.LinkSetMaster(hostVeth, master); err != nil {
			return fmt.Errorf("failed to set host master %s as master of host veth %s: %w", master.Attrs().Name, hostVeth.Attrs().Name, err)
		}
	}

	if err := inNamespace(ns, func() error {
		peVeth, err := netlink.LinkByName(peSide)
		if err != nil {
			return fmt.Errorf("could not find peer veth %s in namespace %s: %w", peSide, params.TargetNS, err)
		}
		name := bridgeName(params.VNI)
		bridge, err := netlink.LinkByName(name)
		if err != nil {
			return fmt.Errorf("could not find bridge %s in namespace %s: %w", name, params.TargetNS, err)
		}
		if err := netlink.LinkSetMaster(peVeth, bridge); err != nil {
			return fmt.Errorf("failed to set bridge %s as master of pe veth %s: %w", name, peVeth.Attrs().Name, err)
		}
		if params.L2GatewayIP != nil {
			if err := assignIPToInterface(bridge, *params.L2GatewayIP); err != nil {
				return fmt.Errorf("failed to assign L2 gateway IP %s to bridge %s: %w", *params.L2GatewayIP, name, err)
			}

			// setting up the same mac address for all the nodes for distributed gateway
			if err := setBridgeFixedMacAddress(bridge, params.VNI); err != nil {
				return fmt.Errorf("failed to set bridge mac address %s: %v", name, err)
			}
		}

		return nil
	}); err != nil {
		return err
	}
	return nil
}

// setupVNI sets up the configuration required by FRR to
// serve a given VNI in the target namespace. This includes:
// - a linux VRF
// - a linux Bridge enslaved to the given VRF
// - a VXLan interface enslaved to the given VRF
//
// Additionally, it creates a veth pair and moves one leg in the target
// namespace.
func setupVNI(ctx context.Context, params VNIParams) error {
	slog.DebugContext(ctx, "setting up VNI", "params", params)
	defer slog.DebugContext(ctx, "end setting up VNI", "params", params)
	ns, err := netns.GetFromName(params.TargetNS)
	if err != nil {
		return fmt.Errorf("SetupVNI: Failed to get network namespace %s: %w", params.TargetNS, err)
	}
	defer func() {
		if err := ns.Close(); err != nil {
			slog.Error("failed to close namespace", "namespace", params.TargetNS, "error", err)
		}
	}()

	hostVeth, err := setupVeth(ctx, params.VNI, ns)
	if err != nil {
		return err
	}

	err = netlink.LinkSetUp(hostVeth)
	if err != nil {
		return fmt.Errorf("could not set link up for host leg %s: %v", hostVeth, err)
	}

	if err := inNamespace(ns, func() error {
		_, peSideName := vethNamesFromVNI(params.VNI)
		peVeth, err := netlink.LinkByName(peSideName)
		if err != nil {
			return fmt.Errorf("could not find peer veth %s in namespace %s: %w", peSideName, params.TargetNS, err)
		}
		err = netlink.LinkSetUp(peVeth)
		if err != nil {
			return fmt.Errorf("could not set link up for host leg %s: %v", hostVeth, err)
		}

		slog.DebugContext(ctx, "setting up vrf", "vrf", params.VRF)
		vrf, err := setupVRF(params.VRF)
		if err != nil {
			return err
		}

		err = netlink.LinkSetMaster(peVeth, vrf)
		if err != nil {
			return fmt.Errorf("failed to set vrf %s as master of pe veth %s: %w", vrf.Name, peVeth.Attrs().Name, err)
		}

		slog.DebugContext(ctx, "setting up bridge")
		bridge, err := setupBridge(params, vrf)
		if err != nil {
			return err
		}

		slog.DebugContext(ctx, "setting up vxlan")
		err = setupVXLan(params, bridge)
		if err != nil {
			return err
		}

		return nil
	}); err != nil {
		return err
	}

	return nil
}

// RemoveNonConfiguredVNIs removes from the target namespace the
// leftovers corresponding to VNIs that are not configured anymore.
func RemoveNonConfiguredVNIs(targetNS string, params []VNIParams) error {
	vrfs := map[string]bool{}
	vnis := map[int]bool{}
	for _, p := range params {
		vrfs[p.VRF] = true
		vnis[p.VNI] = true
	}
	hostLinks, err := netlink.LinkList()
	if err != nil {
		return fmt.Errorf("remove non configured vnis: failed to list links: %w", err)
	}
	failedDeletes := []error{}
	if err := deleteLinksForType(BridgeLinkType, vnis, hostLinks, vniFromHostBridgeName); err != nil {
		failedDeletes = append(failedDeletes, fmt.Errorf("remove bridge links: %w", err))
		return err
	}

	for _, hl := range hostLinks {
		if hl.Type() != VethLinkType {
			continue
		}
		if !strings.HasPrefix(hl.Attrs().Name, HostVethPrefix) {
			continue
		}
		vni, err := vniFromHostVeth(hl.Attrs().Name)
		if err != nil {
			failedDeletes = append(failedDeletes, fmt.Errorf("remove host leg: %s %w", hl.Attrs().Name, err))
			continue
		}
		if vnis[vni] {
			continue
		}
		if err := netlink.LinkDel(hl); err != nil {
			failedDeletes = append(failedDeletes, fmt.Errorf("remove host leg: %s %w", hl.Attrs().Name, err))
		}
	}

	ns, err := netns.GetFromName(targetNS)
	if err != nil {
		return fmt.Errorf("RemoveNonConfiguredVNIs: Failed to get network namespace %s: %w", targetNS, err)
	}
	defer func() {
		if err := ns.Close(); err != nil {
			slog.Error("failed to close namespace", "namespace", targetNS, "error", err)
		}
	}()

	if err := inNamespace(ns, func() error {
		links, err := netlink.LinkList()
		if err != nil {
			return fmt.Errorf("remove non configured vnis: failed to list links: %w", err)
		}

		if err := deleteLinksForType(VXLanLinkType, vnis, links, vniFromVXLanName); err != nil {
			failedDeletes = append(failedDeletes, fmt.Errorf("remove vlan links: %w", err))
			return err
		}
		if err := deleteLinksForType(BridgeLinkType, vnis, links, vniFromBridgeName); err != nil {
			failedDeletes = append(failedDeletes, fmt.Errorf("remove bridge links: %w", err))
			return err
		}

		for _, l := range links {
			if l.Type() != VRFLinkType {
				continue
			}
			if vrfs[l.Attrs().Name] {
				continue
			}
			if err := netlink.LinkDel(l); err != nil {
				failedDeletes = append(failedDeletes, fmt.Errorf("remove non configured vnis: failed to delete vrf %s %w", l.Attrs().Name, err))
			}
		}
		return errors.Join(failedDeletes...)
	}); err != nil {
		return err
	}

	return errors.Join(failedDeletes...)
}

// deleteLinks deletes all the links of the given type that do not correspond to
// any VNI.
func deleteLinksForType(linkType string, vnis map[int]bool, links []netlink.Link, vniFromName func(string) (int, error)) error {
	deleteErrors := []error{}
	for _, l := range links {
		if l.Type() != linkType {
			continue
		}
		vni, err := vniFromName(l.Attrs().Name)
		if errors.As(err, &NotRouterInterfaceError{}) {
			// not a router interface, skip
			continue
		}
		if err != nil {
			deleteErrors = append(deleteErrors, fmt.Errorf("remove non configured vnis: failed to get vni for %s %w", linkType, err))
			continue
		}
		if _, ok := vnis[vni]; ok {
			continue
		}
		if err := netlink.LinkDel(l); err != nil {
			deleteErrors = append(deleteErrors, fmt.Errorf("remove non configured vnis: failed to delete %s %s %w", linkType, l.Attrs().Name, err))
			continue
		}
	}

	return errors.Join(deleteErrors...)
}

func hostMaster(vni int, m HostMaster) (netlink.Link, error) {
	if !m.AutoCreate {
		hostMaster, err := netlink.LinkByName(m.Name)
		if err != nil {
			return nil, fmt.Errorf("could not find host master %s: %w", m.Name, err)
		}
		return hostMaster, nil
	}
	bridge, err := createHostBridge(vni)
	if err != nil {
		return nil, fmt.Errorf("getHostMaster: failed to create host bridge %d: %w", vni, err)
	}
	return bridge, nil
}

// assignIPsToInterface assigns both IPv4 and IPv6 addresses to an interface.
// It fails if no IPs are provided (both IPv4 and IPv6 are empty).
func assignIPsToInterface(link netlink.Link, ipv4, ipv6 string) error {
	if ipv4 == "" && ipv6 == "" {
		return fmt.Errorf("at least one IP address must be provided (IPv4 or IPv6)")
	}

	if ipv4 != "" {
		if err := assignIPToInterface(link, ipv4); err != nil {
			return fmt.Errorf("failed to assign IPv4 address %s: %w", ipv4, err)
		}
	}

	if ipv6 != "" {
		if err := assignIPToInterface(link, ipv6); err != nil {
			return fmt.Errorf("failed to assign IPv6 address %s: %w", ipv6, err)
		}
	}

	return nil
}
