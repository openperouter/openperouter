// SPDX-License-Identifier:Apache-2.0

package hostnetwork

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/openperouter/openperouter/internal/netnamespace"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
	"golang.org/x/sys/unix"
)

const vniFilterProbeDevice = "opvnifprobe"

// vniFilterSupport caches a successful capability probe: the probe creates and
// deletes a device, so it must not run on every reconcile.
var vniFilterSupport atomic.Bool

// probeVniFilterDevice is a test seam.
var probeVniFilterDevice = func() error {
	if link, err := netlink.LinkByName(vniFilterProbeDevice); err == nil {
		if err := netlink.LinkDel(link); err != nil {
			return fmt.Errorf("removing stale probe device %s: %w", vniFilterProbeDevice, err)
		}
	}
	probe := &netlink.Vxlan{
		LinkAttrs: netlink.LinkAttrs{Name: vniFilterProbeDevice},
		FlowBased: true,
		Port:      4789,
	}
	if _, err := createVniFilterVxlan(probe); err != nil {
		return err
	}
	link, err := netlink.LinkByName(vniFilterProbeDevice)
	if err != nil {
		return fmt.Errorf("looking up probe device %s: %w", vniFilterProbeDevice, err)
	}
	if err := netlink.LinkDel(link); err != nil {
		return fmt.Errorf("removing probe device %s: %w", vniFilterProbeDevice, err)
	}
	return nil
}

// EnsureVniFilterSupport verifies the kernel can create an external vnifilter
// VXLAN device before any legacy device is purged. It fails closed: a node
// without support keeps its current dataplane instead of losing it to a
// migration it cannot complete.
func EnsureVniFilterSupport(targetNS string) error {
	if vniFilterSupport.Load() {
		return nil
	}
	ns, err := netns.GetFromPath(targetNS)
	if err != nil {
		return fmt.Errorf("EnsureVniFilterSupport: failed to get network namespace %s: %w", targetNS, err)
	}
	defer func() {
		if err := ns.Close(); err != nil {
			slog.Error("failed to close namespace", "namespace", targetNS, "error", err)
		}
	}()

	if err := netnamespace.In(ns, probeVniFilterDevice); err != nil {
		slog.Info("VXLAN VNI filtering is not supported, keeping the current dataplane",
			"namespace", targetNS, "error", err)
		return fmt.Errorf("kernel does not support VXLAN VNI filtering (Linux 5.18 or newer is required): %w", err)
	}
	vniFilterSupport.Store(true)
	return nil
}

// PurgeLegacyVXLans deletes the traditional per-VNI VXLAN devices that
// pre-date the VLAN-aware model. A traditional device and an external
// vnifilter device cannot share a UDP port in one network namespace, so every
// legacy device on a configured port must go before any external device is
// brought up.
func PurgeLegacyVXLans(targetNS string, ports []int32) error {
	if len(ports) == 0 {
		return nil
	}
	configuredPorts := make(map[int32]struct{}, len(ports))
	for _, port := range ports {
		configuredPorts[port] = struct{}{}
	}

	ns, err := netns.GetFromPath(targetNS)
	if err != nil {
		return fmt.Errorf("PurgeLegacyVXLans: failed to get network namespace %s: %w", targetNS, err)
	}
	defer func() {
		if err := ns.Close(); err != nil {
			slog.Error("failed to close namespace", "namespace", targetNS, "error", err)
		}
	}()

	return netnamespace.In(ns, func() error {
		links, err := netlink.LinkList()
		if err != nil {
			return fmt.Errorf("listing links in %s: %w", targetNS, err)
		}
		var errs []error
		for _, link := range links {
			if err := purgeLegacyVXLan(link, configuredPorts); err != nil {
				errs = append(errs, err)
			}
		}
		return errors.Join(errs...)
	})
}

