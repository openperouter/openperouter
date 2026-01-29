// SPDX-License-Identifier:Apache-2.0

package bridgerefresh

import (
	"fmt"
	"net"
	"net/netip"

	"github.com/mdlayher/arp"
	"github.com/vishvananda/netlink"
)

// sendARPProbe sends a unicast ARP request to refresh a neighbor entry.
// Using unicast to the known MAC is more efficient than broadcast
// and specifically targets the VM we want to keep in the neighbor table.
func (r *BridgeRefresher) sendARPProbe(targetIP net.IP, targetMAC net.HardwareAddr) error {
	if len(r.gatewayIPs) == 0 {
		return fmt.Errorf("no gateway IPs configured for ARP probes")
	}

	bridge, err := netlink.LinkByName(r.bridgeName)
	if err != nil {
		return fmt.Errorf("failed to get bridge %s: %w", r.bridgeName, err)
	}

	srcMAC := bridge.Attrs().HardwareAddr
	if len(srcMAC) == 0 {
		return fmt.Errorf("bridge %s has no hardware address", r.bridgeName)
	}

	// Use first IPv4 gateway as source IP
	srcIP := r.gatewayIPs[0]

	// Convert net.IP to netip.Addr for the arp package
	srcAddr, ok := netip.AddrFromSlice(srcIP.To4())
	if !ok {
		return fmt.Errorf("failed to convert source IP %s to netip.Addr", srcIP)
	}

	targetIP4 := targetIP.To4()
	if targetIP4 == nil {
		return fmt.Errorf("target IP %s is not IPv4", targetIP)
	}
	targetAddr, ok := netip.AddrFromSlice(targetIP4)
	if !ok {
		return fmt.Errorf("failed to convert target IP %s to netip.Addr", targetIP)
	}

	// Create ARP client on bridge interface
	ifi := &net.Interface{
		Index:        bridge.Attrs().Index,
		Name:         r.bridgeName,
		HardwareAddr: srcMAC,
		MTU:          bridge.Attrs().MTU,
		Flags:        net.FlagUp | net.FlagBroadcast | net.FlagMulticast,
	}

	client, err := arp.Dial(ifi)
	if err != nil {
		return fmt.Errorf("failed to create ARP client on %s: %w", r.bridgeName, err)
	}
	defer func() { _ = client.Close() }()

	// Build ARP request packet
	// Using unicast ARP to the known MAC address
	packet, err := arp.NewPacket(arp.OperationRequest, srcMAC, srcAddr, targetMAC, targetAddr)
	if err != nil {
		return fmt.Errorf("failed to create ARP packet: %w", err)
	}

	// Send unicast ARP to the target MAC
	if err := client.WriteTo(packet, targetMAC); err != nil {
		return fmt.Errorf("failed to send ARP probe to %s (%s): %w", targetIP, targetMAC, err)
	}

	return nil
}
