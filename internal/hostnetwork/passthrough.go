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

type PassthroughParams struct {
	TargetNS string
	LinkIPs  LinkIPs
}

var PassthroughNames = VethNames{
	HostSide:      "pt-host",
	NamespaceSide: "pt-ns",
}

// SetupPassthroughInNamespace sets up the passthrough in the already open
// router namespace handle ns.
func SetupPassthroughInNamespace(ctx context.Context, ns netns.NsHandle, params PassthroughParams) error {
	slog.DebugContext(ctx, "setup passthrough", "params", params)
	defer slog.DebugContext(ctx, "setup passthrough done")
	if err := setupNamespacedVeth(ctx, PassthroughNames, ns); err != nil {
		return fmt.Errorf("SetupPassthrough: failed to setup VNI veth: %w", err)
	}

	hostVeth, err := netlink.LinkByName(PassthroughNames.HostSide)
	if errors.As(err, &netlink.LinkNotFoundError{}) {
		return fmt.Errorf("SetupPassthrough: host veth %s does not exist, cannot setup Passthrough", PassthroughNames.HostSide)
	}

	err = AssignIPsToInterface(hostVeth, params.LinkIPs.HostIPv4, params.LinkIPs.HostIPv6)
	if err != nil {
		return fmt.Errorf("failed to assign IPs to host veth: %w", err)
	}

	if err := netnamespace.In(ns, func() error {
		peVeth, err := netlink.LinkByName(PassthroughNames.NamespaceSide)
		if err != nil {
			return fmt.Errorf("could not find peer veth %s in namespace %s: %w", PassthroughNames.NamespaceSide, params.TargetNS, err)
		}

		err = AssignIPsToInterface(peVeth, params.LinkIPs.NSIPv4, params.LinkIPs.NSIPv6)
		if err != nil {
			return fmt.Errorf("failed to assign IPs to PE veth: %w", err)
		}
		return nil
	}); err != nil {
		return err
	}
	return nil
}

// RemovePassthroughInNamespace removes the passthrough from the already open
// router namespace handle ns.
func RemovePassthroughInNamespace(ns netns.NsHandle) error {
	if err := RemoveLinkByName(PassthroughNames.HostSide); err != nil {
		return fmt.Errorf("RemovePassthrough: failed to remove host link %s: %w", PassthroughNames.HostSide, err)
	}

	if err := netnamespace.In(ns, func() error {
		if err := RemoveLinkByName(PassthroughNames.NamespaceSide); err != nil {
			return fmt.Errorf("remove namespace-side veth leg: %w", err)
		}
		return nil
	}); err != nil {
		return err
	}

	return nil
}

func RemoveLinkByName(name string) error {
	link, err := netlink.LinkByName(name)
	if errors.As(err, &netlink.LinkNotFoundError{}) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("remove link by name: failed to get link %s: %w", name, err)
	}
	return netlink.LinkDel(link)
}
