// SPDX-License-Identifier:Apache-2.0

package grout

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"math/rand"
	"net"

	"github.com/openperouter/openperouter/internal/hostnetwork"
	"github.com/openperouter/openperouter/internal/netnamespace"
	"github.com/openperouter/openperouter/internal/sysctl"
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

	peRouterNs, err := netns.GetFromPath(params.TargetNS)
	if err != nil {
		return fmt.Errorf("SetupPassthrough: failed to find namespace %s: %w", params.TargetNS, err)
	}
	defer func() {
		if err := peRouterNs.Close(); err != nil {
			slog.Error("failed to close namespace", "namespace", params.TargetNS, "error", err)
		}
	}()

	if err := ensureTapPortInHostNamespace(ctx, client, portName, tapName, "main", peRouterNs); err != nil {
		return err
	}

	// Assign IPs to the host-side TAP (now in host namespace).
	hostTap, err := netlink.LinkByName(tapName)
	if err != nil {
		return fmt.Errorf("host TAP %s not found after move: %w", tapName, err)
	}
	if err := hostnetwork.AssignIPsToInterface(hostTap, params.LinkIPs.HostIPv4, params.LinkIPs.HostIPv6); err != nil {
		return fmt.Errorf("failed to assign IPs to host TAP: %w", err)
	}

	// Assign IPs to the grout port "pt-ns" via grcli.
	if err := ensurePortAddresses(ctx, client, portName, params.LinkIPs.NSIPv4, params.LinkIPs.NSIPv6); err != nil {
		return fmt.Errorf("failed to ensure IPs to grout port: %w", err)
	}

	if err := netnamespace.In(peRouterNs, func() error {
		// Grout creates a NOARP kernel interface for each port. BGP packets leave
		// through the `main` interface but return on the port's kernel interface (grout control plane tap),
		// so rp_filter must be disabled to allow the asymmetric path.
		if err := sysctl.Ensure(sysctl.DisableRPFilter(portName)); err != nil {
			return fmt.Errorf("failed to disable rp_filter on passthrough port %s: %w", portName, err)
		}
		return nil
	}); err != nil {
		return err
	}

	return nil
}

func RemovePassthrough(ctx context.Context, client *Client) error {
	// Remove the host-side TAP if it exists.
	if err := hostnetwork.RemoveLinkByName(hostnetwork.PassthroughNames.HostSide); err != nil {
		return fmt.Errorf("RemovePassthrough: failed to remove host TAP %s: %w", hostnetwork.PassthroughNames.HostSide, err)
	}
	// Delete the grout port so it can be recreated on the next setup.
	portName := hostnetwork.PassthroughNames.NamespaceSide
	if err := client.deletePort(ctx, portName); err != nil {
		return fmt.Errorf("RemovePassthrough: failed to delete grout port %s: %w", portName, err)
	}
	return nil
}

// ensureTapPortInNamespace is idempotent: it creates the grout port and
// moves the TAP to the host namespace if needed, and recovers from partial
// setups where the port exists but the TAP is missing from the host NS.
func ensureTapPortInHostNamespace(ctx context.Context, client *Client, portName, tapName, vrf string, groutNs netns.NsHandle) error {
	portExists, err := client.portExists(ctx, portName)
	if err != nil {
		return fmt.Errorf("failed to check grout port %s: %w", portName, err)
	}

	if !portExists {
		slog.InfoContext(ctx, "creating grout port",
			"port", portName, "tap", tapName)
		devargs := fmt.Sprintf("net_tap%s,iface=%s", makeTapRandomString(), tapName)
		if err := client.ensurePortInVRF(ctx, portName, devargs, vrf); err != nil {
			return fmt.Errorf("failed to create grout port %s in VRF %s: %w", portName, vrf, err)
		}
	}

	tapExistsInHostNs, err := hostnetwork.LinkExists(tapName)
	if err != nil {
		return fmt.Errorf("failed to check if TAP %s exists: %w", tapName, err)
	}
	if !tapExistsInHostNs {
		slog.InfoContext(ctx, "moving TAP to host namespace", "tap", tapName)

		if err := moveLinkToHostNamespace(ctx, tapName, groutNs); err != nil {
			return fmt.Errorf("failed to move TAP to host namespace: %w", err)
		}

		if err := setUniqueMAC(tapName); err != nil {
			return fmt.Errorf("failed to set unique MAC on host TAP: %w", err)
		}
	}

	return nil
}

