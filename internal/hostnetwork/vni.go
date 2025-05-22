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
	VRF        string `json:"vrf"`
	TargetNS   string `json:"targetns"`
	VTEPIP     string `json:"vtepip"`
	VethHostIP string `json:"vethhostip"`
	VethNSIP   string `json:"vethnsip"`
	VNI        int    `json:"vni"`
	VXLanPort  int    `json:"vxlanport"`
}

const (
	VRFLinkType    = "vrf"
	BridgeLinkType = "bridge"
	VXLanLinkType  = "vxlan"
	VethLinkType   = "veth"
)

// SetupVNI sets up all the configuration required by FRR to
// serve a given VNI in the target namespace. This includes:
// - a linux VRF
// - a linux Bridge enslaved to the given VRF
// - a VXLan interface enslaved to the given VRF
//
// Additionally, it creates a veth pair and moves one leg in the target
// namespace.
func SetupVNI(ctx context.Context, params VNIParams) error {
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

	hostVeth, peVeth, err := setupVeth(ctx, params.VRF, ns)
	if err != nil {
		return err
	}
	err = assignIPToInterface(hostVeth, params.VethHostIP)
	if err != nil {
		return err
	}

	err = netlink.LinkSetUp(hostVeth)
	if err != nil {
		return fmt.Errorf("could not set link up for host leg %s: %v", hostVeth, err)
	}
	slog.DebugContext(ctx, "host side is up", "veth", hostVeth.Attrs().Name)

	if err := inNamespace(ns, func() error {
		err = assignIPToInterface(peVeth, params.VethNSIP)
		if err != nil {
			return err
		}
		err = netlink.LinkSetUp(peVeth)
		if err != nil {
			return fmt.Errorf("could not set link up for host leg %s: %v", hostVeth, err)
		}
		slog.DebugContext(ctx, "ns side is up", "veth", peVeth.Attrs().Name)

		slog.DebugContext(ctx, "setting up vrf", "vrf", params.VRF)
		vrf, err := setupVRF(params.VRF)
		if err != nil {
			return err
		}

		err = netlink.LinkSetMaster(peVeth, vrf)
		if err != nil {
			return fmt.Errorf("failed to set vrf %s as master of pe veth %s", vrf.Name, peVeth.Attrs().Name)
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
	for _, hl := range hostLinks {
		if hl.Type() != VethLinkType {
			continue
		}
		if !strings.HasPrefix(hl.Attrs().Name, HostVethPrefix) {
			continue
		}
		vrf := vrfFromHostVeth(hl.Attrs().Name)
		if vrfs[vrf] {
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
