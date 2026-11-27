// SPDX-License-Identifier:Apache-2.0

package hostnetwork

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
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
	// IPv6VXLanOverhead is the number of bytes added by VXLan over IPv6 encapsulation.
	IPv6VXLanOverhead = 70
)

type VNIParams struct {
	VRF            string
	TargetNS       string
	VTEPIP         string
	VNI            int32
	VXLanPort      *int32
	TunnelOverhead int
}

type L3VNIParams struct {
	VNIParams
	Name      string
	LinkIPs   *LinkIPs
	RouterMAC net.HardwareAddr
}

type L3PassthroughParams struct {
	TargetNS string
	LinkIPs  LinkIPs
}

type LinkIPs struct {
	HostIPv4 string
	NSIPv4   string
	HostIPv6 string
	NSIPv6   string
}

type L2VNIAttachment int

const (
	L2VNIAttachmentHost L2VNIAttachment = iota
	L2VNIAttachmentVFTrunk
)

type L2VNIParams struct {
	Attachment L2VNIAttachment
	VNIParams
	Name         string
	L2GatewayIPs []string
	HostMaster   *HostMaster
	Trunk        *L2VNITrunkParams
}

// L2VNITrunkParams maps an L2VNI to a VLAN tag on an L2Trunk.
type L2VNITrunkParams struct {
	Name string
	VLAN int32
}

// L2TrunkParams describes the port carrying the VLAN-tagged traffic of the
// L2VNIs referencing an L2Trunk.
type L2TrunkParams struct {
	Name     string
	TargetNS string
	SRIOV    *SRIOVTrunkParams
}

// SRIOVTrunkParams identifies the SR-IOV VF used as the trunk port.
type SRIOVTrunkParams struct {
	PFName            string
	VFIndex           int32
	AcceleratedConfig *AcceleratedConfigParams
}

// VNIConfigurator sets up VNIs in the router namespace. It borrows the
// namespace handle and the OVS client, so the caller must keep both open for as
// long as the configurator is in use.
type VNIConfigurator struct {
	ns              netns.NsHandle
	ovsClient       *OVSClient
	underlayMTU     int
	vtepDeviceIndex int
}

type HostMaster struct {
	Name       *string
	Type       string
	AutoCreate *bool
}

const (
	VRFLinkType       = "vrf"
	BridgeLinkType    = "LinuxBridge"
	VXLanLinkType     = "vxlan"
	OVSBridgeLinkType = "OVSBridge"
)

type NotRouterInterfaceError struct {
	Name string
}

func (e NotRouterInterfaceError) Error() string {
	return fmt.Sprintf("interface %s is not a router interface", e.Name)
}

// NewVNIConfigurator builds a VNIConfigurator for the router namespace handle
// ns, bound to ovsClient for OVS-backed host masters. The caller keeps
// ownership of both and must keep them open for as long as the configurator is
// in use.
func NewVNIConfigurator(ns netns.NsHandle, ovsClient *OVSClient) (*VNIConfigurator, error) {
	underlayMTU, err := FindUnderlayMTU(ns)
	if err != nil {
		return nil, fmt.Errorf("could not find underlay MTU: %w", err)
	}

	vtepDeviceIndex, err := tunnelEndpointIndexIn(ns)
	if err != nil {
		return nil, err
	}
	return &VNIConfigurator{
		ns:              ns,
		ovsClient:       ovsClient,
		underlayMTU:     underlayMTU,
		vtepDeviceIndex: vtepDeviceIndex,
	}, nil
}

func (c *VNIConfigurator) setupVRFInNS(ctx context.Context, vrf string, options ...string) error {
	return netnamespace.In(c.ns, func() error {
		slog.DebugContext(ctx, "setting up vrf", "vrf", vrf)
		return setupVRF(vrf, options...)
	})
}

