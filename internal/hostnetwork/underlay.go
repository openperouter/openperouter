// SPDX-License-Identifier:Apache-2.0

package hostnetwork

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"slices"

	"github.com/openperouter/openperouter/internal/cni"
	"github.com/openperouter/openperouter/internal/netnamespace"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
)

const (
	loopbackName = "lo"
)

// underlayGroupID is the link group ID assigned to all underlay interfaces
// moved into the network namespace. This allows us to identify and query
// all underlay interfaces by their group membership.
const underlayGroupID = 4242

type UnderlayParams struct {
	UnderlayInterfaces []string                      `json:"underlay_interfaces"`
	TargetNS           string                        `json:"target_ns"`
	TunnelEndpoint     *UnderlayTunnelEndpointParams `json:"tunnel_endpoint"`
	// CNI, when set, provisions the underlay interface inside the namespace by
	// invoking a CNI configuration (e.g. from a NetworkAttachmentDefinition)
	// instead of moving a physical nic. Mutually exclusive with UnderlayInterfaces.
	CNI *UnderlayCNIParams `json:"cni,omitempty"`
}

// UnderlayCNIParams holds the data needed to provision the underlay interface
// through a CNI plugin chain directly into the router network namespace.
type UnderlayCNIParams struct {
	// Config is the CNI conf/conflist JSON (the NAD .spec.config, unchanged).
	Config []byte `json:"config"`
	// IfName is the interface name created inside the namespace.
	IfName string `json:"if_name"`
	// BinDirs are the directories searched for CNI plugin binaries.
	BinDirs []string `json:"bin_dirs"`
	// CacheDir is libcni's result cache directory.
	CacheDir string `json:"cache_dir"`
	// Addresses are the per-node IPs (CIDR notation) handed to the IPAM plugin
	// via the CNI "ips" capability.
	Addresses []string `json:"addresses"`
}

// NodeName, when set (k8s mode), is incorporated into the CNI container ID so
// the DHCP client identifier (which the dhcp plugin derives from the container
// ID) is unique per node. Without this every node presents the shared DHCP
// server the same client-id and they collide onto a single lease. It is set
// once at startup (see cmd/hostcontroller).
var NodeName string

// cniContainerIDPrefix is the stable base of the CNI container ID used for the
// underlay attachment in the router netns (the netns is well-known and singular
// per node).
const cniContainerIDPrefix = "perouter-underlay"

// cniContainerID returns the node-scoped CNI container ID for the underlay
// attachment. Node-scoping keeps the derived DHCP client-id unique per node.
func cniContainerID() string {
	if NodeName == "" {
		return cniContainerIDPrefix
	}
	return cniContainerIDPrefix + "-" + NodeName
}

