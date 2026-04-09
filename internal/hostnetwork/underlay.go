// SPDX-License-Identifier:Apache-2.0

package hostnetwork

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/openperouter/openperouter/internal/netnamespace"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
)

const (
	UnderlayLoopback = "lound"
)

// used to identify the interface moved into the network ns to serve
// the underlay
const underlayInterfaceSpecialAddr = "172.16.1.1/32"

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
	ns, err := netns.GetFromPath(params.TargetNS)
	if err != nil {
		return fmt.Errorf("setupUnderlay: Failed to find network namespace %s: %w", params.TargetNS, err)
	}
	defer func() {
		if err := ns.Close(); err != nil {
			slog.Error("failed to close namespace", "namespace", params.TargetNS, "error", err)
		}
	}()

	for _, underlayInterface := range params.UnderlayInterfaces {
		if err := moveUnderlayInterface(ctx, underlayInterface, ns); err != nil {
			return err
		}
	}

	if params.EVPN == nil {
		return nil
	}

	if params.EVPN.VtepIP != "" {
		if err := ensureLoopback(ctx, ns, params.EVPN.VtepIP); err != nil {
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
	slog.DebugContext(ctx, "setup underlay", "step", "creating loopback interface")
	defer slog.DebugContext(ctx, "setup underlay", "step", "loopback interface created")

	if err := netnamespace.In(ns, func() error {
		loopback, err := netlink.LinkByName(UnderlayLoopback)
		if errors.As(err, &netlink.LinkNotFoundError{}) {
			slog.DebugContext(ctx, "setup underlay", "step", "creating loopback interface")
			loopback = &netlink.Dummy{LinkAttrs: netlink.LinkAttrs{Name: UnderlayLoopback}}
			if err := netlink.LinkAdd(loopback); err != nil {
				return fmt.Errorf("assignVTEPToLoopback: failed to create loopback underlay - %w", err)
			}
		}

		err = assignIPToInterface(loopback, vtepIP)
		if err != nil {
			return err
		}
		if err := netlink.LinkSetUp(loopback); err != nil {
			return fmt.Errorf("ensureLoopback: failed to bring up %s: %w", UnderlayLoopback, err)
		}
		return nil
	}); err != nil {
		return err
	}

	return nil
}

// moveUnderlayInterface moves the interface to be used for the underlay connectivity in
// the given namespace.
func moveUnderlayInterface(ctx context.Context, underlayInterface string, ns netns.NsHandle) error {
	// Check if this specific interface is already in the target namespace.
	// With multiple underlay interfaces, each is moved independently.
	err := moveInterfaceToNamespace(ctx, underlayInterface, ns)
	if err != nil {
		return err
	}

	if err := netnamespace.In(ns, func() error {
		underlay, err := netlink.LinkByName(underlayInterface)
		if err != nil {
			return fmt.Errorf("failed to get underlay nic by name %s: %w", underlayInterface, err)
		}

		// we assign a special address so we can detect if an interface was already moved.
		// With multiple underlay interfaces only the first one gets the marker IP, to avoid
		// conflicts when the same address cannot be assigned to multiple interfaces.
		nsHasMarkerIP, err := namespaceHasIP(ns, underlayInterfaceSpecialAddr)
		if err != nil {
			return fmt.Errorf("failed to check marker IP in namespace: %w", err)
		}
		if !nsHasMarkerIP {
			if err := assignIPToInterface(underlay, underlayInterfaceSpecialAddr); err != nil {
				return err
			}
		}
		if err := netlink.LinkSetUp(underlay); err != nil {
			return fmt.Errorf("could not set link up for VRF %s: %v", underlay.Attrs().Name, err)
		}
		return nil
	}); err != nil {
		return err
	}
	return nil
}

// namespaceHasIP returns true if any interface in the given namespace has the specified IP.
func namespaceHasIP(ns netns.NsHandle, ip string) (bool, error) {
	result := false
	err := netnamespace.In(ns, func() error {
		links, err := netlink.LinkList()
		if err != nil {
			return fmt.Errorf("failed to list links: %w", err)
		}
		for _, l := range links {
			has, err := interfaceHasIP(l, ip)
			if err != nil {
				return err
			}
			if has {
				result = true
				return nil
			}
		}
		return nil
	})
	return result, err
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

	underlayInterface, err := findInterfaceWithIP(ns, underlayInterfaceSpecialAddr)
	if err != nil {
		return false, fmt.Errorf("failed to get old underlay interface %w", err)
	}
	return underlayInterface != "", nil
}

// findInterfaceWithIP retrieves the interface assigned to the given ip
// in the given network ns.
func findInterfaceWithIP(ns netns.NsHandle, ip string) (string, error) {
	res := ""
	err := netnamespace.In(ns, func() error {
		links, err := netlink.LinkList()
		if err != nil {
			return fmt.Errorf("failed to list links: %w", err)
		}
		for _, l := range links {
			addr, _ := netlink.AddrList(l, netlink.FAMILY_ALL)
			slog.Debug("find underlay", "checking link", l.Attrs().Name, "addresses", addr)
			hasIP, err := interfaceHasIP(l, ip)
			if err != nil {
				return err
			}
			if hasIP {
				res = l.Attrs().Name
				return nil
			}
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if res != "" {
		slog.Debug("returning found has ip", "res", res)
		return res, nil
	}
	slog.Debug("returning not found")
	return "", nil
}