// SetupL3VNI sets up a Layer 3 VNI in the router namespace the configurator
// was built for. It creates the VRF, then the private switching bridge, its
// external VXLAN device and the br-pe-<vni> VLAN endpoint that zebra discovers
// as the L3-SVI, and moves the veth to the VRF corresponding to the L3 routing
// domain, exposing it to the default host namespace. The SVI carries
// params.RouterMAC so all the L3VNIs of a node advertise the same RMAC.
func (c *VNIConfigurator) SetupL3VNI(ctx context.Context, params L3VNIParams) error {
	if err := c.setupVRFInNS(ctx, params.VRF); err != nil {
		return fmt.Errorf("SetupL3VNI: failed to setup VRF: %w", err)
	}
	_, endpoint, err := c.setupVNI(ctx, params.VNIParams, l3VNIBridgeOptions(params.RouterMAC)...)
	if err != nil {
		return fmt.Errorf("SetupL3VNI: failed to setup VNI: %w", err)
	}
	if err := netnamespace.In(c.ns, func() error {
		return enslaveEndpointToVRF(endpoint, params.VRF)
	}); err != nil {
		return fmt.Errorf("SetupL3VNI: failed to attach L3-SVI to VRF %s: %w", params.VRF, err)
	}

	slog.DebugContext(ctx, "setting up l3 VNI", "params", params)
	defer slog.DebugContext(ctx, "end setting up l3 VNI", "params", params)

	if params.LinkIPs == nil {
		slog.DebugContext(ctx, "no host veth configured, skipping setup")
		return nil
	}

	if err := c.setupHostVeth(
		ctx,
		vethNamesFromVNI(params.VNI),
		params.LinkIPs,
		params.VRF,
		params.TunnelOverhead); err != nil {
		return fmt.Errorf("SetupL3VNI: failed to setup host veth pair: %w", err)
	}
	return nil
}

func l3VNIBridgeOptions(mac net.HardwareAddr) []NetlinkOption {
	options := []NetlinkOption{setAddrGenModeNone}
	if len(mac) == 0 {
		return options
	}
	return append(options, bridgeMACOption(mac))
}

// bridgeMACOption returns an option that sets mac on the endpoint.
func bridgeMACOption(mac net.HardwareAddr) NetlinkOption {
	return func(link netlink.Link) error {
		return ensureBridgeMACAddress(link, mac)
	}
}

// SetupL2VNI sets up a Layer 2 VNI in the router namespace the configurator
// was built for. It creates the private switching bridge, its external VXLAN
// device and the br-pe-<vni> VLAN endpoint, then connects the router-side veth
// leg to the bridge as an untagged access port, exposing the L2 domain to the
// default host namespace.
func (c *VNIConfigurator) SetupL2VNI(ctx context.Context, params L2VNIParams) error {
	var endpointOptions []NetlinkOption
	if len(params.L2GatewayIPs) > 0 {
		mac, err := BridgeFixedMAC(params.VNI)
		if err != nil {
			return fmt.Errorf("SetupL2VNI: failed to compute gateway MAC: %w", err)
		}
		// The same MAC on every node keeps the distributed gateway identity shared.
		endpointOptions = append(endpointOptions, bridgeMACOption(mac))
	}

	bridge, endpoint, err := c.setupVNI(ctx, params.VNIParams, endpointOptions...)
	if err != nil {
		return fmt.Errorf("SetupL2VNI: failed to setup VNI: %w", err)
	}
	vethNames := vethNamesFromVNI(params.VNI)
	hostVeth, err := setupNamespacedVethInNamespace(ctx, vethNames, c.ns)
	if err != nil {
		return fmt.Errorf("SetupL2VNI: failed to setup VNI veth: %w", err)
	}

	slog.DebugContext(ctx, "setting up l2 VNI", "params", params)
	defer slog.DebugContext(ctx, "end setting up l2 VNI", "params", params)

	slog.Info("SetupL2VNI: found host veth", "name", hostVeth.Attrs().Name, "index", hostVeth.Attrs().Index)

	if err := SetVethMTUForTunnelOverhead(hostVeth, c.underlayMTU, params.TunnelOverhead); err != nil {
		return fmt.Errorf("SetupL2VNI: failed to set MTU on host veth %s: %w", vethNames.HostSide, err)
	}

	if params.HostMaster != nil {
		if err := SetupHostMaster(ctx, c.ovsClient, params, hostVeth); err != nil {
			return err
		}
	}

	if err := netnamespace.In(c.ns, func() error {
		return c.setupL2VNIRouterSide(params, vethNames.NamespaceSide, bridge, endpoint)
	}); err != nil {
		return err
	}

	return nil
}

