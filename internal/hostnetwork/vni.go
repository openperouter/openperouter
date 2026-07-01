// SPDX-License-Identifier:Apache-2.0

package hostnetwork

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/openperouter/openperouter/internal/netnamespace"
	"github.com/openperouter/openperouter/internal/ovsmodel"
	libovsclient "github.com/ovn-kubernetes/libovsdb/client"
	"github.com/ovn-kubernetes/libovsdb/model"
	"github.com/ovn-kubernetes/libovsdb/ovsdb"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
	"k8s.io/utils/ptr"
)

const (
	// VXLanOverhead is the number of bytes added by VXLan encapsulation.
	VXLanOverhead = 50
	// SRv6Overhead is the number of bytes added by SRv6 encapsulation (outer IPv6 header + SRH)
	// 40 bytes IPv6 header + 24 bytes for the SRH.
	SRv6Overhead = 64

	// MinVethMTU is the minimum MTU we will set on the veth.
	// 1280 is the IPv6 minimum MTU (RFC 8200); the kernel will reject
	// or disable IPv6 on the link below this.
	MinVethMTU = 1280
)

type VNIParams struct {
	VRF       string `json:"vrf"`
	TargetNS  string `json:"targetns"`
	VTEPIP    string `json:"vtepip"`
	VNI       int32  `json:"vni"`
	VXLanPort *int32 `json:"vxlanport,omitempty"`
}

type L3VNIParams struct {
	VNIParams `json:",inline"`
	Name      string `json:"name"`
	HostVeth  *Veth  `json:"veth"`
}

type L3PassthroughParams struct {
	TargetNS string `json:"targetns"`
	HostVeth Veth   `json:"veth"`
}

type Veth struct {
	HostIPv4 string `json:"hostipv4"`
	NSIPv4   string `json:"nsipv4"`
	HostIPv6 string `json:"hostipv6"`
	NSIPv6   string `json:"nsipv6"`
}

type L2VNIParams struct {
	VNIParams    `json:",inline"`
	Name         string      `json:"name"`
	L2GatewayIPs []string    `json:"l2gatewayips"`
	HostMaster   *HostMaster `json:"hostmaster"`
}

type HostMaster struct {
	Name       *string `json:"name,omitempty"`
	Type       string  `json:"type,omitempty"`
	AutoCreate *bool   `json:"autocreate,omitempty"`
}

type L3VPNParams struct {
	Name                string `json:"name"`
	HostVeth            *Veth  `json:"veth"`
	VRF                 string `json:"vrf"`
	TargetNS            string `json:"targetns"`
	InterfaceIdentifier int32  `json:"vni"`
}

const (
	VRFLinkType       = "vrf"
	BridgeLinkType    = "linux-bridge"
	VXLanLinkType     = "vxlan"
	OVSBridgeLinkType = "ovs-bridge"
)

type NotRouterInterfaceError struct {
	Name string
}

func (e NotRouterInterfaceError) Error() string {
	return fmt.Sprintf("interface %s is not a router interface", e.Name)
}

// SetupL3VNI sets up a Layer 3 VNI in the target namespace.
// It uses setupVNI to create the necessary VRF, bridge, and
// VXLan interface, and moves the veth to the VRF corresponding
// to the L3 routing domain, exposing it to the default host namespace.
func SetupL3VNI(ctx context.Context, params L3VNIParams) error {
	if err := setupVNI(ctx, params.VNIParams, setAddrGenModeNone); err != nil {
		return fmt.Errorf("SetupL3VNI: failed to setup VNI: %w", err)
	}
	slog.DebugContext(ctx, "setting up l3 VNI", "params", params)
	defer slog.DebugContext(ctx, "end setting up l3 VNI", "params", params)

	if params.HostVeth == nil {
		slog.DebugContext(ctx, "no host veth configured, skipping setup")
		return nil
	}

	if err := setupHostVeth(
		ctx,
		params.VNI,
		params.TargetNS,
		params.HostVeth,
		params.VRF,
		VXLanOverhead); err != nil {
		return fmt.Errorf("SetupL3VNI: failed to setup host veth pair: %w", err)
	}
	return nil
}

