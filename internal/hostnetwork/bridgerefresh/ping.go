// SPDX-License-Identifier:Apache-2.0

package bridgerefresh

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"syscall"

	"github.com/openperouter/openperouter/internal/ipfamily"
	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

// sendPing sends an ICMP ping to the target IP via the bridge interface.
// This refreshes the neighbor entry, preventing EVPN Type-2 route withdrawal and
// also ensures stale entries are garbage collected.
// Supports both IPv4 (ICMP Echo) and IPv6 (ICMPv6 Echo Request).
func (r *BridgeRefresher) sendPing(targetIP net.IP) error {
	network := "ip6:ipv6-icmp"
	msgType := icmp.Type(ipv6.ICMPTypeEchoRequest)
	if ipfamily.ForAddress(targetIP) == ipfamily.IPv4 {
		network = "ip4:icmp"
		msgType = ipv4.ICMPTypeEcho
	}

	dialer := net.Dialer{
		Control: func(network, address string, c syscall.RawConn) error {
			var opErr error
			if err := c.Control(func(fd uintptr) {
				opErr = syscall.SetsockoptString(int(fd), syscall.SOL_SOCKET, syscall.SO_BINDTODEVICE, r.bridgeName)
			}); err != nil {
				return err
			}
			return opErr
		},
	}

	conn, err := dialer.DialContext(context.Background(), network, targetIP.String())
	if err != nil {
		return fmt.Errorf("failed to create ICMP connection to %s on %s: %w", targetIP, r.bridgeName, err)
	}
	defer func() {
		if err := conn.Close(); err != nil {
			slog.Error("failed to close ICMP connection", "ip", targetIP, "bridge", r.bridgeName, "error", err)
		}
	}()

	msg := icmp.Message{
		Type: msgType,
		Code: 0,
		Body: &icmp.Echo{
			ID:   os.Getpid() & 0xffff,
			Seq:  1,
			Data: []byte("openperouter"),
		},
	}

	msgBytes, err := msg.Marshal(nil)
	if err != nil {
		return fmt.Errorf("failed to marshal ICMP message: %w", err)
	}

	if _, err := conn.Write(msgBytes); err != nil {
		return fmt.Errorf("ping to %s via %s failed: %w", targetIP, r.bridgeName, err)
	}

	return nil
}
