// SPDX-License-Identifier:Apache-2.0

package hostnetwork

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
)

type VNIParams struct {
	VRF        string
	TargetNS   string
	VTEPIP     string
	VethHostIP string
	VethNSIP   string
	VNI        int
	VXLanPort  int
}

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
		return fmt.Errorf("SetupVNI: Failed to get network namespace %s", params.TargetNS)
	}

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

	if err := inNamespace(ns, func() error {
		err = assignIPToInterface(peVeth, params.VethNSIP)
		if err != nil {
			return err
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
			return fmt.Errorf("failed to set vrf %s as marter of pe veth %s", vrf.Name, peVeth.Attrs().Name)
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
func RemoveNonConfiguredVNIs(ns netns.NsHandle, params []VNIParams) error {
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
	for _, hl := range hostLinks {
		if hl.Type() == "veth" && strings.HasPrefix(hl.Attrs().Name, HostVethPrefix) {
			vrf := vrfFromHostVeth(hl.Attrs().Name)
			if vrfs[vrf] {
				continue
			}
			if err := netlink.LinkDel(hl); err != nil {
				return fmt.Errorf("remove host leg: %s %w", hl.Attrs().Name, err)
			}
		}
	}

	err = inNamespace(ns, func() error {
		links, err := netlink.LinkList()
		if err != nil {
			return fmt.Errorf("remove non configured vnis: failed to list links: %w", err)
		}

		if err := deleteLinks("vxlan", vnis, links, vniFromVXLanName); err != nil {
			return err
		}
		if err := deleteLinks("bridge", vnis, links, vniFromBridgeName); err != nil {
			return err
		}

		for _, l := range links {
			if l.Type() == "vrf" && !vrfs[l.Attrs().Name] {
				if err := netlink.LinkDel(l); err != nil {
					return fmt.Errorf("remove non configured vnis: failed to delete vrf %s %w", l.Attrs().Name, err)
				}
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	return nil
}

// deleteLinks deletes all the links of the given type that do not correspond to
// any VNI.
func deleteLinks(linkType string, vnis map[int]bool, links []netlink.Link, vniFromName func(string) (int, error)) error {
	for _, l := range links {
		if l.Type() == linkType {
			vni, err := vniFromName(l.Attrs().Name)
			if err != nil {
				return fmt.Errorf("remove non configured vnis: failed to get vni for %s %w", linkType, err)
			}
			if _, ok := vnis[vni]; ok {
				continue
			}
			if err := netlink.LinkDel(l); err != nil {
				return fmt.Errorf("remove non configured vnis: failed to delete %s %s %w", linkType, l.Attrs().Name, err)
			}
		}
	}

	return nil
}