// SetupL3VPN sets up a Layer 3 VPN in the target namespace.
// It uses setupL3VPN to create the necessary VRF, and moves the veth to the
// VRF corresponding to the L3 routing domain, exposing it to the default host
// namespace.
func SetupL3VPN(ctx context.Context, params L3VPNParams) error {
	slog.DebugContext(ctx, "setting up L3VPN", "params", params)
	defer slog.DebugContext(ctx, "end setting up L3VPN", "params", params)

	if err := setupL3VPN(ctx, params); err != nil {
		return fmt.Errorf("SetupL3VPN: failed to setup VNI: %w", err)
	}

	if params.HostVeth == nil {
		slog.DebugContext(ctx, "no host veth configured, skipping setup")
		return nil
	}

	if err := setupHostVeth(
		ctx,
		params.InterfaceIdentifier,
		params.TargetNS,
		params.HostVeth,
		params.VRF,
		SRv6Overhead); err != nil {
		return fmt.Errorf("SetupL3VPN: failed to setup host veth pair: %w", err)
	}
	return nil
}

// setupHostVeth configures the veth pair that connects the host to the perouter namespace, for
// L3VNI and L3VPN.
func setupHostVeth(ctx context.Context, interfaceIdentifier int32, targetNS string, hostVeth *Veth,
	vrfName string, tunnelOverhead int) error {
	vethNames := vethNamesFromIdentifier(interfaceIdentifier)
	if err := setupNamespacedVeth(ctx, vethNames, targetNS); err != nil {
		return fmt.Errorf("failed to setup VNI veth: %w", err)
	}

	ns, err := netns.GetFromPath(targetNS)
	if err != nil {
		return fmt.Errorf("failed to get network namespace %s: %w", targetNS, err)
	}
	defer func() {
		if err := ns.Close(); err != nil {
			slog.Error("failed to close namespace", "namespace", targetNS, "error", err)
		}
	}()

	hostVethLink, err := netlink.LinkByName(vethNames.HostSide)
	if errors.As(err, &netlink.LinkNotFoundError{}) {
		return fmt.Errorf("host veth %s does not exist, cannot setup", vethNames.HostSide)
	}
	if err != nil {
		return fmt.Errorf("failed to get host veth %s: %w", vethNames.HostSide, err)
	}

	err = assignIPsToInterface(hostVethLink, hostVeth.HostIPv4, hostVeth.HostIPv6)
	if err != nil {
		return fmt.Errorf("failed to assign IPs to host veth: %w", err)
	}

	underlayMTU, err := findUnderlayMTU(ns)
	if err != nil {
		return fmt.Errorf("could not find underlay MTU: %w", err)
	}

	if err := setVethMTUForTunnelOverhead(hostVethLink, underlayMTU, tunnelOverhead); err != nil {
		return fmt.Errorf("failed to set MTU on host veth %s: %w", vethNames.HostSide, err)
	}

	if err := netnamespace.In(ns, func() error {
		peVethLink, err := netlink.LinkByName(vethNames.NamespaceSide)
		if err != nil {
			return fmt.Errorf("could not find peer veth %s in namespace %s: %w", vethNames.NamespaceSide, targetNS, err)
		}

		if err := setVethMTUForTunnelOverhead(peVethLink, underlayMTU, tunnelOverhead); err != nil {
			return fmt.Errorf("failed to set MTU on pe veth %s: %w", vethNames.NamespaceSide, err)
		}

		vrfLink, err := netlink.LinkByName(vrfName)
		if err != nil {
			return fmt.Errorf("could not find vrf %s in namespace %s: %w", vrfName, targetNS, err)
		}

		err = linkSetMaster(peVethLink, vrfLink)
		if err != nil {
			return fmt.Errorf("failed to set vrf %s as master of pe veth %s: %w", vrfName, peVethLink.Attrs().Name, err)
		}
		// Note: since the ipv6 address is removed after enslaving the veth to the vrf, this has to
		// be performed after the veth is enslaved to the vrf.
		err = assignIPsToInterface(peVethLink, hostVeth.NSIPv4, hostVeth.NSIPv6)
		if err != nil {
			return fmt.Errorf("failed to assign IPs to PE veth: %w", err)
		}
		return nil
	}); err != nil {
		return err
	}

	return nil
}

