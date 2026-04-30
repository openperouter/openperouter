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

// SetupUnderlay configures the underlay interface in the target namespace.
// It returns the MTU of the underlay interface (0 when in Multus mode where
// no interface is explicitly moved).
func SetupUnderlay(ctx context.Context, params UnderlayParams) (netlink.Link, error) {
	slog.DebugContext(ctx, "setup underlay", "params", params)
	defer slog.DebugContext(ctx, "setup underlay done")
	ns, err := netns.GetFromPath(params.TargetNS)
	if err != nil {
		return nil, fmt.Errorf("setupUnderlay: Failed to find network namespace %s: %w", params.TargetNS, err)
	}
	defer func() {
		if err := ns.Close(); err != nil {
			slog.Error("failed to close namespace", "namespace", params.TargetNS, "error", err)
		}
	}()

	var link netlink.Link
	if params.UnderlayInterface != "" {
		link, err = moveUnderlayInterface(ctx, params.UnderlayInterface, ns)
		if err != nil {
			return nil, err
		}
	}

	if params.EVPN == nil {
		return link, nil
	}

	if params.EVPN.VtepIP != "" {
		if err := ensureLoopback(ctx, ns, params.EVPN.VtepIP); err != nil {
			return nil, err
		}
	}

	return link, nil
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
// the given namespace. It returns the MTU of the underlay interface.
func moveUnderlayInterface(ctx context.Context, underlayInterface string, ns netns.NsHandle) (netlink.Link, error) {
	currentUnderlayInterface, err := findInterfaceWithIP(ns, underlayInterfaceSpecialAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to get old underlay interface %w", err)
	}

	if currentUnderlayInterface != nil && currentUnderlayInterface.Attrs().Name == underlayInterface { // nothing to do
		slog.DebugContext(ctx, "move underlay", "event", "underlay nic already set")
		return currentUnderlayInterface, nil
	}

	if currentUnderlayInterface != nil && currentUnderlayInterface.Attrs().Name != underlayInterface { // need to move the old one back
		slog.DebugContext(ctx, "move underlay", "event", "different underlay nic found, removing", "old", currentUnderlayInterface, "new", underlayInterface)
		// given the tricky nature of the operation, better error and let the caller delete the namespace and start the machinery from scratch.
		// moving the underlay is a destructive operation anyway.
		return nil, UnderlayExistsError(fmt.Sprintf("existing underlay found: %s, new is %s", currentUnderlayInterface, underlayInterface))
	}
	return moveInterfaceToNamespace(ctx, underlayInterface, []string{underlayInterfaceSpecialAddr}, ns)
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
	return underlayInterface != nil, nil
}

// findInterfaceWithIP retrieves the interface link assigned to the given ip
// in the given network ns.
func findInterfaceWithIP(ns netns.NsHandle, ip string) (netlink.Link, error) {
	var res netlink.Link
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
				res = l
				return nil
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if res != nil {
		slog.Debug("returning found has ip", "res", res)
		return res, nil
	}
	slog.Debug("returning not found")
	return nil, nil
}
