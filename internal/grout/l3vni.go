// SPDX-License-Identifier:Apache-2.0

package grout

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"slices"

	"github.com/openperouter/openperouter/internal/hostnetwork"
	"k8s.io/utils/ptr"
)

const (
	vxlanNameFmt     = "vni%d"
	defaultVRFName   = "main"
	defaultVXLanPort = 4789
)

func SetupL3VNI(ctx context.Context, client *Client, params hostnetwork.L3VNIParams) error {
	slog.DebugContext(ctx, "setup L3VNI", "vrf", params.VRF, "vni", params.VNI)
	defer slog.DebugContext(ctx, "setup L3VNI done", "vrf", params.VRF, "vni", params.VNI)

	if err := setupVNI(ctx, client, params.VNIParams); err != nil {
		return fmt.Errorf("SetupL3VNI: failed to setup VNI: %w", err)
	}

	if params.LinkIPs == nil {
		slog.DebugContext(ctx, "no host TAP configured, skipping setup")
		return nil
	}

	if err := setupHostTap(ctx, client, linkPairFromVNI(params.VNI), params.TargetNS, params.VRF,
		*params.LinkIPs, params.TunnelOverhead); err != nil {
		return fmt.Errorf("SetupL3VNI: failed to setup host TAP: %w", err)
	}
	return nil
}

func setupVNI(ctx context.Context, client *Client, params hostnetwork.VNIParams) error {
	if err := client.ensureVRF(ctx, params.VRF); err != nil {
		return fmt.Errorf("failed to create VRF %s: %w", params.VRF, err)
	}

	vtepIP, _, err := net.ParseCIDR(params.VTEPIP)
	if err != nil {
		return fmt.Errorf("failed to parse vtep ip %v: %w", params.VTEPIP, err)
	}

	if err := client.ensureVXLAN(ctx,
		fmt.Sprintf(vxlanNameFmt, params.VNI),
		vtepIP.String(),
		params.VRF,
		params.VNI,
		ptr.Deref(params.VXLanPort, defaultVXLanPort),
	); err != nil {
		return fmt.Errorf("failed to create vxlan interface: %w", err)
	}

	return nil
}

func RemoveAllVNIs(ctx context.Context, client *Client, ovsClient *hostnetwork.OVSClient, targetNS string) error {
	return RemoveNonConfiguredVNIs(ctx, client, ovsClient, targetNS, []hostnetwork.VNIParams{})
}

func RemoveNonConfiguredVNIs(ctx context.Context, client *Client, ovsClient *hostnetwork.OVSClient, targetNamespace string, configured []hostnetwork.VNIParams) error {
	slog.DebugContext(ctx, "removing stale VNIs")
	defer slog.DebugContext(ctx, "removing stale VNIs done")

	configuredVNIs := make(map[int32]struct{}, len(configured))
	for _, p := range configured {
		configuredVNIs[p.VNI] = struct{}{}
	}

	ifaces, err := client.listInterfaces(ctx)
	if err != nil {
		return fmt.Errorf("RemoveNonConfiguredVNIs: failed to list interfaces: %w", err)
	}

	staleVNIs := findStaleVNIs(ifaces, configuredVNIs)
	for _, vni := range staleVNIs {
		slog.InfoContext(ctx, "removing stale VNI", "vni", vni)
		if err := removeVNI(ctx, client, vni); err != nil {
			return fmt.Errorf("RemoveNonConfiguredVNIs: failed to remove VNI %d: %w", vni, err)
		}
	}

	return hostnetwork.RemoveNonConfiguredVNIs(ovsClient, targetNamespace, configured)
}

// RemoveAllVRFs removes all non-default VRFs from the target namespace.
func RemoveAllVRFs(ctx context.Context, client *Client, targetNS string) error {
	return RemoveNonConfiguredVRFs(ctx, client, targetNS, []string{})
}

