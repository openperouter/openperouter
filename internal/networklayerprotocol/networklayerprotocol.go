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
	VPN     SAFI = "vpn"
)

// NLP (NetworkLayerProtocol) represents a full address family plus subsequent address family, as defined in RFC4760 for
// BGP multiprotocol extensions.
type NLP struct {
	AFI        AFI
	SAFI       SAFI
	Properties NLPProperties
}

// AFI (Address Family Identifier) holds the Address Family identifier for BGP multiprotocol extensions.
type AFI string

// SAFI (Subsequent Address Family Identifier) holds the Subsequent Address Family identifier for BGP multiprotocol
// extensions.
type SAFI string

// NLPProperties holds the per-address-family rendering attributes of a
// neighbor (for example, whether the neighbor is a route reflector client in
// that network layer protocol).
type NLPProperties struct {
	RouteReflectorClient bool
}

// String returns a string representation of the NLP with AFI and SAFI separated by a single whitespace.
func (nlp NLP) String() string {
	return fmt.Sprintf("%s %s", nlp.AFI, nlp.SAFI)
}

// Matches returns true if AFI and SAFI properties matches.
func (nlp NLP) Matches(nlpToCompare NLP) bool {
	return nlp.AFI == nlpToCompare.AFI && nlp.SAFI == nlpToCompare.SAFI
}

func FindNLP(nlps []NLP, nlp NLP) *NLP {
	idx := slices.IndexFunc(nlps, nlp.Matches)
	if idx == -1 {
		return nil
	}
	return &nlps[idx]
}

// HasNLP returns true if the given network layer protocol can be found in the slice of network layer protocols.
func HasNLP(nlps []NLP, nlp NLP) bool {
	return FindNLP(nlps, nlp) != nil
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

// HasVPNFamily takes a slice of network layer protocols and an AFI and returns true if the vpn version of the
// family can be found in the slice (i.e. ipv4 vpn or ipv6 vpn). Only valid for ipv4 and ipv6, returns false
// for l2vpn.
func HasVPNFamily(nlps []NLP, afi AFI) bool {
	if afi == L2VPN {
		return false
	}
	return HasNLP(nlps, NLP{AFI: afi, SAFI: VPN})
}
