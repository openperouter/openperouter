// SPDX-License-Identifier:Apache-2.0

package grout

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/openperouter/openperouter/internal/hostnetwork"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
)

// SetupPassthrough configures the passthrough interface via the grout dataplane.
// It creates a grout port "pt-ns" with a kernel TAP "pt-host", moves the TAP
// to the host namespace, and assigns IPs to both sides.
func SetupPassthrough(ctx context.Context, client *Client, params hostnetwork.PassthroughParams) error {
	slog.DebugContext(ctx, "setup passthrough", "params", params)
	defer slog.DebugContext(ctx, "setup passthrough done")

	tapName := hostnetwork.PassthroughNames.HostSide       // "pt-host"
	portName := hostnetwork.PassthroughNames.NamespaceSide // "pt-ns"

	ns, err := netns.GetFromPath(params.TargetNS)
	if err != nil {
		return fmt.Errorf("SetupPassthrough: failed to find namespace %s: %w", params.TargetNS, err)
	}
	defer func() {
		if err := ns.Close(); err != nil {
			slog.Error("failed to close namespace", "namespace", params.TargetNS, "error", err)
		}
	}()

	// Check if the grout port already exists. If it does, the TAP was already
	// moved to the host namespace on a previous reconciliation — skip creation.
	portExists, err := client.portExists(ctx, portName)
	if err != nil {
		return fmt.Errorf("failed to check grout port %s: %w", portName, err)
	}

	if !portExists {
		// Remove any stale TAP from a previous run in the host namespace.
		_ = removeLinkByName(tapName)

		// Create grout port "pt-ns" with TAP "pt-host" in the router pod's namespace.
		devargs := fmt.Sprintf("net_tap1,iface=%s", tapName)
		if err := client.ensurePort(ctx, portName, devargs); err != nil {
			return fmt.Errorf("failed to create grout passthrough port: %w", err)
		}

		// Move TAP "pt-host" from the router pod's namespace to the host namespace
		// so that FRR-K8s (running on the host) can peer with it.
		if err := moveLinkToHostNamespace(ctx, tapName, ns); err != nil {
			return fmt.Errorf("failed to move TAP to host namespace: %w", err)
		}

		// DPDK TAP devices share the same MAC on both ends. Give the host-side
		// TAP its own MAC so that IPv6 DAD succeeds and NDP works correctly.
		if err := setUniqueMAC(tapName); err != nil {
			return fmt.Errorf("failed to set unique MAC on host TAP: %w", err)
		}
	} else {
		slog.InfoContext(ctx, "grout passthrough port already exists, skipping TAP setup", "port", portName)
	}

	// Assign IPs to the host-side TAP (now in host namespace).
	hostTap, err := netlink.LinkByName(tapName)
	if err != nil {
		return fmt.Errorf("host TAP %s not found after move: %w", tapName, err)
	}
	if err := assignIPsToLink(hostTap, params.HostVeth.HostIPv4, params.HostVeth.HostIPv6); err != nil {
		return fmt.Errorf("failed to assign IPs to host TAP: %w", err)
	}

	// Assign IPs to the grout port "pt-ns" via grcli.
	if err := assignIPsToGroutPort(ctx, client, portName, params.HostVeth.NSIPv4, params.HostVeth.NSIPv6); err != nil {
		return fmt.Errorf("failed to assign IPs to grout port: %w", err)
	}

	portAddresses, err := client.getAddresses(ctx, portName)
	if err != nil {
		return fmt.Errorf("failed to get grout %s port addresses: %w", portName, err)
	}

	for _, addr := range portAddresses {
		if err := ensureKernelSubnetRoute(ns, "main", addr); err != nil {
			return fmt.Errorf("failed to add kernel route for underlay subnet %s: %w", addr, err)
		}
	}

	return nil
}

func RemovePassthrough(ctx context.Context, client *Client, targetNS string) error {
	// Remove the host-side TAP if it exists.
	if err := removeLinkByName(hostnetwork.PassthroughNames.HostSide); err != nil {
		return fmt.Errorf("RemovePassthrough: failed to remove host TAP %s: %w", hostnetwork.PassthroughNames.HostSide, err)
	}
	// Delete the grout port so it can be recreated on the next setup.
	portName := hostnetwork.PassthroughNames.NamespaceSide
	if err := client.deletePort(ctx, portName); err != nil {
		return fmt.Errorf("RemovePassthrough: failed to delete grout port %s: %w", portName, err)
	}
	return nil
}
