// SPDX-License-Identifier:Apache-2.0

package hostnetwork

import (
	"fmt"
	"net/netip"
)

// SRv6Isolation describes underlay exceptions for the namespace encapsulation source.
// Matching source/destination selectors do not authenticate packet origin.
type SRv6Isolation struct {
	Source                string
	UnderlayTable         uint32
	RemoteLocatorPrefixes []string
}

// VRFTableOwnershipError prevents an isolation lookup from targeting a tenant table.
type VRFTableOwnershipError struct {
	Table uint32
	VRF   string
}

func (e *VRFTableOwnershipError) Error() string {
	return fmt.Sprintf("underlayTable %d belongs to tenant %s", e.Table, e.VRF)
}

func (s SRv6Isolation) Validate() error {
	source, err := netip.ParseAddr(s.Source)
	if err != nil {
		return fmt.Errorf("encapsulationSource must be a unicast IPv6 address")
	}
	if !validSRv6EncapsulationSource(source) {
		return fmt.Errorf("encapsulationSource must be a unicast IPv6 address")
	}
	switch s.UnderlayTable {
	case 0, 253, 255:
		return fmt.Errorf("underlayTable must not be unspecified, default or local")
	}
	if len(s.RemoteLocatorPrefixes) == 0 || len(s.RemoteLocatorPrefixes) > 64 {
		return fmt.Errorf("remoteLocatorPrefixes must contain 1 to 64 prefixes")
	}
	seen := map[netip.Prefix]struct{}{}
	for _, value := range s.RemoteLocatorPrefixes {
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			return fmt.Errorf("remote locator %q must be a canonical non-default unicast IPv6 prefix", value)
		}
		if !validSRv6RemoteLocator(prefix) {
			return fmt.Errorf("remote locator %q must be a canonical non-default unicast IPv6 prefix", value)
		}
		if _, ok := seen[prefix]; ok {
			return fmt.Errorf("duplicate remote locator %q", value)
		}
		seen[prefix] = struct{}{}
	}
	return nil
}

func validSRv6EncapsulationSource(source netip.Addr) bool {
	if !source.Is6() {
		return false
	}
	if source.Is4In6() {
		return false
	}
	if !source.IsGlobalUnicast() {
		return false
	}
	return source.Zone() == ""
}

func validSRv6RemoteLocator(prefix netip.Prefix) bool {
	if !prefix.Addr().Is6() {
		return false
	}
	if prefix.Addr().Is4In6() {
		return false
	}
	if prefix.Bits() == 0 {
		return false
	}
	if prefix != prefix.Masked() {
		return false
	}
	if prefix.Overlaps(netip.MustParsePrefix("fe80::/10")) {
		return false
	}
	if prefix.Overlaps(netip.MustParsePrefix("ff00::/8")) {
		return false
	}
	return prefix.Addr().IsGlobalUnicast()
}
