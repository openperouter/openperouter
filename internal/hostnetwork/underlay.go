// SPDX-License-Identifier:Apache-2.0

package hostnetwork

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"slices"

	"github.com/openperouter/openperouter/internal/netnamespace"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
)

const (
	UnderlayLoopback = "lound"
)

// CIDR range used to assign marker IPs to underlay interfaces moved
// into the network namespace. Each interface gets a unique IP from
// this range (172.16.1.1, 172.16.1.2, ...) so we can track them.
const underlayMarkerCIDR = "172.16.1.0/24"

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

	// Check if there are existing underlay interfaces that aren't in the new list.
	// This means the underlay configuration changed and requires a pod restart.
	existingIfaces, err := findInterfacesInCIDR(ns, underlayMarkerCIDR)
	if err != nil {
		return fmt.Errorf("failed to check existing underlay interfaces: %w", err)
	}
	for name := range existingIfaces {
		if !slices.Contains(params.UnderlayInterfaces, name) {
			return UnderlayExistsError(fmt.Sprintf(
				"existing underlay found: %s, new interfaces are %v", name, params.UnderlayInterfaces))
		}
	}

	for i, underlayInterface := range params.UnderlayInterfaces {
		if err := moveInterfaceToNamespace(ctx, underlayInterface, ns); err != nil {
			return err
		}
		if err := netnamespace.In(ns, func() error {
			underlay, err := netlink.LinkByName(underlayInterface)
			if err != nil {
				return fmt.Errorf("failed to get underlay nic by name %s: %w", underlayInterface, err)
			}
			underlayMarkerIP := fmt.Sprintf("172.16.1.%d/32", i+1)
			if err := assignIPToInterface(underlay, underlayMarkerIP); err != nil {
				return err
			}
			if err := netlink.LinkSetUp(underlay); err != nil {
				return fmt.Errorf("could not set link up for VRF %s: %v", underlay.Attrs().Name, err)
			}
			return nil
		}); err != nil {
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

	ifaces, err := findInterfacesInCIDR(ns, underlayMarkerCIDR)
	if err != nil {
		return false, fmt.Errorf("failed to find underlay interfaces: %w", err)
	}
	return len(ifaces) > 0, nil
}

// findInterfacesInCIDR returns a map of interface name to assigned IP
// for all interfaces in the namespace that have an IP within the given CIDR.
func findInterfacesInCIDR(ns netns.NsHandle, cidr string) (map[string]string, error) {
	_, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, fmt.Errorf("invalid CIDR %s: %w", cidr, err)
	}
	result := make(map[string]string)
	err = netnamespace.In(ns, func() error {
		links, err := netlink.LinkList()
		if err != nil {
			return fmt.Errorf("failed to list links: %w", err)
		}
		for _, l := range links {
			addrs, err := netlink.AddrList(l, netlink.FAMILY_ALL)
			if err != nil {
				return fmt.Errorf("failed to list addresses for %s: %w", l.Attrs().Name, err)
			}
			for _, a := range addrs {
				if ipNet.Contains(a.IP) {
					result[l.Attrs().Name] = a.IPNet.String()
					break
				}
			}
		}
		return nil
	})
	return result, err
}

// IsUnderlayMarkerIP returns true if the given address falls within
// the underlay marker CIDR range.
func IsUnderlayMarkerIP(addr string) bool {
	_, ipNet, err := net.ParseCIDR(underlayMarkerCIDR)
	if err != nil {
		return false
	}
	ip, _, err := net.ParseCIDR(addr)
	if err != nil {
		return false
	}
	return ipNet.Contains(ip)
}