// setUniqueMAC assigns a deterministic locally-administered unicast MAC
// derived from the link name. TAP devices share the same MAC on both ends,
// which causes IPv6 DAD failures on the link-local address and prevents NDP
// from working. Giving the host-side TAP its own MAC avoids both problems.
// Deriving it from the name keeps the address stable across restarts.
func setUniqueMAC(name string) error {
	link, err := netlink.LinkByName(name)
	if err != nil {
		return fmt.Errorf("link %s not found: %w", name, err)
	}

	hash := sha256.Sum256([]byte(name))
	mac := net.HardwareAddr(hash[:6])
	mac[0] = (mac[0] | 0x02) & 0xfe // locally administered, unicast

	// Bring the link down before changing the MAC, then back up.
	// This ensures the kernel regenerates the link-local address
	// from the new MAC.
	if err := netlink.LinkSetDown(link); err != nil {
		return fmt.Errorf("failed to bring %s down: %w", name, err)
	}
	if err := netlink.LinkSetHardwareAddr(link, mac); err != nil {
		return fmt.Errorf("failed to set MAC on %s: %w", name, err)
	}
	if err := netlink.LinkSetUp(link); err != nil {
		return fmt.Errorf("failed to bring %s up: %w", name, err)
	}
	return nil
}

// moveLinkToHostNamespace moves a link from the given namespace to the current
// (host) namespace. If the link is already in the host namespace, this is a no-op.
func moveLinkToHostNamespace(ctx context.Context, name string, srcNS netns.NsHandle) error {
	srcNsHandle, err := netlink.NewHandleAt(srcNS)
	if err != nil {
		return fmt.Errorf("SetupUnderlay: failed to get netlink handle for namespace %s: %w", srcNS.String(), err)
	}
	defer srcNsHandle.Close()

	defaultNetNS, err := netns.Get()
	if err != nil {
		return fmt.Errorf("SetupUnderlay: failed to get netns handle for default namespace: %w", err)
	}
	defer func() {
		if err := defaultNetNS.Close(); err != nil {
			slog.Error("failed to close default namespace", "error", err)
		}
	}()

	defaultNetNSHandle, err := netlink.NewHandleAt(defaultNetNS)
	if err != nil {
		return fmt.Errorf("SetupUnderlay: failed to get netlink handle for default namespace: %w", err)
	}
	defer defaultNetNSHandle.Close()

	if err := hostnetwork.MoveInterfaceToNamespace(ctx, name, srcNsHandle, defaultNetNSHandle, defaultNetNS, 0); err != nil {
		return fmt.Errorf("failed to move interface %s to host namespace: %w", name, err)
	}
	return nil
}

// ensurePortAddresses ensures that the port has the given addresses assigned.
// It deletes any addresses that are not in the new list.
func ensurePortAddresses(ctx context.Context, client *Client, portName string, ipv4, ipv6 string) error {
	if ipv4 == "" && ipv6 == "" {
		return fmt.Errorf("at least one IP address must be provided (IPv4 or IPv6)")
	}

	oldPortAddresses, err := client.getAddresses(ctx, portName)
	if err != nil {
		return fmt.Errorf("failed to get addresses for port %s: %w", portName, err)
	}

	// Delete old addresses that are not in the new list.
	for _, addr := range oldPortAddresses {
		if addr == ipv4 || addr == ipv6 {
			continue
		}

		ipAddr, _, err := net.ParseCIDR(addr)
		if err != nil {
			return fmt.Errorf("failed to parse CIDR %s: %w", addr, err)
		}

		if ipAddr.IsLinkLocalUnicast() {
			// do not delete link-local addresses
			continue
		}

		if err := client.deleteAddress(ctx, portName, addr); err != nil {
			return fmt.Errorf("failed to delete address %s from port %s: %w", addr, portName, err)
		}
	}

	// Assign new addresses.
	for _, addr := range []string{ipv4, ipv6} {
		if addr == "" {
			continue
		}
		if err := client.ensureAddress(ctx, portName, addr); err != nil {
			return fmt.Errorf("failed to assign IP %s to port %s: %w", addr, portName, err)
		}
	}

	return nil
}

func makeTapRandomString() string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"

	b := make([]byte, 8)
	for i := range b {
		b[i] = charset[rand.Intn(len(charset))]
	}
	return string(b)
}
