// SPDX-License-Identifier:Apache-2.0

package hostnetwork

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"

	"github.com/openperouter/openperouter/internal/netnamespace"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
)

const (
	UnderlayLoopback = "lound"
)

// underlayGroupID is the link group ID assigned to all underlay interfaces
// moved into the network namespace. This allows us to identify and query
// all underlay interfaces by their group membership.
const underlayGroupID = 4242

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
	// This means the underlay configuration changed and requires rebuilding
	// the network namespace.
	existingIfaces, err := findInterfacesInGroup(ns, underlayGroupID)
	if err != nil {
		return fmt.Errorf("failed to check existing underlay interfaces: %w", err)
	}
	for _, name := range existingIfaces {
		if !slices.Contains(params.UnderlayInterfaces, name) {
			return UnderlayExistsError(fmt.Sprintf(
				"existing underlay found: %s, new interfaces are %v", name, params.UnderlayInterfaces))
		}
	}

	for _, underlayInterface := range params.UnderlayInterfaces {
		if err := moveInterfaceToNamespace(ctx, underlayInterface, ns); err != nil {
			return err
		}
		if err := netnamespace.In(ns, func() error {
			underlay, err := netlink.LinkByName(underlayInterface)
			if err != nil {
				return fmt.Errorf("failed to get underlay nic by name %s: %w", underlayInterface, err)
			}
			// Set group ID only if not already set (idempotent)
			if underlay.Attrs().Group != underlayGroupID {
				if err := netlink.LinkSetGroup(underlay, int(underlayGroupID)); err != nil {
					return fmt.Errorf("failed to set group ID on underlay interface %s: %w", underlayInterface, err)
				}
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

// RemoveUnderlay removes the underlay state from the named network namespace:
// it resets the group ID on underlay NICs (so HasUnderlayInterface
// returns false on the next reconcile) and deletes the VTEP loopback (lound).
func RemoveUnderlay(targetNS string) error {
	ns, err := netns.GetFromPath(targetNS)
	if err != nil {
		return fmt.Errorf("RemoveUnderlay: failed to find network namespace %s: %w", targetNS, err)
	}
	defer func() {
		if err := ns.Close(); err != nil {
			slog.Error("failed to close namespace", "namespace", targetNS, "error", err)
		}
	}()

	return netnamespace.In(ns, func() error {
		lound, err := netlink.LinkByName(UnderlayLoopback)
		if err != nil {
			if !errors.As(err, &netlink.LinkNotFoundError{}) {
				return fmt.Errorf("failed to find %s: %w", UnderlayLoopback, err)
			}
		} else if err := netlink.LinkDel(lound); err != nil {
			return fmt.Errorf("failed to delete %s: %w", UnderlayLoopback, err)
		}

		links, err := netlink.LinkList()
		if err != nil {
			return fmt.Errorf("failed to list links: %w", err)
		}
		for _, l := range links {
			if l.Attrs().Group == underlayGroupID {
				if err := netlink.LinkSetGroup(l, 0); err != nil {
					return fmt.Errorf("failed to reset group ID on %s: %w", l.Attrs().Name, err)
				}
			}
		}
		return nil
	})
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

	ifaces, err := findInterfacesInGroup(ns, underlayGroupID)
	if err != nil {
		return false, fmt.Errorf("failed to find underlay interfaces: %w", err)
	}
	return len(ifaces) > 0, nil
}

// findInterfacesInGroup returns a slice of interface names
// for all interfaces in the namespace that belong to the specified group.
func findInterfacesInGroup(ns netns.NsHandle, groupID uint32) ([]string, error) {
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
	minMTU := 0
	err := netnamespace.In(ns, func() error {
		links, err := netlink.LinkList()
		if err != nil {
			return fmt.Errorf("failed to list links: %w", err)
		}
		for _, l := range links {
			if l.Attrs().Group == underlayGroupID {
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