type UnderlayTunnelEndpointParams struct {
	IPv4CIDR string `json:"ipv4_cidr"`
	IPv6CIDR string `json:"ipv6_cidr"`
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
	expected := slices.Clone(params.UnderlayInterfaces)
	if params.CNI != nil {
		expected = append(expected, params.CNI.IfName)
	}
	existingIfaces, err := findInterfacesInGroup(ns, underlayGroupID)
	if err != nil {
		return fmt.Errorf("failed to check existing underlay interfaces: %w", err)
	}
	for _, name := range existingIfaces {
		if !slices.Contains(expected, name) {
			return UnderlayExistsError(fmt.Sprintf(
				"existing underlay found: %s, new interfaces are %v", name, expected))
		}
	}

	if params.CNI != nil {
		if err := setupUnderlayCNI(ctx, ns, params); err != nil {
			return fmt.Errorf("failed to setup underlay via cni: %w", err)
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

	if params.TunnelEndpoint == nil {
		return nil
	}

	vtepIPs := make([]string, 0, 2)
	if ip := params.TunnelEndpoint.IPv4CIDR; ip != "" {
		vtepIPs = append(vtepIPs, ip)
	}
	if ip := params.TunnelEndpoint.IPv6CIDR; ip != "" {
		vtepIPs = append(vtepIPs, ip)
	}
	if err := ensureLoopback(ctx, ns, vtepIPs...); err != nil {
		return err
	}

	return nil
}

type UnderlayExistsError string

func (e UnderlayExistsError) Error() string {
	return string(e)
}

// setupUnderlayCNI provisions the underlay interface inside the namespace by
// invoking the CNI configuration, then marks it as an underlay interface so the
// existing detection/rebuild logic keeps working. It is idempotent: if the
// interface already exists, the CNI ADD is skipped and, for a DHCP IPAM, the
// lease is re-established so a freshly (re)started dhcp daemon resumes renewing
// it (see ensureUnderlayLease).
func setupUnderlayCNI(ctx context.Context, ns netns.NsHandle, params UnderlayParams) error {
	alreadyExists := false
	if err := netnamespace.In(ns, func() error {
		if _, err := netlink.LinkByName(params.CNI.IfName); err == nil {
			alreadyExists = true
		}
		return nil
	}); err != nil {
		return err
	}

	if !alreadyExists {
		if _, err := cni.Add(ctx, cni.Params{
			Config:      params.CNI.Config,
			NetNS:       params.TargetNS,
			IfName:      params.CNI.IfName,
			ContainerID: cniContainerID(),
			BinDirs:     params.CNI.BinDirs,
			CacheDir:    params.CNI.CacheDir,
			Addresses:   params.CNI.Addresses,
		}); err != nil {
			return err
		}
	}

	if err := netnamespace.In(ns, func() error {
		link, err := netlink.LinkByName(params.CNI.IfName)
		if err != nil {
			return fmt.Errorf("failed to get cni underlay interface %s: %w", params.CNI.IfName, err)
		}
		if link.Attrs().Group != underlayGroupID {
			if err := netlink.LinkSetGroup(link, int(underlayGroupID)); err != nil {
				return fmt.Errorf("failed to set group ID on cni underlay interface %s: %w", params.CNI.IfName, err)
			}
		}
		if err := netlink.LinkSetUp(link); err != nil {
			return fmt.Errorf("could not set cni underlay interface %s up: %w", params.CNI.IfName, err)
		}
		return nil
	}); err != nil {
		return err
	}

	// When the interface already existed we skipped CNI ADD, so a DHCP IPAM is
	// not (re)acquiring its lease. This is the case after a controller or dhcp
	// daemon restart: the interface lives in the persistent router netns and
	// outlives the controller, but the freshly started daemon has no record of
	// the lease and would stop renewing it. Re-establish the lease so the daemon
	// resumes renewal; it is a no-op for non-DHCP IPAM.
	if alreadyExists {
		if err := ensureUnderlayLease(ctx, ns, params); err != nil {
			return err
		}
	}

	return nil
}

// ensureUnderlayLease re-establishes a DHCP IPAM lease for an existing
// CNI-provisioned underlay interface by invoking the IPAM plugin directly (see
// cni.EnsureIPAM). It is a no-op unless the NAD's IPAM is "dhcp". After the
// daemon returns the lease, any address drift on the interface is reconciled.
func ensureUnderlayLease(ctx context.Context, ns netns.NsHandle, params UnderlayParams) error {
	ipamType, err := cni.IPAMType(params.CNI.Config)
	if err != nil {
		return fmt.Errorf("failed to determine underlay ipam type: %w", err)
	}
	if ipamType != "dhcp" {
		return nil
	}
	leased, err := cni.EnsureIPAM(ctx, params.CNI.BinDirs, params.TargetNS, cniContainerID(), params.CNI.IfName, params.CNI.Config)
	if err != nil {
		return fmt.Errorf("failed to re-establish dhcp lease for %s: %w", params.CNI.IfName, err)
	}
	return netnamespace.In(ns, func() error {
		link, err := netlink.LinkByName(params.CNI.IfName)
		if err != nil {
			return fmt.Errorf("failed to get cni underlay interface %s: %w", params.CNI.IfName, err)
		}
		return reconcileInterfaceAddress(link, leased)
	})
}

// reconcileInterfaceAddress ensures want is the address configured on link,
// replacing a drifted address. Drift can only occur if the lease was re-acquired
// so late that the DHCP server already reassigned the old IP; in normal
// operation the re-acquired lease matches what is on the interface and this is a
// no-op.
func reconcileInterfaceAddress(link netlink.Link, want net.IPNet) error {
	family := netlink.FAMILY_V4
	if want.IP.To4() == nil {
		family = netlink.FAMILY_V6
	}
	addrs, err := netlink.AddrList(link, family)
	if err != nil {
		return fmt.Errorf("failed to list addresses on %s: %w", link.Attrs().Name, err)
	}
	wantStr := want.String()
	for i := range addrs {
		if addrs[i].IPNet != nil && addrs[i].IPNet.String() == wantStr {
			return nil // no drift
		}
	}
	slog.Warn("dhcp lease drift on underlay interface, replacing address",
		"interface", link.Attrs().Name, "leased", wantStr)
	if err := netlink.AddrReplace(link, &netlink.Addr{IPNet: &want}); err != nil {
		return fmt.Errorf("failed to set leased address %s on %s: %w", wantStr, link.Attrs().Name, err)
	}
	for i := range addrs {
		a := &addrs[i]
		if a.IPNet == nil || a.IPNet.String() == wantStr || !a.IP.IsGlobalUnicast() {
			continue
		}
		if err := netlink.AddrDel(link, a); err != nil {
			slog.Warn("failed to remove stale underlay address",
				"interface", link.Attrs().Name, "address", a.IPNet.String(), "error", err)
		}
	}
	return nil
}

func ensureLoopback(ctx context.Context, ns netns.NsHandle, vtepIPs ...string) error {
	slog.DebugContext(ctx, "setup underlay", "step", "setting up loopback interface")
	defer slog.DebugContext(ctx, "setup underlay", "step", "loopback interface set up")

	if err := netnamespace.In(ns, func() error {
		loopback, err := netlink.LinkByName(loopbackName)
		if err != nil {
			return fmt.Errorf("ensureLoopback: failed to retrieve %s, err: %w", loopbackName, err)
		}

		for _, vtepIP := range vtepIPs {
			err = assignIPToInterface(loopback, vtepIP)
			if err != nil {
				return err
			}
		}
		// The link is already set up during namespace creation. However, in order to be idempotent, do this here again,
		// in case something external set the link to down.
		if err := netlink.LinkSetUp(loopback); err != nil {
			return fmt.Errorf("ensureLoopback: failed to bring up %s: %w", loopbackName, err)
		}

		return nil
	}); err != nil {
		return err
	}

	return nil
}

// RemoveUnderlay removes the underlay state from the named network namespace:
// it deletes any CNI-provisioned underlay interface (via the libcni result
// cache, since the underlay/NAD config may already be gone), resets the group
// ID on physical underlay NICs (so HasUnderlayInterface returns false on the
// next reconcile), and clears all IP addresses from the VTEP loopback (lo).
func RemoveUnderlay(ctx context.Context, targetNS string, cniBinDirs []string, cniCacheDir string) error {
	// Tear down a CNI-provisioned underlay interface first. The plugin enters
	// the target netns itself via CNI_NETNS, so this runs outside In().
	if cniCacheDir != "" || len(cniBinDirs) > 0 {
		if err := cni.DelCached(ctx, cniBinDirs, cniCacheDir, cniContainerID()); err != nil {
			return fmt.Errorf("RemoveUnderlay: failed to delete cni underlay: %w", err)
		}
	}

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
		if err := clearNonDefaultLoopbackIPs(loopbackName); err != nil {
			return err
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

func clearNonDefaultLoopbackIPs(intf string) error {
	lo, err := netlink.LinkByName(intf)
	if err != nil {
		return fmt.Errorf("failed to find %s: %w", intf, err)
	}

	addresses, err := netlink.AddrList(lo, netlink.FAMILY_ALL)
	if err != nil {
		return fmt.Errorf("failed to list addresses on %s: %w", intf, err)
	}

	var errs []error
	for _, address := range addresses {
		if address.IP.IsLoopback() {
			continue
		}
		if err := netlink.AddrDel(lo, &address); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
