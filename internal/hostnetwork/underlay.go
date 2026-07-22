// SPDX-License-Identifier:Apache-2.0

package hostnetwork

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/openperouter/openperouter/internal/cniinvoker"
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
	// UnderlayInterfaces are the underlay interfaces to provision: either
	// host network devices moved into the namespace or CNI-provisioned
	// interfaces; an underlay uses one mode or the other.
	UnderlayInterfaces []UnderlayInterface           `json:"underlay_interfaces"`
	TargetNS           string                        `json:"target_ns"`
	TunnelEndpoint     *UnderlayTunnelEndpointParams `json:"tunnel_endpoint"`
}

// UnderlayInterface describes how a single underlay interface is
// provisioned.
type UnderlayInterface struct {
	// InterfaceName tells the nic name inside the router pod.
	InterfaceName string `json:"interfaceName"`
	// Kind tells how the interface is provisioned.
	Kind UnderlayInterfaceKind `json:"kind"`
	// CNI holds the CNI provisioning data; set when Kind is
	// UnderlayInterfaceCNIDev.
	CNI *CNIDeviceParams `json:"cni,omitempty"`
}

// CNIDeviceParams holds the data needed to provision an underlay interface
// through a CNI plugin.
type CNIDeviceParams struct {
	// Config is the CNI conf or conflist JSON.
	Config []byte `json:"config"`
	// CapabilityArgs are the runtime parameters forwarded to the plugin as
	// capability arguments (the CNI runtimeConfig).
	CapabilityArgs map[string]any `json:"capability_args,omitempty"`
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

	// If any existing underlay interfaces were removed from the new list,
	// clean them up before setting up the new ones: network devices are
	// restored to the default namespace, CNI-provisioned interfaces are
	// deleted with a CNI DEL. The caller tears down VNIs beforehand since
	// they are bound to the old underlay and would be non-functional.
	existing, err := UnderlayInterfaces(params.TargetNS)
	if err != nil {
		return err
	}
	if toRemove := UnderlayInterfacesToRemove(existing, params.UnderlayInterfaces); len(toRemove) > 0 {
		slog.InfoContext(ctx, "underlay interfaces changed, removing old interfaces before setup",
			"removed", toRemove, "requested", params.UnderlayInterfaces)
		if err := RestoreUnderlay(ctx, params.TargetNS, toRemove); err != nil {
			return fmt.Errorf("failed to remove old underlay interfaces: %w", err)
		}
	}

	for _, iface := range params.UnderlayInterfaces {
		switch iface.Kind {
		case UnderlayInterfaceNetDev:
			if err := SetupUnderlayNetDevInterface(ctx, targetNetNS, iface); err != nil {
				return err
			}
		case UnderlayInterfaceCNIDev:
			if err := SetupUnderlayCNIDevInterface(ctx, params.TargetNS, iface); err != nil {
				return err
			}
		default:
			return fmt.Errorf("underlay interface %s has unsupported kind %q", iface.InterfaceName, iface.Kind)
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

// SetupUnderlayNetDevInterface provisions a single underlay net dev interface
func SetupUnderlayNetDevInterface(ctx context.Context, ns netns.NsHandle,
	iface UnderlayInterface) error {
	if err := moveInterfaceFromDefaultNetns(ctx, ns, iface.InterfaceName); err != nil {
		return fmt.Errorf("failed to setup underlay net device %s: %w", iface.InterfaceName, err)
	}
	return nil
}

// SetupUnderlayCNIDevInterface provisions a single underlay cni dev interface
func SetupUnderlayCNIDevInterface(ctx context.Context, ns string,
	iface UnderlayInterface) error {
	if err := cniinvoker.Invoker.Add(ctx, cniinvoker.AddParams{
		Config:         iface.CNI.Config,
		NetNS:          ns,
		IfName:         iface.InterfaceName,
		CapabilityArgs: iface.CNI.CapabilityArgs,
	}); err != nil {
		return fmt.Errorf("failed to setup underlay cni device %s: %w", iface.InterfaceName, err)
	}
	return nil
}

// UnderlayInterfaceKind tells how an underlay interface is provisioned.
type UnderlayInterfaceKind string

const (
	// UnderlayInterfaceNetDev is a host network device moved into the
	// namespace and marked with the underlay group ID.
	UnderlayInterfaceNetDev UnderlayInterfaceKind = "netdev"
	// UnderlayInterfaceCNIDev is provisioned by a CNI plugin and recorded
	// in the libcni result cache.
	UnderlayInterfaceCNIDev UnderlayInterfaceKind = "cnidev"
)

// UnderlayInterfaces returns all the underlay interfaces currently
// provisioned for the given network namespace: the network
// devices marked with the underlay group ID and the CNI-provisioned
// interfaces recorded in the libcni result cache (skipped when no invoker is
// configured).
func UnderlayInterfaces(namespace string) ([]UnderlayInterface, error) {
	ns, err := netns.GetFromPath(namespace)
	if err != nil {
		return nil, fmt.Errorf("UnderlayInterfaces: failed to find network namespace %s: %w", namespace, err)
	}
	defer func() {
		if err := ns.Close(); err != nil {
			slog.Error("failed to close namespace", "namespace", namespace, "error", err)
		}
	}()

	return underlayInterfaces(ns)
}

func underlayInterfaces(ns netns.NsHandle) ([]UnderlayInterface, error) {
	netdevs, err := FindInterfacesInGroup(ns, UnderlayGroupID)
	if err != nil {
		return nil, err
	}
	res := []UnderlayInterface{}
	for _, name := range netdevs {
		res = append(res, UnderlayInterface{InterfaceName: name, Kind: UnderlayInterfaceNetDev})
	}
	if cniinvoker.Invoker == nil {
		return res, nil
	}
	cniIfaces, err := cniinvoker.Invoker.CachedIfNames()
	if err != nil {
		return nil, fmt.Errorf("failed to list cni underlay interfaces: %w", err)
	}
	for _, name := range cniIfaces {
		res = append(res, UnderlayInterface{InterfaceName: name, Kind: UnderlayInterfaceCNIDev})
	}
	return res, nil
}

// UnderlayInterfacesToRemove returns the existing underlay interfaces that
// are not requested anymore, preserving how they were provisioned. An
// interface whose kind changed is returned too, so it is torn down according
// to its old kind before being provisioned with the new one.
func UnderlayInterfacesToRemove(existing,
	requested []UnderlayInterface) []UnderlayInterface {
	requestedByName := make(map[string]UnderlayInterface, len(requested))
	for _, iface := range requested {
		requestedByName[iface.InterfaceName] = iface
	}
	removed := []UnderlayInterface{}
	for _, iface := range existing {
		if req, found := requestedByName[iface.InterfaceName]; !found || req.Kind != iface.Kind {
			removed = append(removed, iface)
		}
	}
	return removed
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
//   - it moves the interfaces to remove that are identified by the groupID marker from the aforementioned
//     namespace back to the default network namespace.
func RestoreUnderlay(ctx context.Context, fromNetNSPath string, ifacesToRemove []UnderlayInterface) error {
	// index the interfaces to remove by name for the link list lookup
	toMoveByName := map[string]UnderlayInterface{}
	for _, ifaceToRemove := range ifacesToRemove {
		switch ifaceToRemove.Kind {
		case UnderlayInterfaceNetDev:
			toMoveByName[ifaceToRemove.InterfaceName] = ifaceToRemove
		case UnderlayInterfaceCNIDev:
			if err := cniinvoker.Invoker.Del(ctx, ifaceToRemove.InterfaceName); err != nil {
				return fmt.Errorf("failed to delete cni underlay interfaces %q: %w", ifaceToRemove.InterfaceName, err)
			}
		}
	}
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
			_, found := toMoveByName[l.Attrs().Name]
			if !found {
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

// FindInterfacesInGroup returns a slice of interface names
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
	underlayInterfaces, err := underlayInterfaces(ns)
	if err != nil {
		return 0, fmt.Errorf("failed finding underlay interfaces to calculate MTU: %w", err)
	}

	underlayNames := make(map[string]struct{}, len(underlayInterfaces))
	for _, iface := range underlayInterfaces {
		underlayNames[iface.InterfaceName] = struct{}{}
	}

	minMTU := 0
	err = netnamespace.In(ns, func() error {
		links, err := netlink.LinkList()
		if err != nil {
			return fmt.Errorf("failed to list links: %w", err)
		}
		for _, l := range links {
			if _, ok := underlayNames[l.Attrs().Name]; ok {
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

// moveDefaultNamespaceInterface moves the host network device into the
// namespace, marking it with the underlay group ID. It is idempotent: a
// device already in place is left untouched.
func moveInterfaceFromDefaultNetns(ctx context.Context, ns netns.NsHandle, name string) error {
	defaultNetNS, err := netns.Get()
	if err != nil {
		return fmt.Errorf("setupNetworkDeviceInterface: failed to get netns handle for default namespace: %w", err)
	}
	defer func() {
		if err := defaultNetNS.Close(); err != nil {
			slog.Error("failed to close default namespace", "error", err)
		}
	}()

	nsHandle, err := netlink.NewHandleAt(ns)
	if err != nil {
		return fmt.Errorf("setupNetworkDeviceInterface: failed to get netlink handle for namespace %s: %w", ns.String(), err)
	}
	defer nsHandle.Close()
	defaultNetNSHandle, err := netlink.NewHandleAt(defaultNetNS)
	if err != nil {
		return fmt.Errorf("setupNetworkDeviceInterface: failed to get netlink handle for default namespace: %w", err)
	}
	defer defaultNetNSHandle.Close()

	return MoveInterfaceToNamespace(ctx, name, defaultNetNSHandle, nsHandle, ns, UnderlayGroupID)
}