// SetupL2VNI sets up a Layer 2 VNI in the target namespace.
// It uses setupVNI to create the necessary VRF, bridge, and
// VXLan interface, and enslaves the veth leg to the bridge,
// exposing the L2 domain to the default host namespace.
func SetupL2VNI(ctx context.Context, params L2VNIParams) error {
	if err := setupVNI(ctx, params.VNIParams); err != nil {
		return fmt.Errorf("SetupL2VNI: failed to setup VNI: %w", err)
	}
	vethNames := vethNamesFromIdentifier(params.VNI)
	if err := setupNamespacedVeth(ctx, vethNames, params.TargetNS); err != nil {
		return fmt.Errorf("SetupL2VNI: failed to setup VNI veth: %w", err)
	}

	slog.DebugContext(ctx, "setting up l2 VNI", "params", params)
	defer slog.DebugContext(ctx, "end setting up l2 VNI", "params", params)

	ns, err := netns.GetFromPath(params.TargetNS)
	if err != nil {
		return fmt.Errorf("SetupVNI: Failed to get network namespace %s: %w", params.TargetNS, err)
	}
	defer func() {
		if err := ns.Close(); err != nil {
			slog.Error("failed to close namespace", "namespace", params.TargetNS, "error", err)
		}
	}()

	hostVeth, err := netlink.LinkByName(vethNames.HostSide)
	if errors.As(err, &netlink.LinkNotFoundError{}) {
		return fmt.Errorf("SetupL2VNI: host veth %s does not exist, cannot setup L2 VNI", vethNames.HostSide)
	}
	if err != nil {
		return fmt.Errorf("SetupL2VNI: failed to get host veth %s: %w", vethNames.HostSide, err)
	}
	slog.Info("SetupL2VNI: found host veth", "name", vethNames.HostSide, "index", hostVeth.Attrs().Index)

	underlayMTU, err := findUnderlayMTU(ns)
	if err != nil {
		return fmt.Errorf("could not find underlay MTU: %w", err)
	}

	if err := setVethMTUForTunnelOverhead(hostVeth, underlayMTU, VXLanOverhead); err != nil {
		return fmt.Errorf("SetupL2VNI: failed to set MTU on host veth %s: %w", vethNames.HostSide, err)
	}

	if params.HostMaster != nil {
		if err := setupHostMaster(ctx, params, hostVeth); err != nil {
			return err
		}
	}

	if err := netnamespace.In(ns, func() error {
		return setupL2VNIRouterSide(params, vethNames.NamespaceSide, underlayMTU)
	}); err != nil {
		return err
	}

	return nil
}

func setupL2VNIRouterSide(params L2VNIParams, vethName string, underlayMTU int) error {
	peVeth, err := netlink.LinkByName(vethName)
	if err != nil {
		return fmt.Errorf("could not find peer veth %s in namespace %s: %w", vethName, params.TargetNS, err)
	}

	if err := setVethMTUForTunnelOverhead(peVeth, underlayMTU, VXLanOverhead); err != nil {
		return fmt.Errorf("failed to set MTU on pe veth %s: %w", vethName, err)
	}

	name := BridgeName(params.VNI)
	bridge, err := netlink.LinkByName(name)
	if err != nil {
		return fmt.Errorf("could not find bridge %s in namespace %s: %w", name, params.TargetNS, err)
	}
	if err := linkSetMaster(peVeth, bridge); err != nil {
		return fmt.Errorf("failed to set bridge %s as master of pe veth %s: %w", name, peVeth.Attrs().Name, err)
	}
	if len(params.L2GatewayIPs) > 0 {
		for _, ip := range params.L2GatewayIPs {
			if err := assignIPToInterface(bridge, ip); err != nil {
				return fmt.Errorf("failed to assign L2 gateway IP %s to bridge %s: %w", ip, name, err)
			}
		}

		// setting up the same mac address for all the nodes for distributed gateway
		if err := ensureBridgeFixedMacAddress(bridge, params.VNI); err != nil {
			return fmt.Errorf("failed to set bridge mac address %s: %v", name, err)
		}
	}
	return nil
}

