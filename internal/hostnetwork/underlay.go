// SPDX-License-Identifier:Apache-2.0

package hostnetwork

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

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
	UnderlayInterface string
	VtepIP            string
	TargetNS          string
}

func SetupUnderlay(ctx context.Context, params UnderlayParams) error {
	slog.DebugContext(ctx, "setup underlay", "params", params)
	defer slog.DebugContext(ctx, "setup underlay done")
	ns, err := netns.GetFromName(params.TargetNS)
	if err != nil {
		return fmt.Errorf("setupUnderlay: Failed to find network namespace %s: %w", params.TargetNS, err)
	}
	allErrors := []error{}
	defer deferWithError(&allErrors, func() error { return ns.Close() })

	slog.DebugContext(ctx, "setup underlay", "step", "moving loopback interface")
	if err := inNamespace(ns, func() error {
		loopback, err := netlink.LinkByName(UnderlayLoopback)
		if errors.As(err, &netlink.LinkNotFoundError{}) {
			slog.DebugContext(ctx, "setup underlay", "step", "creating loopback interface")
			loopback = &netlink.Dummy{LinkAttrs: netlink.LinkAttrs{Name: UnderlayLoopback}}
			err = netlink.LinkAdd(loopback)
			if err != nil {
				return fmt.Errorf("assignVTEPToLoopback: failed to create loopback underlay")
			}
		}

		err = assignIPToInterface(loopback, params.VtepIP)
		if err != nil {
			return err
		}

		return nil
	}); err != nil {
		allErrors = append(allErrors, err)
		return errors.Join(allErrors...)
	}

	err = moveUnderlayInterface(ctx, params.UnderlayInterface, ns)
	if err != nil {
		allErrors = append(allErrors, err)
		return errors.Join(allErrors...)
	}
	return errors.Join(allErrors...)
}

type UnderlayExistsError string

func (e UnderlayExistsError) Error() string {
	return string(e)
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

	if err := inNamespace(ns, func() error {
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

// findInterfaceWithIP retrieves the interface assigned to the given ip
// in the given network ns.
func findInterfaceWithIP(ns netns.NsHandle, ip string) (string, error) {
	res := ""
	err := inNamespace(ns, func() error {
		links, err := netlink.LinkList()
		if err != nil {
			return fmt.Errorf("failed to list links")
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
