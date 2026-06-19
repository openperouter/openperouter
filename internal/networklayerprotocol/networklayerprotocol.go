// SPDX-License-Identifier:Apache-2.0

package networklayerprotocol

import (
	"fmt"
	"slices"
)

const (
	IPv4  AFI = "ipv4"
	IPv6  AFI = "ipv6"
	L2VPN AFI = "l2vpn"

	EVPN    SAFI = "evpn"
	Unicast SAFI = "unicast"
)

// NLP (NetworkLayerProtocol) represents a full address family plus subsequent address family, as defined in RFC4760 for
// BGP multiprotocol extensions.
type NLP struct {
	AFI  AFI
	SAFI SAFI
}

// AFI (Address Family Identifier) holds the Address Family identifier for BGP multiprotocol extensions.
type AFI string

// SAFI (Subsequent Address Family Identifier) holds the Subsequent Address Family identifier for BGP multiprotocol
// extensions.
type SAFI string

// String returns a string representation of the NLP with AFI and SAFI separated by a single whitespace.
func (nlp NLP) String() string {
	return fmt.Sprintf("%s %s", nlp.AFI, nlp.SAFI)
}

// HasNLP returns true if the given network layer protocol can be found in the slice of network layer protocols.
func HasNLP(nlps []NLP, nlp NLP) bool {
	return slices.Contains(nlps, nlp)
}

// HasUnicastFamily takes a slice of network layer protocols and an AFI and returns true if the unicast version of the
// family can be found in the slice (i.e. ipv4 unicast or ipv6 unicast). Only valid for ipv4 and ipv6, returns false
// for l2vpn.
func HasUnicastFamily(nlps []NLP, afi AFI) bool {
	if afi == L2VPN {
		return false
	}
	return HasNLP(nlps, NLP{AFI: afi, SAFI: Unicast})
}
