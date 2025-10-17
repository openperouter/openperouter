// SPDX-License-Identifier:Apache-2.0

package hostnetwork

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
)

type VethNames struct {
	HostSide      string
	NamespaceSide string
}

const VethLinkType = "veth"

// setupVeth sets up a veth pair with the provided names and one leg in the
// given namespace.

func setupNamespacedVeth(ctx context.Context, vethNames VethNames, namespace string) error {
	slog.DebugContext(ctx, "setupNamespacedVeth", "hostSide", vethNames.HostSide, "nsSide", vethNames.NamespaceSide)
	defer slog.DebugContext(ctx, "end setupNamespacedVeth", "hostSide", vethNames.HostSide, "nsSide", vethNames.NamespaceSide)

	targetNS, err := netns.GetFromName(namespace)
	if err != nil {
		return fmt.Errorf("setupNamespacedVeth: Failed to get network namespace %s: %w", namespace, err)
	}
	defer func() {
		if err := targetNS.Close(); err != nil {
			slog.Error("failed to close namespace", "namespace", namespace, "error", err)
		}
	}()

	logger := slog.Default().With("host side", vethNames.HostSide, "pe side", vethNames.NamespaceSide)
	logger.DebugContext(ctx, "setting up veth")

	hostSide, err := createVeth(ctx, logger, vethNames)
	if err != nil {
		return fmt.Errorf("could not create veth for %s - %s: %w", vethNames.HostSide, vethNames.NamespaceSide, err)
	}
	if err = netlink.LinkSetUp(hostSide); err != nil {
		return fmt.Errorf("could not set link up for host leg %s: %v", hostSide.Attrs().Name, err)
	}

	// Let's try to look into the namespace
	err = inNamespace(targetNS, func() error {
		namespaceSideLink, err := netlink.LinkByName(vethNames.NamespaceSide)
		if err != nil {
			return err
		}
		slog.DebugContext(ctx, "pe leg already in ns", "pe veth", namespaceSideLink.Attrs().Name)
		return nil
	})
	if err != nil && !errors.As(err, &netlink.LinkNotFoundError{}) { // real error
		return fmt.Errorf("could not find peer by name for %s: %w", vethNames.HostSide, err)
	}
	if err == nil {
		return nil
	}

	// Not in the namespace, let's try locally
	peerIndex, err := netlink.VethPeerIndex(hostSide)
	if err != nil {
		return fmt.Errorf("could not find peer veth for %s: %w", vethNames.HostSide, err)
	}
	nsSide, err := netlink.LinkByIndex(peerIndex)
	if errors.As(err, &netlink.LinkNotFoundError{}) {
		return fmt.Errorf("peer veth not found by index for %s: %w", vethNames.HostSide, err)
	}

	if err != nil {
		return fmt.Errorf("could not find peer by index for %s: %w", vethNames.HostSide, err)
	}

	if err = netlink.LinkSetNsFd(nsSide, int(targetNS)); err != nil {
		return fmt.Errorf("setupUnderlay: Failed to move %s to network namespace %s: %w", nsSide.Attrs().Name, targetNS.String(), err)
	}
	slog.DebugContext(ctx, "pe leg moved to ns", "pe veth", nsSide.Attrs().Name)

	if err := inNamespace(targetNS, func() error {
		nsSideLink, err := netlink.LinkByName(vethNames.NamespaceSide)
		if err != nil {
			return err
		}
		err = netlink.LinkSetUp(nsSideLink)
		if err != nil {
			return fmt.Errorf("could not set link up for namespace leg %s: %v", vethNames.NamespaceSide, err)
		}
		return nil
	}); err != nil {
		return fmt.Errorf("could not set link up for namespace leg %s: %v", vethNames.NamespaceSide, err)
	}

	slog.DebugContext(ctx, "veth is set up", "hostside", vethNames.HostSide, "peside", vethNames.NamespaceSide)
	return nil
}

func createVeth(ctx context.Context, logger *slog.Logger, vethNames VethNames) (*netlink.Veth, error) {
	toCreate := &netlink.Veth{LinkAttrs: netlink.LinkAttrs{Name: vethNames.HostSide}, PeerName: vethNames.NamespaceSide}

	link, err := netlink.LinkByName(vethNames.HostSide)
	if errors.As(err, &netlink.LinkNotFoundError{}) {
		logger.DebugContext(ctx, "veth does not exist, creating", "name", vethNames.HostSide)
		if err := netlink.LinkAdd(toCreate); err != nil {
			return nil, fmt.Errorf("failed to add veth for vrf %s/%s: %w", vethNames.HostSide, vethNames.NamespaceSide, err)
		}
		logger.DebugContext(ctx, "veth created")
		return toCreate, nil
	}

	if err != nil {
		return nil, fmt.Errorf("failed to get link by name for vrf %s/%s: %w", vethNames.HostSide, vethNames.NamespaceSide, err)
	}

	vethHost, ok := link.(*netlink.Veth)
	if ok {
		return vethHost, nil
	}
	logger.DebugContext(ctx, "link exists, but not a veth, deleting and creating")
	if err := netlink.LinkDel(link); err != nil {
		return nil, fmt.Errorf("failed to delete link %v: %w", link, err)
	}

	if err := netlink.LinkAdd(toCreate); err != nil {
		return nil, fmt.Errorf("failed to add veth for vrf %s/%s: %w", vethNames.HostSide, vethNames.NamespaceSide, err)
	}

	slog.DebugContext(ctx, "veth recreated", "veth", vethNames.HostSide)
	return toCreate, nil
}

const HostVethPrefix = "host-"
const PEVethPrefix = "pe-"

// vethNamesFromVNI returns the names of the veth legs
// corresponding to the default namespace and the target namespace, based on VNI.
func vethNamesFromVNI(vni int) VethNames {
	hostSide := fmt.Sprintf("%s%d", HostVethPrefix, vni)
	peSide := fmt.Sprintf("%s%d", PEVethPrefix, vni)
	return VethNames{HostSide: hostSide, NamespaceSide: peSide}
}

// vniFromHostVeth extracts the VNI (as int) from a host veth name.
func vniFromHostVeth(hostVethName string) (int, error) {
	trimmed := strings.TrimPrefix(hostVethName, HostVethPrefix)
	return strconv.Atoi(trimmed)
}
