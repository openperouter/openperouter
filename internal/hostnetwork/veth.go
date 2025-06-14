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

// setupVeth sets up a veth pair with the name generated from the given VNI and one leg in the
// given namespace.
func setupVeth(ctx context.Context, vni int, targetNS netns.NsHandle) (netlink.Link, error) {
	logger := slog.Default().With("VNI", vni)
	logger.DebugContext(ctx, "setting up veth")

	hostSide, err := createVeth(ctx, logger, vni)
	if err != nil {
		return nil, fmt.Errorf("could not create veth for VNI %d: %w", vni, err)
	}

	var peSideNs netlink.Link
	// Let's try to look into the namespace
	err = inNamespace(targetNS, func() error {
		_, peSideName := vethNamesFromVNI(vni)
		peSideNs, err = netlink.LinkByName(peSideName)
		if err != nil {
			return err
		}
		slog.DebugContext(ctx, "pe leg already in ns", "pe veth", peSideNs.Attrs().Name)
		return nil
	})
	if err != nil && !errors.As(err, &netlink.LinkNotFoundError{}) { // real error
		return nil, fmt.Errorf("could not find peer by name for %s: %w", hostSide.Name, err)
	}
	if err == nil {
		return hostSide, nil
	}

	// Not in the namespace, let's try locally
	peerIndex, err := netlink.VethPeerIndex(hostSide)
	if err != nil {
		return nil, fmt.Errorf("could not find peer veth for %s: %w", hostSide.Name, err)
	}
	peSide, err := netlink.LinkByIndex(peerIndex)

	if err != nil && !errors.As(err, &netlink.LinkNotFoundError{}) { // real error
		return nil, fmt.Errorf("could not find peer by index for %s: %w", hostSide.Name, err)
	}

	if err = netlink.LinkSetNsFd(peSide, int(targetNS)); err != nil {
		return nil, fmt.Errorf("setupUnderlay: Failed to move %s to network namespace %s: %w", peSide.Attrs().Name, targetNS.String(), err)
	}
	slog.DebugContext(ctx, "pe leg moved to ns", "pe veth", peSide.Attrs().Name)

	slog.DebugContext(ctx, "veth is set up", "vni", vni)
	return hostSide, nil
}

func createVeth(ctx context.Context, logger *slog.Logger, vni int) (*netlink.Veth, error) {
	hostSide, peSide := vethNamesFromVNI(vni)
	toCreate := &netlink.Veth{LinkAttrs: netlink.LinkAttrs{Name: hostSide}, PeerName: peSide}

	link, err := netlink.LinkByName(hostSide)
	if errors.As(err, &netlink.LinkNotFoundError{}) {
		logger.DebugContext(ctx, "veth does not exist, creating", "name", hostSide)
		if err := netlink.LinkAdd(toCreate); err != nil {
			return nil, fmt.Errorf("failed to add veth for vrf %s/%s: %w", hostSide, peSide, err)
		}
		logger.DebugContext(ctx, "veth created")
		return toCreate, nil
	}

	if err != nil {
		return nil, fmt.Errorf("failed to get link by name for vrf %s/%s: %w", hostSide, peSide, err)
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
		return nil, fmt.Errorf("failed to add veth for vrf %s/%s: %w", hostSide, peSide, err)
	}

	slog.DebugContext(ctx, "veth recreated", "veth", hostSide)
	return toCreate, nil
}

const HostVethPrefix = "host-"
const PEVethPrefix = "pe-"

// vethNamesFromVNI returns the names of the veth legs
// corresponding to the default namespace and the target namespace, based on VNI.
func vethNamesFromVNI(vni int) (string, string) {
	hostSide := fmt.Sprintf("%s%d", HostVethPrefix, vni)
	peSide := fmt.Sprintf("%s%d", PEVethPrefix, vni)
	return hostSide, peSide
}

// vniFromHostVeth extracts the VNI (as int) from a host veth name.
func vniFromHostVeth(hostVethName string) (int, error) {
	trimmed := strings.TrimPrefix(hostVethName, HostVethPrefix)
	return strconv.Atoi(trimmed)
}
