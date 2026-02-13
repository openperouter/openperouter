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
	UnderlayInterface string              `json:"underlay_interface"`
	TargetNS          string              `json:"target_ns"`
	EVPN              *UnderlayEVPNParams `json:"evpn"`
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

	if params.UnderlayInterface != "" {
		if err := moveUnderlayInterface(ctx, params.UnderlayInterface, ns); err != nil {
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
		return nil
	}); err != nil {
		return err
	}

	return nil
}

// moveUnderlayInterface moves the interface to be used for the underlay connectivity in
// the given namespace.
func moveUnderlayInterface(ctx context.Context, underlayInterface string, ns netns.NsHandle) error {
	currentUnderlayInterface, err := findInterfaceWithIP(ns, underlayInterfaceSpecialAddr)
	if err != nil {
		return fmt.Errorf("failed to get old underlay interface %w", err)
	}

	if currentUnderlayInterface != "" && currentUnderlayInterface == underlayInterface { // nothing to do
		slog.DebugContext(ctx, "move underlay", "event", "underlay nic already set")
		return nil
	}

	if currentUnderlayInterface != "" && currentUnderlayInterface != underlayInterface { // need to move the old one back
		slog.DebugContext(ctx, "move underlay", "event", "different underlay nic found, removing", "old", currentUnderlayInterface, "new", underlayInterface)
		// given the tricky nature of the operation, better error and let the caller delete the namespace and start the machinery from scratch.
		// moving the underlay is a destructive operation anyway.
		return UnderlayExistsError(fmt.Sprintf("existing underlay found: %s, new is %s", currentUnderlayInterface, underlayInterface))
	}

	err = moveInterfaceToNamespace(ctx, underlayInterface, ns)
	if err != nil {
		return err
	}

	if err := netnamespace.In(ns, func() error {
		underlay, err := netlink.LinkByName(underlayInterface)
		if err != nil {
			return fmt.Errorf("failed to get underlay nic by name %s: %w", underlayInterface, err)
		}

		// we assign a special address so we we can detect if an interface was already moved.
		if err := assignIPToInterface(underlay, underlayInterfaceSpecialAddr); err != nil {
			return err
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
