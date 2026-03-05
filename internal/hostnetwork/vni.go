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
)

type VNIParams struct {
	VRF           string `json:"vrf"`
	TargetNS      string `json:"targetns"`
	VTEPIP        string `json:"vtepip"`
	VTEPInterface string `json:"vtepiface"`
	VNI           int    `json:"vni"`
	VXLanPort     int    `json:"vxlanport"`
}

type L3VNIParams struct {
	VNIParams `json:",inline"`
	HostVeth  *Veth `json:"veth"`
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
	L2GatewayIPs []string    `json:"l2gatewayips"`
	HostMaster   *HostMaster `json:"hostmaster"`
}

type HostMaster struct {
	Name       string `json:"name,omitempty"`
	Type       string `json:"type,omitempty"`
	AutoCreate bool   `json:"autocreate,omitempty"`
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
	if err := setupVNI(ctx, params.VNIParams); err != nil {
		return fmt.Errorf("SetupL3VNI: failed to setup VNI: %w", err)
	}
	slog.DebugContext(ctx, "setting up l3 VNI", "params", params)
	defer slog.DebugContext(ctx, "end setting up l3 VNI", "params", params)

	if params.HostVeth == nil {
		slog.DebugContext(ctx, "no host veth configured, skipping setup")
		return nil
	}
	vethNames := vethNamesFromVNI(params.VNI)
	if err := setupNamespacedVeth(ctx, vethNames, params.TargetNS); err != nil {
		return fmt.Errorf("SetupL3VNI: failed to setup VNI veth: %w", err)
	}

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
		return fmt.Errorf("SetupL3VNI: host veth %s does not exist, cannot setup L3 VNI", vethNames.HostSide)
	}

	err = assignIPsToInterface(hostVeth, params.HostVeth.HostIPv4, params.HostVeth.HostIPv6)
	if err != nil {
		return fmt.Errorf("failed to assign IPs to host veth: %w", err)
	}

	if err := netnamespace.In(ns, func() error {
		peVeth, err := netlink.LinkByName(vethNames.NamespaceSide)
		if err != nil {
			return fmt.Errorf("could not find peer veth %s in namespace %s: %w", vethNames.NamespaceSide, params.TargetNS, err)
		}

		vrf, err := netlink.LinkByName(params.VRF)
		if err != nil {
			return fmt.Errorf("could not find vrf %s in namespace %s: %w", params.VRF, params.TargetNS, err)
		}

		err = linkSetMaster(peVeth, vrf)
		if err != nil {
			return fmt.Errorf("failed to set vrf %s as master of pe veth %s: %w", params.VRF, peVeth.Attrs().Name, err)
		}
		// Note: since the ipv6 address is removed after enslaving the veth to the vrf, this has to
		// be performed after the veth is enslaved to the vrf.
		err = assignIPsToInterface(peVeth, params.HostVeth.NSIPv4, params.HostVeth.NSIPv6)
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
	vethNames := vethNamesFromVNI(params.VNI)
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

	if params.HostMaster != nil {
		bridgeConfig := *params.HostMaster
		switch bridgeConfig.Type {
		case OVSBridgeLinkType:
			lowerDeviceName := bridgeConfig.Name
			if bridgeConfig.AutoCreate {
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
	}

	if err := netnamespace.In(ns, func() error {
		peVeth, err := netlink.LinkByName(vethNames.NamespaceSide)
		if err != nil {
			return fmt.Errorf("could not find peer veth %s in namespace %s: %w", vethNames.NamespaceSide, params.TargetNS, err)
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
	}); err != nil {
		return err
	}
	return nil
}

// setupVNI sets up the configuration required by FRR to
// serve a given VNI in the target namespace. This includes:
// - a linux VRF
// - a linux Bridge enslaved to the given VRF
// - a VXLan interface enslaved to the given VRF
//
// Additionally, it creates a veth pair and moves one leg in the target
// namespace.
func setupVNI(ctx context.Context, params VNIParams) error {
	slog.DebugContext(ctx, "setting up VNI", "params", params)
	defer slog.DebugContext(ctx, "end setting up VNI", "params", params)
	ns, err := netns.GetFromPath(params.TargetNS)
	if err != nil {
		return fmt.Errorf("SetupVNI: Failed to get network namespace %s: %w", params.TargetNS, err)
	}
	defer func() {
		if err := ns.Close(); err != nil {
			slog.Error("failed to close namespace", "namespace", params.TargetNS, "error", err)
		}
	}()

	if err := netnamespace.In(ns, func() error {

		slog.DebugContext(ctx, "setting up vrf", "vrf", params.VRF)
		vrf, err := setupVRF(params.VRF)
		if err != nil {
			return err
		}

		slog.DebugContext(ctx, "setting up bridge")
		bridge, err := setupBridge(params, vrf)
		if err != nil {
			return err
		}

		slog.DebugContext(ctx, "setting up vxlan")
		err = setupVXLan(params, bridge)
		if err != nil {
			return err
		}

		return nil
	}); err != nil {
		return err
	}

	return nil
}

// RemoveAllVNIs removes from the target namespace the bridges / VRFs / veths
// for all VNIs.
func RemoveAllVNIs(targetNS string) error {
	return RemoveNonConfiguredVNIs(targetNS, []VNIParams{})
}

// RemoveNonConfiguredVNIs removes from the target namespace the
// leftovers corresponding to VNIs that are not configured anymore.
func RemoveNonConfiguredVNIs(targetNS string, params []VNIParams) error {
	vrfs := map[string]bool{}
	vnis := map[int]bool{}
	for _, p := range params {
		vrfs[p.VRF] = true
		vnis[p.VNI] = true
	}
	hostLinks, err := netlink.LinkList()
	if err != nil {
		return fmt.Errorf("remove non configured vnis: failed to list links: %w", err)
	}
	failedDeletes := []error{}
	if err := deleteLinksForType(BridgeLinkType, vnis, hostLinks, vniFromHostBridgeName); err != nil {
		failedDeletes = append(failedDeletes, fmt.Errorf("remove bridge links: %w", err))
	}

	if err := removeOVSBridgesForVNIs(context.Background(), vnis); err != nil {
		failedDeletes = append(failedDeletes, fmt.Errorf("remove OVS bridges: %w", err))
	}

	for _, hl := range hostLinks {
		if hl.Type() != VethLinkType {
			continue
		}
		if !strings.HasPrefix(hl.Attrs().Name, HostVethPrefix) {
			continue
		}
		vni, err := vniFromHostVeth(hl.Attrs().Name)
		if err != nil {
			failedDeletes = append(failedDeletes, fmt.Errorf("remove host leg: %s %w", hl.Attrs().Name, err))
			continue
		}
		if vnis[vni] {
			continue
		}
		if err := netlink.LinkDel(hl); err != nil {
			failedDeletes = append(failedDeletes, fmt.Errorf("remove host leg: %s %w", hl.Attrs().Name, err))
		}
	}

	ns, err := netns.GetFromPath(targetNS)
	if err != nil {
		return fmt.Errorf("RemoveNonConfiguredVNIs: Failed to get network namespace %s: %w", targetNS, err)
	}
	defer func() {
		if err := ns.Close(); err != nil {
			slog.Error("failed to close namespace", "namespace", targetNS, "error", err)
		}
	}()

	if err := netnamespace.In(ns, func() error {
		links, err := netlink.LinkList()
		if err != nil {
			return fmt.Errorf("remove non configured vnis: failed to list links: %w", err)
		}

		if err := deleteLinksForType(VXLanLinkType, vnis, links, vniFromVXLanName); err != nil {
			failedDeletes = append(failedDeletes, fmt.Errorf("remove vlan links: %w", err))
			return err
		}
		if err := deleteLinksForType(BridgeLinkType, vnis, links, vniFromBridgeName); err != nil {
			failedDeletes = append(failedDeletes, fmt.Errorf("remove bridge links: %w", err))
			return err
		}

		for _, l := range links {
			if l.Type() != VRFLinkType {
				continue
			}
			if vrfs[l.Attrs().Name] {
				continue
			}
			if err := netlink.LinkDel(l); err != nil {
				failedDeletes = append(failedDeletes, fmt.Errorf("remove non configured vnis: failed to delete vrf %s %w", l.Attrs().Name, err))
			}
		}
		return errors.Join(failedDeletes...)
	}); err != nil {
		return err
	}

	return errors.Join(failedDeletes...)
}

// deleteLinks deletes all the links of the given type that do not correspond to
// any VNI.
func deleteLinksForType(linkType string, vnis map[int]bool, links []netlink.Link, vniFromName func(string) (int, error)) error {
	deleteErrors := []error{}
	for _, l := range links {
		if l.Type() != netlinkTypeFor(linkType) {
			continue
		}
		vni, err := vniFromName(l.Attrs().Name)
		if errors.As(err, &NotRouterInterfaceError{}) {
			// not a router interface, skip
			continue
		}
		if err != nil {
			deleteErrors = append(deleteErrors, fmt.Errorf("remove non configured vnis: failed to get vni for %s %w", linkType, err))
			continue
		}
		if _, ok := vnis[vni]; ok {
			continue
		}
		if err := netlink.LinkDel(l); err != nil {
			deleteErrors = append(deleteErrors, fmt.Errorf("remove non configured vnis: failed to delete %s %s %w", linkType, l.Attrs().Name, err))
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

// removeOVSBridgesForVNIs removes auto-created OVS bridges that are not in the configured VNIs list.
// Only deletes bridges with external_id "created-by: openperouter" (auto-created bridges).
func removeOVSBridgesForVNIs(ctx context.Context, vnis map[int]bool) error {
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
			// For non-managed bridges, only detach our veth ports for removed VNIs.
			// vnis is the set of VNIs we want to keep; ports for any other VNI are removed.
			if err := detachOurPortsFromBridge(ctx, ovs, bridge, vnis); err != nil {
				deleteErrors = append(deleteErrors, err)
			}
			continue
		}

		vni, err := vniFromHostBridgeName(bridge.Name)
		if errors.As(err, &NotRouterInterfaceError{}) {
			slog.Debug("skipping bridge - not our naming pattern", "name", bridge.Name)
			continue // Not our bridge naming pattern
		}
		if err != nil {
			deleteErrors = append(deleteErrors, fmt.Errorf("failed to parse VNI from bridge %s: %w", bridge.Name, err))
			continue
		}

		if vnis[vni] {
			slog.Debug("keeping bridge - VNI is configured", "name", bridge.Name, "vni", vni)
			continue // Bridge should be kept
		}

		slog.Info("deleting auto-created OVS bridge", "name", bridge.Name, "vni", vni, "uuid", bridge.UUID)

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

	if len(ops) > 0 {
		txResult, err := ovs.Transact(ctx, ops...)
		if err != nil {
			deleteErrors = append(deleteErrors, fmt.Errorf("failed to delete OVS bridges: %w", err))
			slog.Error("failed to batch-delete OVS bridges", "error", err)
		} else if _, err := ovsdb.CheckOperationResults(txResult, ops); err != nil {
			deleteErrors = append(deleteErrors, fmt.Errorf("OVS bridge deletion operations failed: %w", err))
			slog.Error("OVS bridge deletion operations failed", "error", err)
		} else {
			slog.Info("successfully deleted auto-created OVS bridges")
		}
	}

	return errors.Join(deleteErrors...)
}

// removePortsFromBridge generates OVSDB operations to detach and delete ports
// from an OVS bridge. If filter is nil, all ports are removed. If filter is
// provided, each port is fetched and only ports for which filter returns true
// are included. Ports that cannot be fetched are silently skipped.
func removePortsFromBridge(ctx context.Context, ovs libovsclient.Client, bridge ovsmodel.Bridge, filter func(*ovsmodel.Port) bool) ([]ovsdb.Operation, error) {
	var matchingUUIDs []string
	for _, portUUID := range bridge.Ports {
		if filter != nil {
			port := &ovsmodel.Port{UUID: portUUID}
			if err := ovs.Get(ctx, port); err != nil {
				if errors.Is(err, libovsclient.ErrNotFound) {
					continue
				}
				return nil, fmt.Errorf("failed to get port %s from bridge %s: %w", portUUID, bridge.Name, err)
			}
			if !filter(port) {
				continue
			}
		}
		matchingUUIDs = append(matchingUUIDs, portUUID)
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
// non-managed OVS bridge for VNIs that are no longer configured.
func detachOurPortsFromBridge(ctx context.Context, ovs libovsclient.Client, bridge ovsmodel.Bridge, configuredVNIs map[int]bool) error {
	filter := func(port *ovsmodel.Port) bool {
		if !strings.HasPrefix(port.Name, HostVethPrefix) {
			return false
		}
		vni, err := vniFromHostVeth(port.Name)
		if err != nil {
			return false
		}
		return !configuredVNIs[vni]
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

func hostMaster(vni int, m HostMaster) (netlink.Link, error) {
	if !m.AutoCreate {
		hostMaster, err := netlink.LinkByName(m.Name)
		if err != nil {
			return nil, fmt.Errorf("could not find host master %s: %w", m.Name, err)
		}
		return hostMaster, nil
	}
	bridge, err := createHostBridge(vni)
	if err != nil {
		return nil, fmt.Errorf("getHostMaster: failed to create host bridge %d: %w", vni, err)
	}
	return bridge, nil
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
