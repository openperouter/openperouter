// SPDX-License-Identifier:Apache-2.0

package sysctl

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/openperouter/openperouter/internal/netnamespace"
	"github.com/vishvananda/netns"
)

// Sysctl represents a sysctl setting to be enabled.
type Sysctl struct {
	Path               string // The sysctl path under /proc/sys/
	Description        string // Human-readable description for logging
	Value              string // Value to configure
	UnsupportedWarning string // If non-empty, log this warning and skip instead of failing when the sysctl is not supported
}

// EnsureInNamespace enables the given sysctls in the target namespace.
// Each sysctl is checked and only written if not already set to "1".
func EnsureInNamespace(namespace string, sysctls ...Sysctl) error {
	ns, err := netns.GetFromPath(namespace)
	if err != nil {
		return fmt.Errorf("failed to get network namespace %s: %w", namespace, err)
	}
	defer func() {
		if err := ns.Close(); err != nil {
			slog.Error("Ensure: failed to close namespace", "error", err, "namespace", namespace)
		}
	}()

	err = netnamespace.In(ns, func() error {
		return Ensure(sysctls...)
	})
	if err != nil {
		return fmt.Errorf("failed to ensure sysctls at network namespace %q: %w", namespace, err)
	}
	return nil
}

// Ensure enables the given sysctls.
// Each sysctl is checked and only written if not already set to "1".
func Ensure(sysctls ...Sysctl) error {
	for _, s := range sysctls {
		if err := ensureSysctl(s); err != nil {
			return err
		}
	}
	return nil
}

// IPv4Forwarding returns the sysctl definition for enabling IPv4 forwarding.
func IPv4Forwarding() Sysctl {
	return Sysctl{
		Path:        "net/ipv4/conf/all/forwarding",
		Description: "IPv4 forwarding",
		Value:       "1",
	}
}

// IPv6Forwarding returns the sysctl definition for enabling IPv6 forwarding.
func IPv6Forwarding() Sysctl {
	return Sysctl{
		Path:        "net/ipv6/conf/all/forwarding",
		Description: "IPv6 forwarding",
		Value:       "1",
	}
}

// ArpAcceptAll returns the sysctl definition for enabling arp_accept on all
// running interfaces. Enabling arp_accept allows the kernel to create neighbor entries
// from received Gratuitous ARP packets, which is critical for fast EVPN MAC/IP route
// advertisement during VM migrations.
func ArpAcceptAll() Sysctl {
	return Sysctl{
		Path:        "net/ipv4/conf/all/arp_accept",
		Description: "arp_accept on all interfaces",
		Value:       "1",
	}
}

// ArpAcceptDefault returns the sysctl definition for enabling arp_accept on
// newly created interfaces. This ensures that any new interface will inherit the
// arp_accept setting.
func ArpAcceptDefault() Sysctl {
	return Sysctl{
		Path:        "net/ipv4/conf/default/arp_accept",
		Description: "arp_accept on new interfaces",
		Value:       "1",
	}
}

// AcceptUntrackedNAAll returns the sysctl definition for enabling accept_untracked_na.
// This is the IPv6 equivalent of arp_accept - it allows the kernel to create neighbor entries
// from received unsolicited Neighbor Advertisement packets, which is critical for fast EVPN
// MAC/IP route advertisement during VM migrations with IPv6.
// Note: This sysctl is only available on kernels >= 5.18.
func AcceptUntrackedNAAll() Sysctl {
	return Sysctl{
		Path:               "net/ipv6/conf/all/accept_untracked_na",
		Description:        "accept_untracked_na on all interfaces",
		Value:              "1",
		UnsupportedWarning: "Check if kernel is >= 5.18, layer 2 traffic might be impacted / mac learning might be slower",
	}
}

// AcceptUntrackedNADefault returns the sysctl definition for enabling accept_untracked_na on
// newly created interfaces. This ensures that any new interface will inherit the setting.
// This is the IPv6 equivalent of arp_accept - it allows the kernel to create neighbor entries
// from received unsolicited Neighbor Advertisement packets, which is critical for fast EVPN
// MAC/IP route advertisement during VM migrations with IPv6.
// Note: This sysctl is only available on kernels >= 5.18.
func AcceptUntrackedNADefault() Sysctl {
	return Sysctl{
		Path:               "net/ipv6/conf/default/accept_untracked_na",
		Description:        "accept_untracked_na on new interfaces",
		Value:              "1",
		UnsupportedWarning: "Check if kernel is >= 5.18, layer 2 traffic might be impacted / mac learning might be slower",
	}
}

// EnableVRFStrictMode sets the sysctl definition for enabling VRF strict mode.
// The "strict mode" imposes that each VRF can be associated to a routing table only if such routing table is not
// already in use by any other VRF. The setting is not available if no VRFs are created and according to
// https://onvox.net/2024/12/16/srv6-frr/ should be set each time after adding a VRF for safe measure. If you see
// rejected BGP routes in the table (`B>r`), this is most likely due to a problem with these setting not being set
// when FRR wanted to install the routes.
// Reference: https://man7.org/linux/man-pages/man8/ip-route.8.html
// Reference: https://lwn.net/Articles/823946/
func EnableVRFStrictMode() Sysctl {
	return Sysctl{
		Path:        "net/vrf/strict_mode",
		Description: "configure VRF strict mode",
		Value:       "1",
	}
}

// Seg6MakeFlowLabel sets the sysctl for SRv6 flow label handling to use seg6_make_flowlabel().
// When set to 1, the kernel will compute the flow label using seg6_make_flowlabel().
// Reference: https://www.kernel.org/doc/html/v6.1/networking/seg6-sysctl.html
func Seg6MakeFlowLabel() Sysctl {
	return Sysctl{
		Path:        "net/ipv6/seg6_flowlabel",
		Description: "compute the flowlabel using seg6_make_flowlabel()",
		Value:       "1",
	}
}

// EnableSeg6All sets the sysctl definition for enabling SRv6 on all interfaces.
// This enables Segment Routing over IPv6 (SRv6) functionality on all network interfaces,
// allowing them to process and forward packets with SRv6 segment routing headers.
// This is required for the kernel to accept and process SRv6 encapsulated traffic.
// Reference: https://docs.kernel.org/networking/seg6-sysctl.html
func EnableSeg6All() Sysctl {
	return Sysctl{
		Path:        "net/ipv6/conf/all/seg6_enabled",
		Description: "enable seg6 on all interfaces",
		Value:       "1",
	}
}

// ensureSysctl reads the sysctl at the given path and writes the desired value if it differs
// from the current one. If the proc file does not exist and UnsupportedWarning is set, the
// sysctl is silently skipped with a warning instead of returning an error.
func ensureSysctl(s Sysctl) error {
	path := "/proc/sys/" + s.Path
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) && s.UnsupportedWarning != "" {
		slog.Warn("skipping unsupported sysctl", "path", s.Path, "description", s.Description, "warning", s.UnsupportedWarning)
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", path, err)
	}

	currentValue := strings.TrimSpace(string(data))
	if currentValue == s.Value {
		return nil
	}

	if err := os.WriteFile(path, []byte(s.Value), 0644); err != nil {
		return fmt.Errorf("failed to write to %s: %w", path, err)
	}
	slog.Info("sysctl enabled", "path", s.Path, "description", s.Description)

	return nil
}
