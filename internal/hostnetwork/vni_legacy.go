// SPDX-License-Identifier:Apache-2.0

package hostnetwork

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/openperouter/openperouter/internal/netnamespace"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
	"golang.org/x/sys/unix"
)

// LegacyVNIIDs identifies owned fixed-ID tunnels before any metadata tunnel is created.
// The kernel cannot bind both modes to the same local VXLAN socket.
func LegacyVNIIDs(targetNS string) ([]int32, error) {
	ns, err := netns.GetFromPath(targetNS)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := ns.Close(); err != nil {
			slog.Error("failed to close namespace", "namespace", targetNS, "error", err)
		}
	}()
	var vnis []int32
	err = netnamespace.In(ns, func() error {
		links, err := netlink.LinkList()
		if err != nil {
			return err
		}
		for _, link := range links {
			vxlan, ok := link.(*netlink.Vxlan)
			if !ok || vxlan.FlowBased {
				continue
			}
			vni, err := vniFromVXLanName(vxlan.Name)
			if errors.As(err, &NotRouterInterfaceError{}) {
				continue
			}
			if err != nil {
				return err
			}
			if int(vni) != vxlan.VxlanId {
				continue
			}
			vnis = append(vnis, vni)
		}
		return nil
	})
	return vnis, err
}

// RemoveLegacyVNIs removes the previously identified owned legacy topology.
// Callers must stop refreshers for these VNIs before invoking this function.
func RemoveLegacyVNIs(targetNS string, vnis []int32) error {
	ns, err := netns.GetFromPath(targetNS)
	if err != nil {
		return err
	}
	defer func() {
		if err := ns.Close(); err != nil {
			slog.Error("failed to close namespace", "namespace", targetNS, "error", err)
		}
	}()
	return netnamespace.In(ns, func() error {
		// Remove every fixed-ID tunnel before creating any external tunnel.
		for _, vni := range vnis {
			if err := removeLegacyTunnel(vni); err != nil {
				return err
			}
		}
		for _, vni := range vnis {
			if err := removeLegacyBridge(vni); err != nil {
				return err
			}
		}
		return nil
	})
}

func removeLegacyTunnel(vni int32) error {
	link, err := netlink.LinkByName(vxLanNameFromVNI(vni))
	if errors.As(err, &netlink.LinkNotFoundError{}) {
		return nil
	}
	if err != nil {
		return err
	}
	vxlan, ok := link.(*netlink.Vxlan)
	if !ok || vxlan.FlowBased || vxlan.VxlanId != int(vni) {
		return nil
	}
	if err := netlink.LinkDel(link); err != nil && !errors.Is(err, unix.ENODEV) {
		return fmt.Errorf("remove legacy VXLAN %s: %w", link.Attrs().Name, err)
	}
	return nil
}

func removeLegacyBridge(vni int32) error {
	link, err := netlink.LinkByName(BridgeName(vni))
	if errors.As(err, &netlink.LinkNotFoundError{}) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, ok := link.(*netlink.Bridge); !ok {
		return nil
	}
	if err := netlink.LinkDel(link); err != nil && !errors.Is(err, unix.ENODEV) {
		return fmt.Errorf("remove legacy bridge %s: %w", link.Attrs().Name, err)
	}
	return nil
}