func RemoveNonConfiguredVRFs(ctx context.Context, client *Client, targetNamespace string, configuredVRFs []string) error {
	ifaces, err := client.listInterfaces(ctx)
	if err != nil {
		return fmt.Errorf("RemoveNonConfiguredVRFs: failed to list interfaces: %w", err)
	}

	staleVRFs := findStaleVRFs(ifaces, configuredVRFs)
	for _, vrf := range staleVRFs {
		if vrf == defaultVRFName {
			continue
		}
		slog.InfoContext(ctx, "removing stale VRF", "vrf", vrf)
		if err := client.deleteInterface(ctx, vrf); err != nil {
			return fmt.Errorf("failed to delete VRF %s: %w", vrf, err)
		}
	}

	return nil
}

func removeVNI(ctx context.Context, client *Client, vni int32) error {
	if err := removeHostTap(ctx, client, linkPairFromVNI(vni)); err != nil {
		return err
	}

	bridgeName := hostnetwork.BridgeName(vni)
	if err := deleteBridgeAttachedPorts(ctx, client, bridgeName); err != nil {
		return err
	}

	if err := client.deleteInterface(ctx, bridgeName); err != nil {
		return fmt.Errorf("failed to delete bridge %s: %w", bridgeName, err)
	}

	vxlanName := vxlanNameFromVNI(vni)
	if err := client.deleteInterface(ctx, vxlanName); err != nil {
		return fmt.Errorf("failed to delete VXLAN %s: %w", vxlanName, err)
	}

	return nil
}

func findStaleVNIs(ifaces []groutInterface, configuredVNIs map[int32]struct{}) []int32 {
	var stale []int32
	for _, iface := range ifaces {
		if iface.Type != "vxlan" {
			continue
		}
		vni, ok := vniFromVXLANName(iface.Name)
		if !ok {
			continue
		}
		if _, ok := configuredVNIs[vni]; !ok {
			stale = append(stale, vni)
		}
	}
	return stale
}

func findStaleVRFs(ifaces []groutInterface, configuredVRFs []string) []string {
	var stale []string
	for _, iface := range ifaces {
		if iface.Type != "vrf" {
			continue
		}

		if !slices.Contains(configuredVRFs, iface.Name) {
			stale = append(stale, iface.Name)
		}

	}
	return stale
}

// linkPairFromVNI returns the names of the link pair legs
// corresponding to the default namespace and the target namespace, based on VNI.
func linkPairFromVNI(vni int32) hostnetwork.VethNames {
	hostSide := fmt.Sprintf("%s%d", hostnetwork.HostEVPNPrefix, vni)
	peSide := fmt.Sprintf("%s%d", hostnetwork.PEEVPNPrefix, vni)
	return hostnetwork.VethNames{HostSide: hostSide, NamespaceSide: peSide}
}

func vxlanNameFromVNI(vni int32) string {
	return fmt.Sprintf(vxlanNameFmt, vni)
}

func vniFromVXLANName(name string) (int32, bool) {
	var vni int32
	if n, _ := fmt.Sscanf(name, vxlanNameFmt, &vni); n != 1 {
		return 0, false
	}
	// Sscanf ignores trailing text after the VNI.
	return vni, vxlanNameFromVNI(vni) == name
}

// deleteBridgeAttachedPorts removes bridge members before the bridge itself
// can be deleted.
func deleteBridgeAttachedPorts(ctx context.Context, client *Client, bridgeName string) error {
	ifaces, err := client.listInterfaces(ctx)
	if err != nil {
		return fmt.Errorf("failed to list interfaces: %w", err)
	}
	for _, iface := range ifaces {
		if iface.Domain != bridgeName {
			continue
		}
		if err := client.deleteInterface(ctx, iface.Name); err != nil {
			return fmt.Errorf("failed to delete VLAN %s of bridge %s: %w", iface.Name, bridgeName, err)
		}
	}
	return nil
}
