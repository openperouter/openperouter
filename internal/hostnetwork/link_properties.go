// SPDX-License-Identifier:Apache-2.0

package hostnetwork

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netlink/nl"
	"github.com/vishvananda/netns"
	"golang.org/x/sys/unix"
)

// assingIPToInterface assignes the given address to the link.
func assignIPToInterface(link netlink.Link, address string) error {
	addr, err := netlink.ParseAddr(address)
	if err != nil {
		return fmt.Errorf("assignIPToInterface: failed to parse address %s for interface %s: %w", address, link.Attrs().Name, err)
	}

	hasIP, err := interfaceHasIP(link, address)
	if err != nil {
		return err
	}
	if hasIP {
		return nil
	}
	err = netlink.AddrAdd(link, addr)
	if err != nil {
		return fmt.Errorf("assignIPToInterface: failed to add address %s to interface %s, err %v", address, link.Attrs().Name, err)
	}
	return nil
}

// interfaceHasIP tells if the given link has the provided ip.
func interfaceHasIP(link netlink.Link, address string) (bool, error) {
	_, err := netlink.ParseAddr(address)
	if err != nil {
		return false, fmt.Errorf("interfaceHasIP: failed to parse address %s for interface %s: %w", address, link.Attrs().Name, err)
	}
	addresses, err := netlink.AddrList(link, netlink.FAMILY_ALL)
	if err != nil {
		return false, fmt.Errorf("interfaceHasIP: failed to list addresses for interface %s: %w", link.Attrs().Name, err)
	}
	for _, a := range addresses {
		if a.IPNet.String() == address {
			return true, nil
		}
	}
	return false, nil
}

// interfaceHasNoIP tells if the given link does not have
// ips of the given family.
func interfaceHasNoIP(link netlink.Link, family int) (bool, error) {
	addresses, err := netlink.AddrList(link, family)
	if err != nil {
		return false, fmt.Errorf("interfaceHasNoIP: failed to list addresses for interface %s: %w", link.Attrs().Name, err)
	}
	if len(addresses) == 0 {
		return true, nil
	}

	return false, nil
}

// setAddrGenModeNone sets addr_gen_mode to none (value "1") on the given link.
// It is idempotent: if already set, it does nothing to avoid unnecessary netlink events.
func setAddrGenModeNone(l netlink.Link) error {
	fileName := fmt.Sprintf("/proc/sys/net/ipv6/conf/%s/addr_gen_mode", l.Attrs().Name)
	fileName = filepath.Clean(fileName)
	if !strings.HasPrefix(fileName, "/proc/sys/") {
		panic(fmt.Errorf("attempt to escape")) // TODO: replace with os.Root when Go 1.24 is out
	}

	currentValue, err := os.ReadFile(fileName)
	if err != nil {
		return fmt.Errorf("addrGenModeNone: error reading file: %w", err)
	}
	if strings.TrimSpace(string(currentValue)) == "1" {
		return nil
	}

	file, err := os.OpenFile(fileName, os.O_WRONLY, 0)
	if err != nil {
		return fmt.Errorf("addrGenModeNone: error opening file: %w", err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			slog.Error("failed to close file", "file", fileName, "error", err)
		}
	}()

	if _, err := fmt.Fprintf(file, "%s\n", "1"); err != nil {
		return fmt.Errorf("addrGenModeNone: error writing to file: %w", err)
	}
	return nil
}

// linkSetUp sets the link up only if it's not already up.
// This avoids unnecessary RTM_NEWLINK events that can cause FRR to flush neighbor entries.
func linkSetUp(l netlink.Link) error {
	currentLink, err := netlink.LinkByIndex(l.Attrs().Index)
	if err != nil {
		return fmt.Errorf("linkSetUp: failed to get link by index: %w", err)
	}

	if currentLink.Attrs().Flags&net.FlagUp != 0 {
		return nil
	}

	return netlink.LinkSetUp(l)
}

// linkSetMTU sets the MTU on the link only if it differs from the desired value.
// This avoids unnecessary RTM_NEWLINK events that can cause FRR to flush neighbor entries.
func linkSetMTU(l netlink.Link, mtu int) error {
	currentLink, err := netlink.LinkByIndex(l.Attrs().Index)
	if err != nil {
		return fmt.Errorf("linkSetMTU: failed to get link by index: %w", err)
	}

	if currentLink.Attrs().MTU == mtu {
		return nil
	}

	return netlink.LinkSetMTU(l, mtu)
}