func setupHostMaster(ctx context.Context, params L2VNIParams, hostVeth netlink.Link) error {
	bridgeConfig := *params.HostMaster
	switch bridgeConfig.Type {
	case OVSBridgeLinkType:
		lowerDeviceName := ptr.Deref(bridgeConfig.Name, "")
		if ptr.Deref(bridgeConfig.AutoCreate, false) {
			lowerDeviceName = hostBridgeName(params.VNI)
		}
		if err := ensureOVSBridgeAndAttach(ctx, lowerDeviceName, hostVeth.Attrs().Name); err != nil {
			return fmt.Errorf("failed to ensure OVS bridge %s and attach %s: %w", lowerDeviceName, hostVeth.Attrs().Name, err)
		}
	case BridgeLinkType:
		master, err := hostMaster(params.VNI, bridgeConfig)
		if err != nil {
			return fmt.Errorf("SetupL2VNI: failed to get host master for VRF %s: %w", params.VRF, err)
		}
		if err := linkSetMaster(hostVeth, master); err != nil {
			return fmt.Errorf("failed to set host master %s as master of host veth %s: %w", master.Attrs().Name, hostVeth.Attrs().Name, err)
		}
	default:
		return fmt.Errorf("provided hostmaster.Type %q is not supported", bridgeConfig.Type)
	}
	return nil
}

// setupVNI sets up the configuration required by FRR to
// serve a given VNI in the target namespace. This includes:
// - a linux VRF (only when params.VRF is non-empty)
// - a linux Bridge, enslaved to the VRF when one exists
// - a VXLan interface
func setupVNI(ctx context.Context, params VNIParams, options ...NetlinkOption) error {
	slog.DebugContext(ctx, "setting up VNI", "params", params)
	defer slog.DebugContext(ctx, "end setting up VNI", "params", params)
	ns, err := netns.GetFromPath(params.TargetNS)
	if err != nil {
		return fmt.Errorf("failed to get network namespace %s: %w", params.TargetNS, err)
	}
	defer func() {
		if err := ns.Close(); err != nil {
			slog.Error("failed to close namespace", "namespace", params.TargetNS, "error", err)
		}
	}()

	return netnamespace.In(ns, func() error {
		var vrf *netlink.Vrf
		if params.VRF != "" {
			slog.DebugContext(ctx, "setting up vrf", "vrf", params.VRF)
			var err error
			vrf, err = setupVRF(params.VRF)
			if err != nil {
				return err
			}
		}

		slog.DebugContext(ctx, "setting up bridge")
		bridge, err := setupBridge(params, vrf, options...)
		if err != nil {
			return err
		}

		slog.DebugContext(ctx, "setting up vxlan")
		return setupVXLan(params, bridge)
	})
}

// setupL3VPN sets up the configuration required by FRR to
// serve a given L3VPN in the target namespace. This includes:
// - a linux VRF
// - sysctl strict mode and disable RP filter (for SRv6)
func setupL3VPN(ctx context.Context, params L3VPNParams) error {
	ns, err := netns.GetFromPath(params.TargetNS)
	if err != nil {
		return fmt.Errorf("failed to get network namespace %s: %w", params.TargetNS, err)
	}
	defer func() {
		if err := ns.Close(); err != nil {
			slog.Error("failed to close namespace", "namespace", params.TargetNS, "error", err)
		}
	}()

	return netnamespace.In(ns, func() error {
		slog.DebugContext(ctx, "setting up vrf", "vrf", params.VRF)
		_, err := setupVRF(params.VRF, withSRv6())
		return err
	})
}

