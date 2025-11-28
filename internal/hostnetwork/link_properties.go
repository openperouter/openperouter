// SPDX-License-Identifier:Apache-2.0

package hostnetwork

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
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

// setAddrGenModeNone sets addr_gen_mode to none to the given link.
func setAddrGenModeNone(l netlink.Link) error {
	fileName := fmt.Sprintf("/proc/sys/net/ipv6/conf/%s/addr_gen_mode", l.Attrs().Name)
	fileName = filepath.Clean(fileName)
	if !strings.HasPrefix(fileName, "/proc/sys/") {
		panic(fmt.Errorf("attempt to escape")) // TODO: replace with os.Root when Go 1.24 is out
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

// moveInterfaceToNamespace takes the given interface and moves it into the given namespace.
func moveInterfaceToNamespace(ctx context.Context, intf string, ns netns.NsHandle) error {
	slog.DebugContext(ctx, "move intf to namespace", "intf", intf, "namespace", ns.String())
	defer slog.DebugContext(ctx, "move intf to namespace end", "intf", intf, "namespace", ns.String())

	err := inNamespace(ns, func() error {
		_, err := netlink.LinkByName(intf)
		if err != nil {
			return fmt.Errorf("moveInterfaceToNamespace: Failed to find link %s: %w", intf, err)
		}
		return nil
	})
	if err == nil {
		slog.DebugContext(ctx, "intf is already in namespace", "intf", intf, "namespace", ns.String())
		return nil
	}
	var nsError setNamespaceError
	if errors.As(err, &nsError) {
		return fmt.Errorf("moveInterfaceToNamespace: Failed to execute in ns %s: %w", intf, err)
	}

	link, err := netlink.LinkByName(intf)
	if err != nil {
		return fmt.Errorf("moveInterfaceToNamespace: Failed to find link %s: %w", intf, err)
	}

	addresses, err := netlink.AddrList(link, netlink.FAMILY_ALL)
	if err != nil {
		return fmt.Errorf("moveInterfaceToNamespace: Failed to get addresses for intf %s: %w", link.Attrs().Name, err)
	}

	slog.DebugContext(ctx, "addresses before moving", "addresses", addresses)
	err = netlink.LinkSetNsFd(link, int(ns))
	if err != nil {
		return fmt.Errorf("moveInterfaceToNamespace: Failed to move %s to network namespace %s: %w", link.Attrs().Name, ns.String(), err)
	}
	if err := inNamespace(ns, func() error {
		nsLink, err := netlink.LinkByName(intf)
		if err != nil {
			return fmt.Errorf("moveInterfaceToNamespace: Failed to get link by name %s up in network namespace %s: %w", intf, ns.String(), err)
		}
		err = netlink.LinkSetUp(nsLink)
		if err != nil {
			return fmt.Errorf("moveInterfaceToNamespace: Failed to set %s up in network namespace %s: %w", nsLink.Attrs().Name, ns.String(), err)
		}

		slog.DebugContext(ctx, "restoring addresses in namespace", "addresses", addresses)
		for _, a := range addresses {
			slog.DebugContext(ctx, "restoring address in namespace", "address", a, "flags", a.Flags)
			IFA_F_NOPREFIXROUTE := 0x200 // remove no prefix route
			a.Flags &= ^IFA_F_NOPREFIXROUTE
			slog.DebugContext(ctx, "restoring address in namespace after no prefix", "address", a, "flags", a.Flags)
			err := netlink.AddrAdd(nsLink, &a)
			if err != nil && !os.IsExist(err) {
				return fmt.Errorf("moveNicToNamespace: Failed to add address %s to %s: %w", a, nsLink.Attrs().Name, err)
			}
		}
		return nil
	}); err != nil {
		return err
	}
	return nil
}

func intToInt32(val int) (int32, error) {
	if val < math.MinInt32 || val > math.MaxInt32 {
		return 0, fmt.Errorf("can't convert %d to int32", val)
	}
	return int32(val), nil
}