// setupVXLan sets up a vxlan interface corresponding to the provided
// vniParams.
func setupVXLan(params VNIParams, bridge *netlink.Bridge, vtepDeviceIndex int) error {
	vxlan, err := createVXLan(params, bridge, vtepDeviceIndex)
	if err != nil {
		return err
	}

	if err := setAddrGenModeNone(vxlan); err != nil {
		return fmt.Errorf("failed to set addr_gen_mode to 1 for %s: %w", vxlan.Name, err)
	}
	if err := netlink.LinkSetBrNeighSuppress(vxlan, true); err != nil {
		return fmt.Errorf("failed to set neigh suppression for %s: %w", vxlan.Name, err)
	}
	// LinkSetLearning disables MAC learning for the bridge_slave. ip link syntax is:
	// ip link set <...> type bridge_slave learning off
	// Note that this is different from the VXLAN's 'nolearning' parameter (which we set inside createVXLan via
	// Learning: false) and which disables VTEP learning.
	if err := netlink.LinkSetLearning(vxlan, false); err != nil {
		return fmt.Errorf("failed to disable MAC learning (learning off) for %s: %w", vxlan.Name, err)
	}

	if err = linkSetUp(vxlan); err != nil {
		return fmt.Errorf("could not set link up for vxlan %s: %v", vxlan.Name, err)
	}

	return nil
}

// checkVXLanConfigured reports why an existing VXLAN must be replaced. An
// external vnifilter device has VxlanId 0: the VNI set lives in the VNI filter
// entries, so comparing VxlanId alone would recreate the device on every
// reconcile. The entries themselves are converged in place by
// ensureVlanToVNI and removeVlanToVNI.
func checkVXLanConfigured(observed, desired *netlink.Vxlan, vniFilterEnabled bool) error {
	if !observed.FlowBased || observed.VxlanId != 0 {
		return errors.New("VXLAN is not an external metadata device")
	}
	if !vniFilterEnabled {
		return errors.New("VXLAN VNI filtering is not enabled")
	}
	if observed.Port != desired.Port {
		return fmt.Errorf("port is not one coming from params: %d, %d", observed.Port, desired.Port)
	}
	if observed.Learning {
		return errors.New("VTEP learning is enabled")
	}
	if !observed.SrcAddr.Equal(desired.SrcAddr) {
		return fmt.Errorf("src addr does not match vtep ip: %v, expected %v", observed.SrcAddr, desired.SrcAddr)
	}
	if observed.VtepDevIndex != desired.VtepDevIndex {
		return fmt.Errorf("vtep dev index is not tunnel endpoint index: %d %d", observed.VtepDevIndex, desired.VtepDevIndex)
	}
	return nil
}

// checkLegacyVXLanConfigured checks if a pre-VLAN-aware VXLan has the required
// properties passed as parameters.
func checkLegacyVXLanConfigured(vxLan *netlink.Vxlan, bridgeIndex, vtepDeviceIndex int, params VNIParams) error {
	if vxLan.MasterIndex != bridgeIndex {
		return fmt.Errorf("master index is not bridge index: %d, %d", vxLan.MasterIndex, bridgeIndex)
	}

	if vxLan.VxlanId != int(params.VNI) {
		return fmt.Errorf("vxlanid is not vni: %d, %d", vxLan.VxlanId, params.VNI)
	}

	if params.VXLanPort == nil {
		return errors.New("failed to parse VXLAN information, VXLAN port is nil")
	}

	paramsVXLanPort := int(*params.VXLanPort)
	if vxLan.Port != paramsVXLanPort {
		return fmt.Errorf("port is not one coming from params: %d, %d", vxLan.Port, paramsVXLanPort)
	}
	if vxLan.Learning {
		return errors.New("VTEP learning is enabled")
	}
	if err := validateVxlan(vxLan, params); err != nil {
		return err
	}
	if vxLan.VtepDevIndex != vtepDeviceIndex {
		return fmt.Errorf("vtep dev index is not tunnel endpoint index: %d %d", vxLan.VtepDevIndex, vtepDeviceIndex)
	}
	return nil
}