// RemoveAllNonConfiguredObjects removes from the target namespace all bridges / VRFs / veths / etc.
func RemoveAllNonConfiguredObjects(targetNS string) error {
	return RemoveNonConfiguredObjects(targetNS, map[string]bool{}, map[int32]bool{})
}

// RemoveNonConfiguredObjects removes from the target namespace the
// leftovers corresponding to VRFs and interface identifiers that are not configured anymore.
func RemoveNonConfiguredObjects(targetNS string, vrfs map[string]bool, interfaceIdentifiers map[int32]bool) error {
	failedDeletes := removeHostSideInterfaces(interfaceIdentifiers)

	ns, err := netns.GetFromPath(targetNS)
	if err != nil {
		return fmt.Errorf("RemoveNonConfiguredObjects: Failed to get network namespace %s: %w", targetNS, err)
	}
	defer func() {
		if err := ns.Close(); err != nil {
			slog.Error("failed to close namespace", "namespace", targetNS, "error", err)
		}
	}()

	if err := netnamespace.In(ns, func() error {
		nsErrors := removeNamespaceSideInterfaces(interfaceIdentifiers, vrfs)
		failedDeletes = append(failedDeletes, nsErrors...)
		return errors.Join(failedDeletes...)
	}); err != nil {
		return err
	}

	return errors.Join(failedDeletes...)
}

func removeHostSideInterfaces(interfaceIdentifiers map[int32]bool) []error {
	var failedDeletes []error

	hostLinks, err := netlink.LinkList()
	if err != nil {
		return []error{fmt.Errorf("remove non configured interfaces: failed to list links: %w", err)}
	}
	if err := deleteLinksForType(BridgeLinkType, interfaceIdentifiers, hostLinks, interfaceIDFromHostBridgeName); err != nil {
		failedDeletes = append(failedDeletes, fmt.Errorf("remove bridge links: %w", err))
	}
	if err := removeOVSBridgesForInterfaceIDs(context.Background(), interfaceIdentifiers); err != nil {
		failedDeletes = append(failedDeletes, fmt.Errorf("remove OVS bridges: %w", err))
	}

	for _, hl := range hostLinks {
		if hl.Type() != VethLinkType {
			continue
		}
		if !strings.HasPrefix(hl.Attrs().Name, HostVethPrefix) {
			continue
		}
		interfaceIdentifier, err := interfaceIdentifierFromHostVeth(hl.Attrs().Name)
		if err != nil {
			failedDeletes = append(failedDeletes, fmt.Errorf("remove host leg: %s %w", hl.Attrs().Name, err))
			continue
		}
		if interfaceIdentifiers[interfaceIdentifier] {
			continue
		}
		if err := netlink.LinkDel(hl); err != nil {
			failedDeletes = append(failedDeletes, fmt.Errorf("remove host leg: %s %w", hl.Attrs().Name, err))
		}
	}
	return failedDeletes
}

func removeNamespaceSideInterfaces(interfaceIdentifier map[int32]bool, vrfs map[string]bool) []error {
	var failedDeletes []error

	links, err := netlink.LinkList()
	if err != nil {
		return []error{fmt.Errorf("remove non configured interfaces: failed to list links: %w", err)}
	}
	if err := deleteLinksForType(VXLanLinkType, interfaceIdentifier, links, interfaceIDFromVXLanName); err != nil {
		failedDeletes = append(failedDeletes, fmt.Errorf("remove vlan links: %w", err))
	}
	if err := deleteLinksForType(BridgeLinkType, interfaceIdentifier, links, interfaceIDFromBridgeName); err != nil {
		failedDeletes = append(failedDeletes, fmt.Errorf("remove bridge links: %w", err))
	}

	for _, l := range links {
		if l.Type() != VRFLinkType {
			continue
		}
		if vrfs[l.Attrs().Name] {
			continue
		}
		if err := netlink.LinkDel(l); err != nil {
			failedDeletes = append(failedDeletes, fmt.Errorf("remove non configured interfaces: failed to delete vrf %s %w", l.Attrs().Name, err))
		}
	}
	return failedDeletes
}