// setupL2VNIRouterSide configures the router side of the veth pair that backs
// an L2VNI and, when the VNI has a gateway, the br-pe-<vni> endpoint that owns
// the gateway addresses, anycast MAC and VRF membership.
func (c *VNIConfigurator) setupL2VNIRouterSide(params L2VNIParams, vethName string, bridge *netlink.Bridge,
	endpoint netlink.Link) error {
	peVeth, err := netlink.LinkByName(vethName)
	if err != nil {
		return fmt.Errorf("could not find peer veth %s in the router namespace: %w", vethName, err)
	}

	if err := SetVethMTUForTunnelOverhead(peVeth, c.underlayMTU, params.TunnelOverhead); err != nil {
		return fmt.Errorf("failed to set MTU on pe veth %s: %w", vethName, err)
	}

	if err := linkSetMaster(peVeth, bridge); err != nil {
		return fmt.Errorf("failed to set bridge %s as master of pe veth %s: %w", bridge.Name, peVeth.Attrs().Name, err)
	}
	if err := ensurePortVlans(peVeth, newAccessPort(internalVLANID)); err != nil {
		return fmt.Errorf("failed to configure access VLAN on pe veth %s: %w", peVeth.Attrs().Name, err)
	}

	if len(params.L2GatewayIPs) == 0 {
		return nil
	}

	if err := enslaveEndpointToVRF(endpoint, params.VRF); err != nil {
		return fmt.Errorf("failed to attach gateway endpoint %s to VRF %s: %w", endpoint.Attrs().Name, params.VRF, err)
	}
	for _, ip := range params.L2GatewayIPs {
		if err := AssignIPToInterface(endpoint, ip); err != nil {
			return fmt.Errorf("failed to assign L2 gateway IP %s to %s: %w", ip, endpoint.Attrs().Name, err)
		}
	}

	// The gateway MAC is already set on the endpoint by the bridge MAC option.
	mac, err := BridgeFixedMAC(params.VNI)
	if err != nil {
		return err
	}
	if err := ensureLocalFDBEntry(bridge, internalVLANID, mac); err != nil {
		return err
	}
	return nil
}

func SetupHostMaster(ctx context.Context, ovsClient *OVSClient, params L2VNIParams, hostVeth netlink.Link) error {
	bridgeConfig := *params.HostMaster
	switch bridgeConfig.Type {
	case OVSBridgeLinkType:
		lowerDeviceName := ptr.Deref(bridgeConfig.Name, "")
		if ptr.Deref(bridgeConfig.AutoCreate, false) {
			lowerDeviceName = hostBridgeName(params.VNI)
		}
		if err := ensureOVSBridgeAndAttach(ctx, ovsClient, lowerDeviceName, hostVeth.Attrs().Name); err != nil {
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

// setupVNI creates the private VLAN-aware switching bridge that backs a VNI,
// its external vnifilter VXLAN device, the internal VLAN-to-VNI mapping and the
// br-pe-<vni> VLAN endpoint. The endpoint keeps the name downstream code
// depends on while becoming the VLAN device zebra discovers as the SVI. The
// options are applied to the endpoint before it is brought up.
func (c *VNIConfigurator) setupVNI(ctx context.Context, params VNIParams,
	endpointOptions ...NetlinkOption) (*netlink.Bridge, netlink.Link, error) {
	slog.DebugContext(ctx, "setting up VNI", "params", params)
	defer slog.DebugContext(ctx, "end setting up VNI", "params", params)

	var bridge *netlink.Bridge
	var endpoint netlink.Link
	err := netnamespace.In(c.ns, func() error {
		var err error
		bridge, err = ensureSwitchingBridge(switchingBridgeName(params.VNI))
		if err != nil {
			return err
		}
		if err := ensurePortVlans(bridge, newTrunk([]uint16{internalVLANID}), withBridgeInterface()); err != nil {
			return err
		}
		if params.VXLanPort == nil {
			return errors.New("failed to parse VXLAN information, VXLAN port is nil")
		}
		svd, err := ensureSVD(svdParams{
			Name:            vxLanNameFromVNI(params.VNI),
			Bridge:          bridge,
			VTEPIP:          params.VTEPIP,
			Port:            *params.VXLanPort,
			VtepDeviceIndex: c.vtepDeviceIndex,
			HardwareAddr:    bridge.HardwareAddr,
		})
		if err != nil {
			return err
		}
		if err := ensureVlanToVNI(svd, internalVLANID, uint32(params.VNI)); err != nil {
			return err
		}
		endpoint, err = ensureVLANEndpoint(BridgeName(params.VNI), bridge, internalVLANID, endpointOptions...)
		return err
	})
	if err != nil {
		return nil, nil, err
	}
	return bridge, endpoint, nil
}

// enslaveEndpointToVRF binds the VLAN endpoint to its routing domain, or
// detaches it when the VNI has no VRF.
func enslaveEndpointToVRF(endpoint netlink.Link, vrfName string) error {
	if vrfName == "" {
		if endpoint.Attrs().MasterIndex == 0 {
			return nil
		}
		if err := netlink.LinkSetNoMaster(endpoint); err != nil {
			return fmt.Errorf("detaching VLAN endpoint %s from its master: %w", endpoint.Attrs().Name, err)
		}
		return nil
	}
	vrf, err := lookupVRF(vrfName)
	if err != nil {
		return fmt.Errorf("could not find VRF %s for VLAN endpoint %s: %w", vrfName, endpoint.Attrs().Name, err)
	}
	if err := linkSetMaster(endpoint, vrf); err != nil {
		return fmt.Errorf("failed to set VRF %s as master of VLAN endpoint %s: %w", vrfName, endpoint.Attrs().Name, err)
	}
	return nil
}

// RemoveAllVNIs removes from the target namespace the bridges / veths
// for all VNIs.
func RemoveAllVNIs(ovsClient *OVSClient, targetNS string) error {
	return RemoveNonConfiguredVNIs(ovsClient, targetNS, []VNIParams{})
}

// RemoveNonConfiguredVNIs removes from the target namespace the
// leftovers corresponding to VNIs that are not configured anymore.
func RemoveNonConfiguredVNIs(ovsClient *OVSClient, targetNS string, params []VNIParams) error {
	vnis := map[int32]bool{}
	for _, p := range params {
		vnis[p.VNI] = true
	}

	errs := removeHostSideVNIs(vnis, ovsClient)

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
		nsErrors := removeNamespaceSideVNIs(vnis)
		return errors.Join(nsErrors...)
	}); err != nil {
		errs = append(errs, err)
	}

	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("RemoveNonConfiguredVNIs: %w", err)
	}
	return nil
}

