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
	TargetNS string `json:"namespace"`
	HostVeth Veth   `json:"veth"`
}

var PassthroughNames = VethNames{
	HostSide:      "pt-host",
	NamespaceSide: "pt-ns",
}

func SetupPassthrough(ctx context.Context, params PassthroughParams) error {
	slog.DebugContext(ctx, "setup passthrough", "params", params)
	defer slog.DebugContext(ctx, "setup passthrough done")
	if err := setupNamespacedVeth(ctx, PassthroughNames, params.TargetNS); err != nil {
		return fmt.Errorf("SetupPassthrough: failed to setup VNI veth: %w", err)
	}

	ns, err := netns.GetFromPath(params.TargetNS)
	if err != nil {
		return fmt.Errorf("SetupVNI: Failed to get network namespace %s: %w", params.TargetNS, err)
	}
	defer func() {
		if err := ns.Close(); err != nil {
			slog.Error("failed to close namespace", "namespace", params.TargetNS, "error", err)
		}
	}()

	hostVeth, err := netlink.LinkByName(PassthroughNames.HostSide)
	if errors.As(err, &netlink.LinkNotFoundError{}) {
		return fmt.Errorf("SetupPassthrough: host veth %s does not exist, cannot setup Passthrough", PassthroughNames.HostSide)
	}

	err = assignIPsToInterface(hostVeth, params.HostVeth.HostIPv4, params.HostVeth.HostIPv6)
	if err != nil {
		return fmt.Errorf("failed to assign IPs to host veth: %w", err)
	}

	if err := netnamespace.In(ns, func() error {
		peVeth, err := netlink.LinkByName(PassthroughNames.NamespaceSide)
		if err != nil {
			return fmt.Errorf("could not find peer veth %s in namespace %s: %w", PassthroughNames.NamespaceSide, params.TargetNS, err)
		}

		err = assignIPsToInterface(peVeth, params.HostVeth.NSIPv4, params.HostVeth.NSIPv6)
		if err != nil {
			return fmt.Errorf("failed to assign IPs to PE veth: %w", err)
		}
		return nil
	}); err != nil {
		return err
	}
	return nil
}

func RemovePassthrough(targetNS string) error {
	if err := removeLinkByName(PassthroughNames.HostSide); err != nil {
		return fmt.Errorf("RemovePassthrough: failed to remove host link %s: %w", PassthroughNames.HostSide, err)
	}

	ns, err := netns.GetFromPath(targetNS)
	if err != nil {
		return fmt.Errorf("RemovePassthrough: failed to get network namespace %s: %w", targetNS, err)
	}
	defer func() {
		if err := ns.Close(); err != nil {
			slog.Error("failed to close namespace", "namespace", targetNS, "error", err)
		}
	}()

	if err := netnamespace.In(ns, func() error {
		if err := removeLinkByName(PassthroughNames.NamespaceSide); err != nil {
			return fmt.Errorf("remove namespace-side veth leg: %w", err)
		}
		return nil
	}); err != nil {
		return err
	}

	return nil
}

func removeLinkByName(name string) error {
	link, err := netlink.LinkByName(name)
	if errors.As(err, &netlink.LinkNotFoundError{}) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("remove link by name: failed to get link %s: %w", name, err)
	}
	return netlink.LinkDel(link)
}
