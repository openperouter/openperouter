// SPDX-License-Identifier:Apache-2.0

package hostnetwork

import (
	"fmt"
	"net"

	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netlink/nl"
	"golang.org/x/sys/unix"
)

// setSRv6TunnelSource sets the namespace's actual encapsulation source, rather
// than relying on kernel source selection or FRR's loopback fallback.
func setSRv6TunnelSource(source net.IP) error {
	family, err := netlink.GenlFamilyGet("SEG6")
	if err != nil {
		return fmt.Errorf("resolve SEG6 generic netlink family: %w", err)
	}
	req := nl.NewNetlinkRequest(int(family.ID), unix.NLM_F_REQUEST|unix.NLM_F_ACK)
	// SEG6_CMD_SET_TUNSRC, SEG6_GENL_VERSION, SEG6_ATTR_DST.
	req.AddRawData(append([]byte{3, 1, 0, 0}, nl.NewRtAttr(1, source.To16()).Serialize()...))
	if _, err := req.Execute(unix.NETLINK_GENERIC, 0); err != nil {
		return fmt.Errorf("set SRv6 tunnel source: %w", err)
	}
	return nil
}

func getSRv6TunnelSource() (net.IP, error) {
	family, err := netlink.GenlFamilyGet("SEG6")
	if err != nil {
		return nil, fmt.Errorf("resolve SEG6 generic netlink family: %w", err)
	}
	req := nl.NewNetlinkRequest(int(family.ID), unix.NLM_F_REQUEST)
	req.AddRawData([]byte{4, 1, 0, 0})
	messages, err := req.Execute(unix.NETLINK_GENERIC, family.ID)
	if err != nil {
		return nil, fmt.Errorf("request SRv6 tunnel source: %w", err)
	}
	for _, message := range messages {
		if len(message) < 4 {
			return nil, fmt.Errorf("short SEG6 response")
		}
		attrs, err := nl.ParseRouteAttr(message[4:])
		if err != nil {
			return nil, fmt.Errorf("decode SRv6 tunnel source attributes: %w", err)
		}
		for _, attr := range attrs {
			if attr.Attr.Type == 1 && len(attr.Value) == 16 {
				return net.IP(attr.Value), nil
			}
		}
	}
	return nil, fmt.Errorf("SEG6 source missing from netlink response")
}