// deleteLinks deletes all the links of the given type that do not correspond to
// any interface identifier.
func deleteLinksForType(linkType string, interfaceIdentifiers map[int32]bool, links []netlink.Link,
	interfaceIDFromName func(string) (int32, error)) error {
	deleteErrors := []error{}
	for _, l := range links {
		if l.Type() != netlinkTypeFor(linkType) {
			continue
		}
		interfaceID, err := interfaceIDFromName(l.Attrs().Name)
		if errors.As(err, &NotRouterInterfaceError{}) {
			// not a router interface, skip
			continue
		}
		if err != nil {
			deleteErrors = append(deleteErrors, fmt.Errorf("remove non configured interfaces: failed to get "+
				"interface ID for %s %w", linkType, err))
			continue
		}
		if _, ok := interfaceIdentifiers[interfaceID]; ok {
			continue
		}
		if err := netlink.LinkDel(l); err != nil {
			deleteErrors = append(deleteErrors, fmt.Errorf("remove non configured interfaces: failed to "+
				"delete %s %s %w", linkType, l.Attrs().Name, err))
			continue
		}
	}

	return errors.Join(deleteErrors...)
}

// netlinkTypeFor maps API link type names to netlink link type names.
func netlinkTypeFor(linkType string) string {
	if linkType == BridgeLinkType {
		return "bridge"
	}
	return linkType
}