func createVXLan(params VNIParams, bridge *netlink.Bridge, vtepDeviceIndex int) (*netlink.Vxlan, error) {
	vtepIP, _, err := net.ParseCIDR(params.VTEPIP)
	if err != nil {
		return nil, fmt.Errorf("failed to parse vtep ip %v: %w", params.VTEPIP, err)
	}

	if params.VXLanPort == nil {
		return nil, errors.New("failed to parse VXLAN information, VXLAN port is nil")
	}

	vxlanName := vxLanNameFromVNI(params.VNI)
	toCreate := &netlink.Vxlan{
		LinkAttrs: netlink.LinkAttrs{
			Name:        vxlanName,
			MasterIndex: bridge.Index,
		},
		VxlanId:      int(params.VNI),
		Port:         int(*params.VXLanPort),
		Learning:     false, // Disable VTEP learning (ip link add <...> type vxlan <...> nolearning).
		VtepDevIndex: vtepDeviceIndex,
		SrcAddr:      vtepIP,
	}

	link, err := netlink.LinkByName(vxlanName)
	if err != nil && errors.As(err, &netlink.LinkNotFoundError{}) {
		if err := netlink.LinkAdd(toCreate); err != nil {
			return nil, fmt.Errorf("failed to create vxlan %s: %w", vxlanName, err)
		}
		return toCreate, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get vxlan link by name %s: %w", vxlanName, err)
	}
	vxlan, ok := link.(*netlink.Vxlan)
	if ok && checkLegacyVXLanConfigured(vxlan, bridge.Index, vtepDeviceIndex, params) == nil {
		return vxlan, nil
	}
	if err := netlink.LinkDel(link); err != nil {
		return nil, fmt.Errorf("failed to delete link %v: %w", link, err)
	}

	if err = netlink.LinkAdd(toCreate); err != nil {
		return nil, fmt.Errorf("failed to create vxlan %s: %w", vxlan.Name, err)
	}
	return toCreate, nil
}

const vniPrefix = "vni"

func vxLanNameFromVNI(vni int32) string {
	return fmt.Sprintf("%s%d", vniPrefix, vni)
}

func vniFromVXLanName(name string) (int32, error) {
	if !strings.HasPrefix(name, vniPrefix) {
		return 0, NotRouterInterfaceError{Name: name}
	}
	vni := strings.TrimPrefix(name, vniPrefix)
	res, err := strconv.ParseInt(vni, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("failed to get vni for vxlan %s", name)
	}
	return int32(res), nil
}

func validateVxlan(vxLan *netlink.Vxlan, params VNIParams) error {
	vtepIP, _, err := net.ParseCIDR(params.VTEPIP)
	if err != nil {
		return fmt.Errorf("failed to parse vtep ip %v: %w", params.VTEPIP, err)
	}
	if !vxLan.SrcAddr.Equal(vtepIP) {
		return fmt.Errorf("src addr does not match vtep ip: %v, expected %v", vxLan.SrcAddr, vtepIP)
	}
	return nil
}

// purgeLegacyVXLan deletes link when it is a legacy per-VNI VXLAN on a
// configured port. Links that are not legacy VXLANs are ignored.
func purgeLegacyVXLan(link netlink.Link, configuredPorts map[int32]struct{}) error {
	vxlan, ok := link.(*netlink.Vxlan)
	if !ok {
		return nil
	}
	if _, configured := configuredPorts[int32(vxlan.Port)]; !configured {
		return nil
	}
	vni, err := vniFromVXLanName(vxlan.Name)
	if errors.As(err, &NotRouterInterfaceError{}) {
		return nil
	}
	if err != nil {
		return err
	}
	// External devices have no fixed VNI and are already migrated.
	if vxlan.FlowBased && vxlan.VxlanId == 0 {
		return nil
	}
	if vxlan.VxlanId != int(vni) {
		return nil
	}
	slog.Info("purging legacy VXLAN device", "device", vxlan.Name, "port", vxlan.Port,
		"reason", "device does not use external VNI filtering")
	if err := netlink.LinkDel(link); err != nil && !errors.Is(err, unix.ENODEV) {
		return fmt.Errorf("deleting legacy VXLAN %s: %w", vxlan.Name, err)
	}
	return nil
}