func removeHostSideVNIs(vnis map[int32]bool, ovsClient *OVSClient) []error {
	var failedDeletes []error

	hostLinks, err := netlink.LinkList()
	if err != nil {
		return []error{fmt.Errorf("remove non configured vnis: failed to list links: %w", err)}
	}
	if err := deleteLinksForType(BridgeLinkType, vnis, hostLinks, vniFromHostBridgeName); err != nil {
		failedDeletes = append(failedDeletes, fmt.Errorf("remove bridge links: %w", err))
	}
	if err := removeOVSBridgesForVNIs(context.Background(), ovsClient, vnis); err != nil {
		failedDeletes = append(failedDeletes, fmt.Errorf("remove OVS bridges: %w", err))
	}

	return append(failedDeletes, removeHostSideVeths(hostLinks, HostEVPNPrefix, vnis)...)
}

func removeNamespaceSideVNIs(vnis map[int32]bool) []error {
	var failedDeletes []error

	links, err := netlink.LinkList()
	if err != nil {
		return []error{fmt.Errorf("remove non configured vnis: failed to list links: %w", err)}
	}
	// Delete upper devices before their parents: a previous partial deletion may
	// already have removed some children from this snapshot.
	for _, owned := range []struct {
		kind  string
		parse func(string) (int32, error)
	}{
		// br-pe-<vni> VLAN endpoints and pre-migration macvlan leftovers.
		{"vlan", vniFromBridgeName},
		{"macvlan", vniFromBridgeName},
		// vni<vni> external VXLAN devices.
		{VXLanLinkType, vniFromVXLanName},
		// br-vl-<vni> switching bridges.
		{BridgeLinkType, vniFromSwitchingBridgeName},
		// br-pe-<vni> bridges created by the pre-VLAN-aware model.
		{BridgeLinkType, vniFromBridgeName},
	} {
		if err := deleteLinksForType(owned.kind, vnis, links, owned.parse); err != nil {
			failedDeletes = append(failedDeletes, fmt.Errorf("remove %s links: %w", owned.kind, err))
		}
	}
	return failedDeletes
}

// deleteLinks deletes all the links of the given type that do not correspond to
// any VNI.
func deleteLinksForType(linkType string, vnis map[int32]bool, links []netlink.Link, vniFromName func(string) (int32, error)) error {
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
func removeOVSBridgesForVNIs(ctx context.Context, ovsClient *OVSClient, vnis map[int32]bool) error {
	ovs, err := ovsClient.Get(ctx)
	if err != nil {
		// OVS not available, skip cleanup gracefully
		slog.Debug("OVS client not available, skipping OVS bridge cleanup", "error", err)
		return nil
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
// non-managed OVS bridge for VNIs that are no longer configured.
func detachOurPortsFromBridge(ctx context.Context, ovs libovsclient.Client, bridge ovsmodel.Bridge, configuredVNIs map[int32]bool) error {
	filter := func(port *ovsmodel.Port) bool {
		if !strings.HasPrefix(port.Name, HostEVPNPrefix) {
			return false
		}
		vni, err := interfaceIDFromPrefix(port.Name, HostEVPNPrefix)
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

// tunnelEndpointIndexIn returns the index of the VTEP source device in ns.
func tunnelEndpointIndexIn(ns netns.NsHandle) (int, error) {
	var index int
	if err := netnamespace.In(ns, func() error {
		tunnelEndpoint, err := netlink.LinkByName(TunnelLoopback)
		if err != nil {
			return fmt.Errorf("failed looking for vtep interface %s: %w", TunnelLoopback, err)
		}
		index = tunnelEndpoint.Attrs().Index
		return nil
	}); err != nil {
		return 0, err
	}
	return index, nil
}