// removeOVSBridgesForInterfaceIDs removes auto-created OVS bridges that are not in the configured interface ID list.
// Only deletes bridges with external_id "created-by: openperouter" (auto-created bridges).
func removeOVSBridgesForInterfaceIDs(ctx context.Context, interfaceIDs map[int32]bool) error {
	ovs, err := NewOVSClient(ctx)
	if err != nil {
		// OVS not available, skip cleanup gracefully
		slog.Debug("OVS client not available, skipping OVS bridge cleanup", "error", err)
		return nil
	}
	defer ovs.Close()

	if _, err := ovs.Monitor(ctx, ovs.NewMonitor(
		libovsclient.WithTable(&ovsmodel.OpenvSwitch{}),
		libovsclient.WithTable(&ovsmodel.Bridge{}),
		libovsclient.WithTable(&ovsmodel.Port{}),
		libovsclient.WithTable(&ovsmodel.Interface{}),
	)); err != nil {
		return fmt.Errorf("failed to setup OVS monitor: %w", err)
	}

	var bridges []ovsmodel.Bridge
	if err := ovs.List(ctx, &bridges); err != nil {
		return fmt.Errorf("failed to list OVS bridges: %w", err)
	}

	deleteErrors := []error{}
	var ops []ovsdb.Operation
	for _, bridge := range bridges {
		slog.Debug("checking OVS bridge for cleanup", "name", bridge.Name, "uuid", bridge.UUID, "external_ids", bridge.ExternalIDs)

		createdBy, hasMarker := bridge.ExternalIDs["created-by"]
		managedByUs := hasMarker && createdBy == "openperouter"

		if !managedByUs {
			// For non-managed bridges, only detach our veth ports for interfaces with removed IDs.
			// interfaceIDs is the set of IDs we want to keep; ports for any other ID are removed.
			if err := detachOurPortsFromBridge(ctx, ovs, bridge, interfaceIDs); err != nil {
				deleteErrors = append(deleteErrors, err)
			}
			continue
		}

		interfaceIdentifier, err := interfaceIDFromHostBridgeName(bridge.Name)
		if errors.As(err, &NotRouterInterfaceError{}) {
			slog.Debug("skipping bridge - not our naming pattern", "name", bridge.Name)
			continue // Not our bridge naming pattern
		}
		if err != nil {
			deleteErrors = append(deleteErrors, fmt.Errorf("failed to parse interface ID from bridge %s: %w",
				bridge.Name, err))
			continue
		}

		if interfaceIDs[interfaceIdentifier] {
			slog.Debug("keeping bridge - interface ID is configured", "name", bridge.Name,
				"interfaceIdentifier", interfaceIdentifier)
			continue // Bridge should be kept
		}

		slog.Info("deleting auto-created OVS bridge", "name", bridge.Name,
			"interfaceIdentifier", interfaceIdentifier, "uuid", bridge.UUID)

		// Remove the bridge from the OpenVSwitch table's bridges array
		ovsRow := &ovsmodel.OpenvSwitch{}
		removeFromOVSOp, err := ovs.WhereCache(func(*ovsmodel.OpenvSwitch) bool { return true }).
			Mutate(ovsRow, model.Mutation{
				Field:   &ovsRow.Bridges,
				Mutator: ovsdb.MutateOperationDelete,
				Value:   []string{bridge.UUID},
			})
		if err != nil {
			deleteErrors = append(deleteErrors, fmt.Errorf("failed to create OVS table mutation for bridge %s: %w", bridge.Name, err))
			continue
		}
		ops = append(ops, removeFromOVSOp...)

		// Finally, delete the bridge
		bridgeToDelete := &ovsmodel.Bridge{UUID: bridge.UUID}
		deleteOps, err := ovs.Where(bridgeToDelete).Delete()
		if err != nil {
			deleteErrors = append(deleteErrors, fmt.Errorf("failed to create delete op for bridge %s: %w", bridge.Name, err))
			continue
		}
		ops = append(ops, deleteOps...)
	}

	if len(ops) == 0 {
		return errors.Join(deleteErrors...)
	}

	txResult, err := ovs.Transact(ctx, ops...)
	if err != nil {
		deleteErrors = append(deleteErrors, fmt.Errorf("failed to delete OVS bridges: %w", err))
		slog.Error("failed to batch-delete OVS bridges", "error", err)
		return errors.Join(deleteErrors...)
	}
	if _, err := ovsdb.CheckOperationResults(txResult, ops); err != nil {
		deleteErrors = append(deleteErrors, fmt.Errorf("OVS bridge deletion operations failed: %w", err))
		slog.Error("OVS bridge deletion operations failed", "error", err)
		return errors.Join(deleteErrors...)
	}
	slog.Info("successfully deleted auto-created OVS bridges")

	return errors.Join(deleteErrors...)
}

