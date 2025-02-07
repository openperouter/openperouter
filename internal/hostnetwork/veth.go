// SPDX-License-Identifier:Apache-2.0

package hostnetwork

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
)

// setupVeth sets up a veth pair with the name generated from the given name and one leg in the
// given namespace.
func setupVeth(ctx context.Context, name string, targetNS netns.NsHandle) (netlink.Link, netlink.Link, error) {
	logger := slog.Default().With("veth", name)
	logger.DebugContext(ctx, "setting up veth")
	hostSide, _ := vethNamesFromVRF(name)

	link, err := netlink.LinkByName(hostSide)
	if err != nil && errors.As(err, &netlink.LinkNotFoundError{}) {
		logger.DebugContext(ctx, "veth does not exist, creating")
		link, err = createVeth(name)
		if err != nil {
			return nil, nil, err
		}
	}

	vethHost, ok := link.(*netlink.Veth)
	if !ok {
		logger.DebugContext(ctx, "veth exists, but not a veth, deleting and creating")
		err := netlink.LinkDel(link)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to delete link %v: %w", link, err)
		}
		vethHost, err = createVeth(name)
		if err != nil {
			return nil, nil, fmt.Errorf("failed create veth %s: %w", name, err)
		}
	}
	slog.DebugContext(ctx, "veth created veth", "veth", name)
	peerIndex, err := netlink.VethPeerIndex(vethHost)
	if err != nil {
		return nil, nil, fmt.Errorf("could not find peer veth for %s: %w", name, err)
	}
	vethPE, err := netlink.LinkByIndex(peerIndex)
	alreadyInNamespace := false
	if err != nil && errors.As(err, &netlink.LinkNotFoundError{}) { // Try to look into the namespace
		if err := inNamespace(targetNS, func() error {
			vethPE, err = netlink.LinkByIndex(peerIndex)
			if err != nil {
				return err
			}
			return nil
		}); err != nil {
			return nil, nil, fmt.Errorf("could not find peer veth by index for %s: %w", name, err)
		}
		slog.DebugContext(ctx, "pe leg already in ns", "pe veth", vethPE.Attrs().Name)
		alreadyInNamespace = true
	}

	if !alreadyInNamespace {
		if err = netlink.LinkSetNsFd(vethPE, int(targetNS)); err != nil {
			return nil, nil, fmt.Errorf("setupUnderlay: Failed to move %s to network namespace %s: %w", vethPE.Attrs().Name, targetNS.String(), err)
		}
		slog.DebugContext(ctx, "pe leg moved to ns", "pe veth", vethPE.Attrs().Name)
	}

	slog.DebugContext(ctx, "veth is up", "vrf", name)
	return vethHost, vethPE, nil
}

func createVeth(vrfName string) (*netlink.Veth, error) {
	hostSide, peSide := vethNamesFromVRF(vrfName)
	vethHost := &netlink.Veth{LinkAttrs: netlink.LinkAttrs{Name: hostSide}, PeerName: peSide}
	err := netlink.LinkAdd(vethHost)
	if err != nil {
		return nil, fmt.Errorf("could not add veth %s: %w", vrfName, err)
	}
	return vethHost, nil
}

const HostVethPrefix = "host"
const PEVethPrefix = "pe"

// vethNamesFromVRF returns the names of the veth legs
// corresponding to the default namespace and the target namespace.
func vethNamesFromVRF(name string) (string, string) {
	hostSide := HostVethPrefix + name
	peSide := PEVethPrefix + name
	return hostSide, peSide
}

func vrfFromHostVeth(hostVethName string) string {
	return strings.TrimPrefix(hostVethName, HostVethPrefix)
}
