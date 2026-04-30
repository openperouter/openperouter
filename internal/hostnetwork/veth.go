// SPDX-License-Identifier:Apache-2.0

package hostnetwork

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/openperouter/openperouter/internal/netnamespace"
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

	targetNS, err := netns.GetFromPath(namespace)
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
	err = netnamespace.In(targetNS, func() error {
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

	// Not in the namespace, let's try locally.
	// The peer may be gone entirely (netns destroyed) or the stale peer index
	// may now point to an unrelated link (e.g. a bridge that reused the index).
	// In either case, delete the orphaned host-side and recreate the pair.
	nsSide, err := findOrRecreateVethPeer(ctx, logger, hostSide, vethNames)
	if err != nil {
		return err
	}

	if err = netlink.LinkSetNsFd(nsSide, int(targetNS)); err != nil {
		return fmt.Errorf("setupUnderlay: Failed to move %s to network namespace %s: %w", nsSide.Attrs().Name, targetNS.String(), err)
	}
	slog.DebugContext(ctx, "pe leg moved to ns", "pe veth", nsSide.Attrs().Name)

	if err := netnamespace.In(targetNS, func() error {
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

// findOrRecreateVethPeer locates the namespace-side veth peer locally.
// If the peer is missing (netns destroyed) or the stale peer index now
// points to an unrelated link (index reuse), the orphaned host-side is
// deleted and the pair is recreated.
func findOrRecreateVethPeer(ctx context.Context, logger *slog.Logger, hostSide *netlink.Veth, vethNames VethNames) (netlink.Link, error) {
	peerIndex, err := netlink.VethPeerIndex(hostSide)
	if err == nil {
		nsSide, linkErr := netlink.LinkByIndex(peerIndex)
		if linkErr == nil && nsSide.Attrs().Name == vethNames.NamespaceSide {
			return nsSide, nil
		}
		// Stale index: either the link is gone or it's an unrelated link
		// (e.g. a bridge that reused the freed index).
		slog.DebugContext(ctx, "peer veth gone or stale index, recreating veth pair",
			"hostSide", vethNames.HostSide, "foundLink", linkName(nsSide), "linkErr", linkErr)
	} else {
		slog.DebugContext(ctx, "peer veth gone, recreating veth pair", "hostSide", vethNames.HostSide)
	}

	if delErr := netlink.LinkDel(hostSide); delErr != nil {
		return nil, fmt.Errorf("could not delete orphaned veth %s: %w", vethNames.HostSide, delErr)
	}
	newHost, err := createVeth(ctx, logger, vethNames)
	if err != nil {
		return nil, fmt.Errorf("could not recreate veth for %s - %s: %w", vethNames.HostSide, vethNames.NamespaceSide, err)
	}
	if err = netlink.LinkSetUp(newHost); err != nil {
		return nil, fmt.Errorf("could not set link up for recreated host leg %s: %v", newHost.Attrs().Name, err)
	}
	peerIndex, err = netlink.VethPeerIndex(newHost)
	if err != nil {
		return nil, fmt.Errorf("could not find peer veth for %s after recreate: %w", vethNames.HostSide, err)
	}
	nsSide, err := netlink.LinkByIndex(peerIndex)
	if err != nil {
		return nil, fmt.Errorf("peer veth not found by index for %s after recreate: %w", vethNames.HostSide, err)
	}
	return nsSide, nil
}

func linkName(l netlink.Link) string {
	if l == nil {
		return "<nil>"
	}
	return l.Attrs().Name
}
