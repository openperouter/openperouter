// SPDX-License-Identifier:Apache-2.0

package hostnetwork

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"

	"github.com/openperouter/openperouter/internal/netnamespace"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
)

const (
	loopbackName = "lo"
)

// underlayGroupID is the link group ID assigned to all underlay interfaces
// moved into the network namespace. This allows us to identify and query
// all underlay interfaces by their group membership.
const underlayGroupID = 4242

type UnderlayParams struct {
	UnderlayInterfaces []string            `json:"underlay_interfaces"`
	TargetNS           string              `json:"target_ns"`
	EVPN               *UnderlayEVPNParams `json:"evpn"`
}

type UnderlayEVPNParams struct {
	VtepIP string `json:"vtep_ip"`
}

func SetupUnderlay(ctx context.Context, params UnderlayParams) error {
	slog.DebugContext(ctx, "setup underlay", "params", params)
	defer slog.DebugContext(ctx, "setup underlay done")

	targetNetNS, err := netns.GetFromPath(params.TargetNS)
	if err != nil {
		return fmt.Errorf("setupUnderlay: Failed to find network namespace %s: %w", params.TargetNS, err)
	}
	defer func() {
		if err := targetNetNS.Close(); err != nil {
			slog.Error("failed to close namespace", "namespace", params.TargetNS, "error", err)
		}
	}()
	defaultNetNS, err := netns.Get()
	if err != nil {
		return fmt.Errorf("SetupUnderlay: failed to get netns handle for default namespace: %w", err)
	}
	defer func() {
		if err := defaultNetNS.Close(); err != nil {
			slog.Error("failed to close default namespace", "error", err)
		}
	}()

	// Check if there are existing underlay interfaces that aren't in the new list.
	// This means the underlay configuration changed and requires rebuilding
	// the network namespace.
	existingIfaces, err := findInterfacesInGroup(targetNetNS, underlayGroupID)
	if err != nil {
		return fmt.Errorf("failed to check existing underlay interfaces: %w", err)
	}
	for _, name := range existingIfaces {
		if !slices.Contains(params.UnderlayInterfaces, name) {
			return UnderlayExistsError(fmt.Sprintf(
				"existing underlay found: %s, new interfaces are %v", name, params.UnderlayInterfaces))
		}
	}

	targetNetNSHandle, err := netlink.NewHandleAt(targetNetNS)
	if err != nil {
		return fmt.Errorf("SetupUnderlay: failed to get netlink handle for namespace %s: %w", targetNetNS.String(), err)
	}
	defer targetNetNSHandle.Close()
	defaultNetNSHandle, err := netlink.NewHandleAt(defaultNetNS)
	if err != nil {
		return fmt.Errorf("SetupUnderlay: failed to get netlink handle for default namespace: %w", err)
	}
	defer defaultNetNSHandle.Close()

	for _, underlayInterface := range params.UnderlayInterfaces {
		if err := moveInterfaceToNamespace(ctx, underlayInterface, defaultNetNSHandle, targetNetNSHandle, targetNetNS,
			underlayGroupID); err != nil {
			return err
		}
	}

	if params.EVPN == nil {
		return nil
	}

	if params.EVPN.VtepIP != "" {
		if err := ensureLoopback(ctx, targetNetNS, params.EVPN.VtepIP); err != nil {
			return err
		}
	}

	return nil
}

type UnderlayExistsError string

func (e UnderlayExistsError) Error() string {
	return string(e)
}

func ensureLoopback(ctx context.Context, ns netns.NsHandle, vtepIP string) error {
	slog.DebugContext(ctx, "setup underlay", "step", "setting up loopback interface")
	defer slog.DebugContext(ctx, "setup underlay", "step", "loopback interface set up")

	if err := netnamespace.In(ns, func() error {
		loopback, err := netlink.LinkByName(loopbackName)
		if err != nil {
			return fmt.Errorf("ensureLoopback: failed to retrieve %s, err: %w", loopbackName, err)
		}

		err = assignIPToInterface(loopback, vtepIP)
		if err != nil {
			return err
		}
		// The link is already set up during namespace creation. However, in order to be idempotent, do this here again,
		// in case something external set the link to down.
		if err := netlink.LinkSetUp(loopback); err != nil {
			return fmt.Errorf("ensureLoopback: failed to bring up %s: %w", loopbackName, err)
		}

		return nil
	}); err != nil {
		return err
	}

	return nil
}

