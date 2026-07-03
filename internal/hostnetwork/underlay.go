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

// UnderlayGroupID is the link group ID assigned to all underlay interfaces
// moved into the network namespace. This allows us to identify and query
// all underlay interfaces by their group membership.
const UnderlayGroupID = 4242

type UnderlayParams struct {
	UnderlayInterfaces []string                      `json:"underlay_interfaces"`
	TargetNS           string                        `json:"target_ns"`
	TunnelEndpoint     *UnderlayTunnelEndpointParams `json:"tunnel_endpoint"`
}

type UnderlayTunnelEndpointParams struct {
	IPv4CIDR string `json:"ipv4_cidr"`
	IPv6CIDR string `json:"ipv6_cidr"`
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

	// If any existing underlay interfaces were removed from the new list,
	// restore them to the default namespace before moving the new ones in.
	// The caller (setupUnderlayWithCleanup) tears down VNIs beforehand
	// since they are bound to the old underlay and would be non-functional.
	// New interfaces are handled below by moveInterfaceToNamespace.
	existingIfaces, err := FindInterfacesInGroup(targetNetNS, UnderlayGroupID)
	if err != nil {
		return fmt.Errorf("failed to check existing underlay interfaces: %w", err)
	}
	if UnderlayInterfacesWereRemoved(existingIfaces, params.UnderlayInterfaces) {
		slog.InfoContext(ctx, "underlay interfaces changed, restoring old interfaces before setup",
			"existing", existingIfaces, "requested", params.UnderlayInterfaces)
		if err := RestoreUnderlay(ctx, params.TargetNS); err != nil {
			return fmt.Errorf("failed to restore old underlay interfaces: %w", err)
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
		if err := MoveInterfaceToNamespace(ctx, underlayInterface, defaultNetNSHandle, targetNetNSHandle, targetNetNS,
			UnderlayGroupID); err != nil {
			return err
		}
	}

	if params.TunnelEndpoint == nil {
		return nil
	}

	vtepIPs := make([]string, 0, 2)
	if ip := params.TunnelEndpoint.IPv4CIDR; ip != "" {
		vtepIPs = append(vtepIPs, ip)
	}
	if ip := params.TunnelEndpoint.IPv6CIDR; ip != "" {
		vtepIPs = append(vtepIPs, ip)
	}
	if err := ensureLoopback(ctx, targetNetNS, vtepIPs...); err != nil {
		return err
	}

	return nil
}

func UnderlayInterfacesWereRemoved(existing, requested []string) bool {
	if len(existing) == 0 {
		return false
	}
	for _, name := range existing {
		if !slices.Contains(requested, name) {
			return true
		}
	}
	return false
}

// UnderlayInterfaces returns the names of all underlay interfaces
// currently present in the given network namespace.
func UnderlayInterfaces(namespace string) ([]string, error) {
	ns, err := netns.GetFromPath(namespace)
	if err != nil {
		return nil, fmt.Errorf("UnderlayInterfaces: failed to find network namespace %s: %w", namespace, err)
	}
	defer func() {
		if err := ns.Close(); err != nil {
			slog.Error("failed to close namespace", "namespace", namespace, "error", err)
		}
	}()

	return FindInterfacesInGroup(ns, UnderlayGroupID)
}

func ensureLoopback(ctx context.Context, ns netns.NsHandle, vtepIPs ...string) error {
	slog.DebugContext(ctx, "setup underlay", "step", "setting up loopback interface")
	defer slog.DebugContext(ctx, "setup underlay", "step", "loopback interface set up")

	if err := netnamespace.In(ns, func() error {
		loopback, err := netlink.LinkByName(loopbackName)
		if err != nil {
			return fmt.Errorf("ensureLoopback: failed to retrieve %s, err: %w", loopbackName, err)
		}

		for _, vtepIP := range vtepIPs {
			err = AssignIPToInterface(loopback, vtepIP)
			if err != nil {
				return err
			}
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

// RestoreUnderlay restores the underlay state:
//   - it clears all non-default IP addresses from the loopback in the namespace identified by fromNetNSPath
//     (`/var/run/netns/perouter`).
//   - it moves the interfaces that are identified by the groupID marker from the aforementioned namespace back to the
//     default network namespace.
func RestoreUnderlay(ctx context.Context, fromNetNSPath string) error {
	restoreUnderlay := func(ctx context.Context, fromNetNSHandle, defaultNetNSHandle *netlink.Handle,
		defaultNetNS netns.NsHandle) error {
		if err := clearNonDefaultLoopbackIPs(fromNetNSHandle, loopbackName); err != nil {
			return fmt.Errorf("RestoreUnderlay: failed to clear non default loopback IPs from interface %s, err: %w",
				loopbackName, err)
		}

		links, err := fromNetNSHandle.LinkList()
		if err != nil {
			return fmt.Errorf("failed to list links: %w", err)
		}

		var errs []error
		for _, l := range links {
			if l.Attrs().Group != UnderlayGroupID {
				continue
			}
			if err = MoveInterfaceToNamespace(ctx, l.Attrs().Name, fromNetNSHandle, defaultNetNSHandle, defaultNetNS,
				0); err != nil {
				errs = append(errs, err)
				continue
			}
		}
		return errors.Join(errs...)
	}

	return restoreUnderlayWithHandles(ctx, fromNetNSPath, restoreUnderlay)
}

// restoreUnderlayWithHandles sets up the required netns and netlink handles for RestoreUnderlay and then calls
// restoreFn with those handles.
func restoreUnderlayWithHandles(ctx context.Context, fromNetNSPath string,
	restoreFn func(context.Context, *netlink.Handle, *netlink.Handle, netns.NsHandle) error) error {
	fromNetNS, err := netns.GetFromPath(fromNetNSPath)
	if err != nil {
		return fmt.Errorf("RestoreUnderlay: failed to find network namespace %s: %w", fromNetNSPath, err)
	}
	defer func() {
		if err := fromNetNS.Close(); err != nil {
			slog.Error("failed to close namespace", "namespace", fromNetNSPath, "error", err)
		}
	}()

	defaultNetNS, err := netns.Get()
	if err != nil {
		return fmt.Errorf("RestoreUnderlay: failed to get netns for default namespace: %w", err)
	}
	defer func() {
		if err := defaultNetNS.Close(); err != nil {
			slog.Error("failed to close default namespace", "error", err)
		}
	}()

	fromNetNSHandle, err := netlink.NewHandleAt(fromNetNS)
	if err != nil {
		return fmt.Errorf("RestoreUnderlay: failed to get netlink handle for namespace %s: %w", fromNetNSPath, err)
	}
	defer fromNetNSHandle.Close()

	defaultNetNSHandle, err := netlink.NewHandleAt(defaultNetNS)
	if err != nil {
		return fmt.Errorf("RestoreUnderlay: failed to get netlink handle for default namespace: %w", err)
	}
	defer defaultNetNSHandle.Close()

	return restoreFn(ctx, fromNetNSHandle, defaultNetNSHandle, defaultNetNS)
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

	ifaces, err := FindInterfacesInGroup(ns, UnderlayGroupID)
	if err != nil {
		return false, fmt.Errorf("failed to find underlay interfaces: %w", err)
	}
	return len(ifaces) > 0, nil
}

// findInterfacesInGroup returns a slice of interface names
// for all interfaces in the namespace that belong to the specified group.
func FindInterfacesInGroup(ns netns.NsHandle, groupID uint32) ([]string, error) {
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
			if l.Attrs().Group == UnderlayGroupID {
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

func clearNonDefaultLoopbackIPs(nsHandle *netlink.Handle, intf string) error {
	lo, err := nsHandle.LinkByName(intf)
	if err != nil {
		return fmt.Errorf("failed to find %s: %w", intf, err)
	}

	addresses, err := nsHandle.AddrList(lo, netlink.FAMILY_ALL)
	if err != nil {
		return fmt.Errorf("failed to list addresses on %s: %w", intf, err)
	}

	var errs []error
	for _, address := range addresses {
		if address.IP.IsLoopback() {
			continue
		}
		if err := nsHandle.AddrDel(lo, &address); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