// linkSetMaster sets the master of a link only if it's not already set to the desired master.
// This avoids unnecessary RTM_NEWLINK events that can cause FRR to flush neighbor entries.
func linkSetMaster(link, master netlink.Link) error {
	currentLink, err := netlink.LinkByIndex(link.Attrs().Index)
	if err != nil {
		return fmt.Errorf("linkSetMaster: failed to get link by index: %w", err)
	}

	if currentLink.Attrs().MasterIndex == master.Attrs().Index {
		return nil
	}

	return netlink.LinkSetMaster(link, master)
}

// setNeighSuppression sets neighbor suppression to the given link.
func setNeighSuppression(link netlink.Link) error {
	req := nl.NewNetlinkRequest(unix.RTM_SETLINK, unix.NLM_F_ACK)

	msg := nl.NewIfInfomsg(unix.AF_BRIDGE)
	var err error
	msg.Index, err = intToInt32(link.Attrs().Index)
	if err != nil {
		return fmt.Errorf("invalid index for %s", link.Attrs().Name)
	}
	req.AddData(msg)

	br := nl.NewRtAttr(unix.IFLA_PROTINFO|unix.NLA_F_NESTED, nil)
	br.AddRtAttr(32, []byte{1})
	req.AddData(br)
	_, err = req.Execute(unix.NETLINK_ROUTE, 0)
	if err != nil {
		return fmt.Errorf("error executing request: %w", err)
	}
	return nil
}

// moveInterfaceToNamespace takes the given interface and moves it from the given to the given namespace.
func moveInterfaceToNamespace(ctx context.Context, intf string, fromHandle, toHandle *netlink.Handle,
	toNS netns.NsHandle, groupID uint32) error {
	slog.DebugContext(ctx, "move intf to namespace", "intf", intf, "toNS", toNS.String())
	defer slog.DebugContext(ctx, "move intf to namespace end", "intf", intf, "toNS", toNS.String())

	// Nothing to do for us if the interface is already inside the target namespace.
	if _, err := toHandle.LinkByName(intf); err == nil {
		slog.DebugContext(ctx, "intf is already in namespace", "intf", intf, "namespace", toNS.String())
		return nil
	}

	// Get the link, store its current addresses so that we can later restore them (addresses are lost upon moving),
	// and move the link to the target netns.
	link, err := fromHandle.LinkByName(intf)
	if err != nil {
		return fmt.Errorf("moveInterfaceToNamespace: Failed to find link %s: %w", intf, err)
	}

	addresses, err := fromHandle.AddrList(link, netlink.FAMILY_ALL)
	if err != nil {
		return fmt.Errorf("moveInterfaceToNamespace: Failed to get addresses for intf %s: %w", link.Attrs().Name, err)
	}
	slog.DebugContext(ctx, "addresses before moving", "addresses", addresses)

	err = fromHandle.LinkSetNsFd(link, int(toNS))
	if err != nil {
		return fmt.Errorf("moveInterfaceToNamespace: Failed to move %s to network namespace %s: %w", link.Attrs().Name, toNS.String(), err)
	}

	// After the move, get the link inside the new namespace, set the link up, and add the addresses again.
	nsLink, err := toHandle.LinkByName(intf)
	if err != nil {
		return fmt.Errorf("moveInterfaceToNamespace: Failed to get link by name %s up in network namespace %s: %w", intf, toNS.String(), err)
	}

	slog.DebugContext(ctx, "restoring addresses in namespace", "addresses", addresses)
	for _, a := range addresses {
		slog.DebugContext(ctx, "restoring address in namespace", "address", a, "flags", a.Flags)
		a.Flags &= ^unix.IFA_F_NOPREFIXROUTE
		slog.DebugContext(ctx, "restoring address in namespace after no prefix", "address", a, "flags", a.Flags)
		err := toHandle.AddrAdd(nsLink, &a)
		if err != nil && !os.IsExist(err) {
			return fmt.Errorf("moveInterfaceToNamespace: Failed to add address %s to %s: %w", a, nsLink.Attrs().Name, err)
		}
	}

	// Set group ID only if not already set (idempotent)
	if nsLink.Attrs().Group != groupID {
		if err := toHandle.LinkSetGroup(nsLink, int(groupID)); err != nil {
			return fmt.Errorf("moveInterfaceToNamespace: Failed to set group ID %d on interface %s: %w", groupID, intf, err)
		}
	}

	err = toHandle.LinkSetUp(nsLink)
	if err != nil {
		return fmt.Errorf("moveInterfaceToNamespace: Failed to set %s up in network namespace %s: %w", nsLink.Attrs().Name, toNS.String(), err)
	}

	return nil
}

func intToInt32(val int) (int32, error) {
	if val < math.MinInt32 || val > math.MaxInt32 {
		return 0, fmt.Errorf("can't convert %d to int32", val)
	}
	return int32(val), nil
}