// RemoveUnderlay removes the underlay state from the named network namespace:
// it resets the group ID on underlay NICs (so HasUnderlayInterface
// returns false on the next reconcile) and clears all IP addresses from the VTEP loopback (lo).
func RemoveUnderlay(ctx context.Context, fromNetNSPath string) error {
	fromNetNS, err := netns.GetFromPath(fromNetNSPath)
	if err != nil {
		return fmt.Errorf("RemoveUnderlay: failed to find network namespace %s: %w", fromNetNSPath, err)
	}
	defer func() {
		if err := fromNetNS.Close(); err != nil {
			slog.Error("failed to close namespace", "namespace", fromNetNSPath, "error", err)
		}
	}()
	defaultNetNS, err := netns.Get()
	if err != nil {
		return fmt.Errorf("RemoveUnderlay: failed to get netns for default namespace: %w", err)
	}
	defer func() {
		if err := defaultNetNS.Close(); err != nil {
			slog.Error("failed to close default namespace", "error", err)
		}
	}()

	fromNetNSHandle, err := netlink.NewHandleAt(fromNetNS)
	if err != nil {
		return fmt.Errorf("RemoveUnderlay: failed to get netlink handle for namespace %s: %w", fromNetNSPath, err)
	}
	defer fromNetNSHandle.Close()
	defaultNetNSHandle, err := netlink.NewHandleAt(defaultNetNS)
	if err != nil {
		return fmt.Errorf("RemoveUnderlay: failed to get netlink handle for default namespace: %w", err)
	}
	defer defaultNetNSHandle.Close()

	if err := clearNonDefaultLoopbackIPs(fromNetNSHandle, loopbackName); err != nil {
		return fmt.Errorf("RemoveUnderlay: failed to clear non default loopback IPs from interface %s, err: %w",
			loopbackName, err)
	}

	links, err := fromNetNSHandle.LinkList()
	if err != nil {
		return fmt.Errorf("failed to list links: %w", err)
	}
	for _, l := range links {
		if l.Attrs().Group != underlayGroupID {
			continue
		}
		if err = moveInterfaceToNamespace(ctx, l.Attrs().Name, fromNetNSHandle, defaultNetNSHandle, defaultNetNS, 0); err != nil {
			return err
		}
	}

	return nil
}

// HasUnderlayInterface returns true if the given network
// namespace already has a configured underlay interface.
func HasUnderlayInterface(namespace string) (bool, error) {
	ns, err := netns.GetFromPath(namespace)
	if err != nil {
		return false, fmt.Errorf("HasUnderlayInterface: failed to find network namespace %s: %w", namespace, err)
	}
	defer func() {
		if err := ns.Close(); err != nil {
			slog.Error("failed to close namespace", "namespace", namespace, "error", err)
		}
	}()

	ifaces, err := findInterfacesInGroup(ns, underlayGroupID)
	if err != nil {
		return false, fmt.Errorf("failed to find underlay interfaces: %w", err)
	}
	return len(ifaces) > 0, nil
}

// findInterfacesInGroup returns a slice of interface names
// for all interfaces in the namespace that belong to the specified group.
func findInterfacesInGroup(ns netns.NsHandle, groupID uint32) ([]string, error) {
	var result []string
	err := netnamespace.In(ns, func() error {
		links, err := netlink.LinkList()
		if err != nil {
			return fmt.Errorf("failed to list links: %w", err)
		}
		for _, l := range links {
			if l.Attrs().Group == groupID {
				result = append(result, l.Attrs().Name)
			}
		}
		return nil
	})
	return result, err
}

// findUnderlayMTU retrieves the lowest MTU among all underlay interfaces.
// This ensures that packets can traverse all underlay paths.
func findUnderlayMTU(ns netns.NsHandle) (int, error) {
	minMTU := 0
	err := netnamespace.In(ns, func() error {
		links, err := netlink.LinkList()
		if err != nil {
			return fmt.Errorf("failed to list links: %w", err)
		}
		for _, l := range links {
			if l.Attrs().Group == underlayGroupID {
				mtu := l.Attrs().MTU
				if minMTU == 0 || mtu < minMTU {
					minMTU = mtu
				}
			}
		}
		if minMTU == 0 {
			slog.Info("no underlay link found when finding MTU")
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return minMTU, nil
}

func clearNonDefaultLoopbackIPs(handle *netlink.Handle, intf string) error {
	lo, err := handle.LinkByName(intf)
	if err != nil {
		return fmt.Errorf("failed to find %s: %w", intf, err)
	}

	addresses, err := handle.AddrList(lo, netlink.FAMILY_ALL)
	if err != nil {
		return fmt.Errorf("failed to list addresses on %s: %w", intf, err)
	}

	var errs []error
	for _, address := range addresses {
		if address.IP.IsLoopback() {
			continue
		}
		if err := handle.AddrDel(lo, &address); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
