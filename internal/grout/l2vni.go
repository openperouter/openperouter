// SPDX-License-Identifier:Apache-2.0

package grout

import (
	"context"
	"fmt"
	"log/slog"
	"net"

	"github.com/openperouter/openperouter/internal/hostnetwork"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
	"k8s.io/utils/ptr"
)

// SetupL2VNI creates a Grout bridge domain backed by a VXLAN interface and
// exposes it to the host through a TAP device.
func SetupL2VNI(ctx context.Context, client *Client, params hostnetwork.L2VNIParams) error {
	slog.DebugContext(ctx, "setup L2VNI", "vni", params.VNI, "vrf", params.VRF)
	defer slog.DebugContext(ctx, "setup L2VNI done", "vni", params.VNI)

	vrf := params.VRF
	if vrf != "" {
		if err := client.ensureVRF(ctx, vrf); err != nil {
			return fmt.Errorf("SetupL2VNI: failed to create VRF %s: %w", vrf, err)
		}
	} else {
		vrf = defaultVRFName
	}

	vtepIP, _, err := net.ParseCIDR(params.VTEPIP)
	if err != nil {
		return fmt.Errorf("SetupL2VNI: failed to parse VTEP IP %q: %w", params.VTEPIP, err)
	}

	vxlanName := fmt.Sprintf(vxlanNameFmt, params.VNI)
	if err := client.ensureVXLAN(ctx, vxlanName, vtepIP.String(), "", params.VNI,
		ptr.Deref(params.VXLanPort, defaultVXLanPort)); err != nil {
		return fmt.Errorf("SetupL2VNI: failed to create VXLAN: %w", err)
	}

	bridgeName := hostnetwork.BridgeName(params.VNI)
	if err := client.ensureBridge(ctx, bridgeName, vrf); err != nil {
		return fmt.Errorf("SetupL2VNI: failed to create bridge %s: %w", bridgeName, err)
	}
	if err := client.ensureBridgeMember(ctx, "vxlan", bridgeName, vxlanName); err != nil {
		return fmt.Errorf("SetupL2VNI: failed to attach VXLAN %s to bridge %s: %w", vxlanName, bridgeName, err)
	}

	linkPair := linkPairFromVNI(params.VNI)
	ns, err := netns.GetFromPath(params.TargetNS)
	if err != nil {
		return fmt.Errorf("SetupL2VNI: failed to find namespace %s: %w", params.TargetNS, err)
	}
	defer func() {
		if err := ns.Close(); err != nil {
			slog.Error("failed to close namespace", "namespace", params.TargetNS, "error", err)
		}
	}()

	if err := ensureTapPortInHostNamespace(ctx, client, linkPair.NamespaceSide, linkPair.HostSide, vrf, ns); err != nil {
		return fmt.Errorf("SetupL2VNI: failed to create TAP port: %w", err)
	}
	if err := client.ensureBridgeMember(ctx, "port", bridgeName, linkPair.NamespaceSide); err != nil {
		return fmt.Errorf("SetupL2VNI: failed to attach TAP %s to bridge %s: %w", linkPair.NamespaceSide, bridgeName, err)
	}
	if err := client.setPortUp(ctx, linkPair.NamespaceSide); err != nil {
		return fmt.Errorf("SetupL2VNI: failed to set Grout port up: %w", err)
	}

	hostTap, err := netlink.LinkByName(linkPair.HostSide)
	if err != nil {
		return fmt.Errorf("SetupL2VNI: host TAP %s not found after move: %w", linkPair.HostSide, err)
	}
	if params.HostMaster != nil {
		if err := hostnetwork.SetupHostMaster(ctx, params, hostTap); err != nil {
			return fmt.Errorf("SetupL2VNI: failed to set up host master: %w", err)
		}
	}

	if len(params.L2GatewayIPs) > 0 {
		if err := setupL2Gateway(ctx, client, bridgeName, params); err != nil {
			return fmt.Errorf("SetupL2VNI: failed to set up L2 gateway: %w", err)
		}
	}

	return nil
}

func setupL2Gateway(ctx context.Context, client *Client, bridgeName string, params hostnetwork.L2VNIParams) error {
	mac, err := hostnetwork.BridgeFixedMAC(params.VNI)
	if err != nil {
		return fmt.Errorf("failed to compute fixed MAC for VNI %d: %w", params.VNI, err)
	}
	if err := client.setBridgeMAC(ctx, bridgeName, net.HardwareAddr(mac).String()); err != nil {
		return fmt.Errorf("failed to set fixed MAC on bridge %s: %w", bridgeName, err)
	}

	for _, ip := range params.L2GatewayIPs {
		if err := client.ensureAddress(ctx, bridgeName, ip); err != nil {
			return fmt.Errorf("failed to assign L2 gateway IP %s to bridge %s: %w", ip, bridgeName, err)
		}
	}
	return nil
}