// removePortsFromBridge generates OVSDB operations to detach and delete ports
// from an OVS bridge. If filter is nil, all ports are removed. If filter is
// provided, each port is fetched and only ports for which filter returns true
// are included. Ports that cannot be fetched are silently skipped.
func removePortsFromBridge(ctx context.Context, ovs libovsclient.Client, bridge ovsmodel.Bridge, filter func(*ovsmodel.Port) bool) ([]ovsdb.Operation, error) {
	var matchingUUIDs []string
	for _, portUUID := range bridge.Ports {
		if filter == nil {
			matchingUUIDs = append(matchingUUIDs, portUUID)
			continue
		}
		port := &ovsmodel.Port{UUID: portUUID}
		if err := ovs.Get(ctx, port); err != nil {
			if errors.Is(err, libovsclient.ErrNotFound) {
				continue
			}
			return nil, fmt.Errorf("failed to get port %s from bridge %s: %w", portUUID, bridge.Name, err)
		}
		if filter(port) {
			matchingUUIDs = append(matchingUUIDs, portUUID)
		}
	}

	if len(matchingUUIDs) == 0 {
		return nil, nil
	}

	var ops []ovsdb.Operation
	bridgeToMutate := &ovsmodel.Bridge{UUID: bridge.UUID}
	mutateOp, err := ovs.Where(bridgeToMutate).Mutate(bridgeToMutate, model.Mutation{
		Field:   &bridgeToMutate.Ports,
		Mutator: ovsdb.MutateOperationDelete,
		Value:   matchingUUIDs,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create port removal op for bridge %s: %w", bridge.Name, err)
	}
	ops = append(ops, mutateOp...)

	for _, portUUID := range matchingUUIDs {
		port := &ovsmodel.Port{UUID: portUUID}
		deleteOp, err := ovs.Where(port).Delete()
		if err != nil {
			return nil, fmt.Errorf("failed to create delete op for port %s: %w", portUUID, err)
		}
		ops = append(ops, deleteOp...)
	}

	return ops, nil
}

// detachOurPortsFromBridge removes openperouter-managed veth ports from a
// non-managed OVS bridge matching identifiers that are no longer configured.
func detachOurPortsFromBridge(ctx context.Context, ovs libovsclient.Client, bridge ovsmodel.Bridge,
	configuredInterfacesIDs map[int32]bool) error {
	filter := func(port *ovsmodel.Port) bool {
		if !strings.HasPrefix(port.Name, HostVethPrefix) {
			return false
		}
		interfaceIdentifier, err := interfaceIdentifierFromHostVeth(port.Name)
		if err != nil {
			return false
		}
		return !configuredInterfacesIDs[interfaceIdentifier]
	}

	ops, err := removePortsFromBridge(ctx, ovs, bridge, filter)
	if err != nil {
		return err
	}
	if len(ops) == 0 {
		return nil
	}
	slog.Info("detaching veth ports from non-managed OVS bridge", "bridge", bridge.Name)
	if _, err := ovs.Transact(ctx, ops...); err != nil {
		return fmt.Errorf("failed to detach ports from bridge %s: %w", bridge.Name, err)
	}
	return nil
}

func hostMaster(vni int32, m HostMaster) (netlink.Link, error) {
	if !ptr.Deref(m.AutoCreate, false) {
		name := ptr.Deref(m.Name, "")
		hostMaster, err := netlink.LinkByName(name)
		if err != nil {
			return nil, fmt.Errorf("could not find host master %s: %w", name, err)
		}
		return hostMaster, nil
	}
	bridge, err := createHostBridge(vni)
	if err != nil {
		return nil, fmt.Errorf("getHostMaster: failed to create host bridge %d: %w", vni, err)
	}
	return bridge, nil
}

// setVethMTUForTunnelOverhead sets the MTU on a veth interface to account for tunnel overhead.
// If the underlay MTU is not found, or if the resulting MTU would be too small,
// the MTU is left unchanged.
func setVethMTUForTunnelOverhead(link netlink.Link, underlayMTU int, overhead int) error {
	if underlayMTU == 0 {
		slog.Debug("No underlay MTU found, leaving veth MTU at default", "veth", link.Attrs().Name)
		return nil
	}
	targetMTU := underlayMTU - overhead
	if targetMTU <= MinVethMTU {
		slog.Warn("Calculated veth MTU is too low, leaving at default",
			"veth", link.Attrs().Name,
			"underlayMTU", underlayMTU,
			"calculatedMTU", targetMTU)
		return nil
	}
	return linkSetMTU(link, targetMTU)
}

// assignIPsToInterface assigns both IPv4 and IPv6 addresses to an interface.
// It fails if no IPs are provided (both IPv4 and IPv6 are empty).
func assignIPsToInterface(link netlink.Link, ipv4, ipv6 string) error {
	if ipv4 == "" && ipv6 == "" {
		return fmt.Errorf("at least one IP address must be provided (IPv4 or IPv6)")
	}

	if ipv4 != "" {
		if err := assignIPToInterface(link, ipv4); err != nil {
			return fmt.Errorf("failed to assign IPv4 address %s: %w", ipv4, err)
		}
	}

	if ipv6 != "" {
		if err := assignIPToInterface(link, ipv6); err != nil {
			return fmt.Errorf("failed to assign IPv6 address %s: %w", ipv6, err)
		}
	}

	return nil
}
