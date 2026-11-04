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

// SetupL2VNI creates a Grout bridge domain backed by a VXLAN interface. The
// bridge is attached to the trunk port of the referenced L2Trunk through a
// VLAN sub-interface when the L2VNI is carried by a trunk, and exposed to the
// host through a TAP device otherwise. trunkPorts maps the name of each L2Trunk
// ready to carry L2VNIs to its grout port.
func SetupL2VNI(
	ctx context.Context,
	client *Client,
	ovsClient *hostnetwork.OVSClient,
	params hostnetwork.L2VNIParams,
	trunkPorts map[string]string,
) error {
	slog.DebugContext(ctx, "setup L2VNI", "vni", params.VNI, "vrf", params.VRF)
	defer slog.DebugContext(ctx, "setup L2VNI done", "vni", params.VNI)

	bridgeName, err := setupL2VNIBridge(ctx, client, params)
	if err != nil {
		return fmt.Errorf("SetupL2VNI: %w", err)
	}
	if err := attachL2VNIBridge(ctx, client, ovsClient, params, bridgeName, trunkPorts); err != nil {
		return fmt.Errorf("SetupL2VNI: %w", err)
	}

	if len(params.L2GatewayIPs) > 0 {
		if err := setupL2Gateway(ctx, client, bridgeName, params); err != nil {
			return fmt.Errorf("SetupL2VNI: failed to set up L2 gateway: %w", err)
		}
	}
	return nil
}

func attachL2VNIBridge(
	ctx context.Context,
	client *Client,
	ovsClient *hostnetwork.OVSClient,
	params hostnetwork.L2VNIParams,
	bridgeName string,
	trunkPorts map[string]string,
) error {
	if params.Trunk == nil {
		if err := deleteBridgeVLANs(ctx, client, bridgeName, ""); err != nil {
			return err
		}
		return attachBridgeToHost(ctx, client, ovsClient, params, bridgeName)
	}
	trunkPort, ok := trunkPorts[params.Trunk.Name]
	if !ok {
		return fmt.Errorf("L2Trunk %q failed provisioning", params.Trunk.Name)
	}
	vlanName := trunkVLANName(trunkPort, params.Trunk.VLAN)
	if err := client.ensureVLAN(ctx, vlanName, trunkPort, params.Trunk.VLAN); err != nil {
		return fmt.Errorf("failed to create VLAN %s for trunk %s: %w", vlanName, params.Trunk.Name, err)
	}
	if err := client.ensureBridgeMember(ctx, interfaceTypeVLAN, bridgeName, vlanName); err != nil {
		return fmt.Errorf("failed to attach trunk %s to bridge %s: %w", params.Trunk.Name, bridgeName, err)
	}
	// The VLAN is named after its trunk and tag, so the one left behind by a
	// previous trunk or tag of the L2VNI is still attached to the bridge.
	return deleteBridgeVLANs(ctx, client, bridgeName, vlanName)
}

// attachBridgeToHost exposes the bridge to the host through a TAP device,
// enslaving the host side to the host master when one is configured.
func attachBridgeToHost(
	ctx context.Context,
	client *Client,
	ovsClient *hostnetwork.OVSClient,
	params hostnetwork.L2VNIParams,
	bridgeName string,
) error {
	linkPair := linkPairFromVNI(params.VNI)
	ns, err := netns.GetFromPath(params.TargetNS)
	if err != nil {
		return fmt.Errorf("failed to find namespace %s: %w", params.TargetNS, err)
	}
	defer func() {
		if err := ns.Close(); err != nil {
			slog.Error("failed to close namespace", "namespace", params.TargetNS, "error", err)
		}
	}()

	if err := ensureTapPortInHostNamespace(ctx, client, linkPair.NamespaceSide, linkPair.HostSide,
		bridgeVRF(params), ns); err != nil {
		return fmt.Errorf("failed to create TAP port: %w", err)
	}
	if err := client.ensureBridgeMember(ctx, interfaceTypePort, bridgeName, linkPair.NamespaceSide); err != nil {
		return fmt.Errorf("failed to attach TAP %s to bridge %s: %w", linkPair.NamespaceSide, bridgeName, err)
	}
	if err := client.setPortUp(ctx, linkPair.NamespaceSide); err != nil {
		return fmt.Errorf("failed to set Grout port up: %w", err)
	}

	if params.HostMaster == nil {
		return nil
	}
	hostTap, err := netlink.LinkByName(linkPair.HostSide)
	if err != nil {
		return fmt.Errorf("host TAP %s not found after move: %w", linkPair.HostSide, err)
	}
	if err := hostnetwork.SetupHostMaster(ctx, ovsClient, params, hostTap); err != nil {
		return fmt.Errorf("failed to set up host master: %w", err)
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

// setupL2VNIBridge creates the VXLAN interface and the bridge of an L2VNI, in
// the L2VNI's VRF, and returns the bridge name.
func setupL2VNIBridge(ctx context.Context, client *Client, params hostnetwork.L2VNIParams) (string, error) {
	vrf := bridgeVRF(params)
	if params.VRF != "" {
		if err := client.ensureVRF(ctx, vrf); err != nil {
			return "", fmt.Errorf("failed to create VRF %s: %w", vrf, err)
		}
	}

	vtepIP, _, err := net.ParseCIDR(params.VTEPIP)
	if err != nil {
		return "", fmt.Errorf("failed to parse VTEP IP %q: %w", params.VTEPIP, err)
	}

	vxlanName := vxlanNameFromVNI(params.VNI)
	if err := client.ensureVXLAN(ctx, vxlanName, vtepIP.String(), "", params.VNI,
		ptr.Deref(params.VXLanPort, defaultVXLanPort)); err != nil {
		return "", fmt.Errorf("failed to create VXLAN: %w", err)
	}

	bridgeName := hostnetwork.BridgeName(params.VNI)
	if err := client.ensureBridge(ctx, bridgeName, vrf); err != nil {
		return "", fmt.Errorf("failed to create bridge %s: %w", bridgeName, err)
	}
	if err := client.ensureBridgeMember(ctx, "vxlan", bridgeName, vxlanName); err != nil {
		return "", fmt.Errorf("failed to attach VXLAN %s to bridge %s: %w", vxlanName, bridgeName, err)
	}
	// The bridge learns its neighbors by snooping, and grout ages them on its
	// own: make sure it does so at the same pace as the kernel datapath.
	if err := client.ensureNeighborAgeing(ctx); err != nil {
		return "", fmt.Errorf("failed to age neighbors like the kernel: %w", err)
	}
	return bridgeName, nil
}

func bridgeVRF(params hostnetwork.L2VNIParams) string {
	if params.VRF != "" {
		return params.VRF
	}
	return defaultVRFName
}
