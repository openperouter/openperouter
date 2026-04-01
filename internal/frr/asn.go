// SPDX-License-Identifier:Apache-2.0

package frr

import (
	"fmt"
)

type asnType int

// PeerASN is the representation of a peer ASN. It can either be numeric, "external" or "internal".
type PeerASN struct {
	asType asnType
	number uint32
}

const (
	typeNumber asnType = iota
	typeExternal
	typeInternal
)

// NewPeerASN creates a PeerASN from the provided ASN number and type string.
// If the provided number is larger then 0, it creates a numeric ASN.
// Otherwise, it creates a type-based ASN (defaults to external unless type is "internal").
func NewPeerASN(number uint32, t string) PeerASN {
	if number > 0 {
		return PeerASN{
			asType: typeNumber,
			number: number,
		}
	}
	asType := typeExternal
	if t == "internal" {
		asType = typeInternal
	}
	return PeerASN{
		asType: asType,
	}
}

// NewPeerASNFromNumber creates an ASN from the provided ASN number.
func NewPeerASNFromNumber(number uint32) PeerASN {
	return NewPeerASN(number, "")
}

// NewPeerASNFromType creates an ASN from the provided type string. The type defaults to external, unless the
// type string equals "internal".
func NewPeerASNFromType(t string) PeerASN {
	return NewPeerASN(0, t)
}

// IsExternalTo returns true if PeerASN's type is "external" or if the provided ASN number differs from the peer's.
func (pa PeerASN) IsExternalTo(a uint32) bool {
	if pa.asType == typeExternal {
		return true
	}
	if pa.asType == typeInternal {
		return false
	}
	return a != pa.number
}

// String returns a string representation of the PeerASN.
func (pa PeerASN) String() string {
	if pa.asType == typeExternal {
		return "external"
	}
	if pa.asType == typeInternal {
		return "internal"
	}
	return fmt.Sprintf("%d", pa.number)
}

// Equal determines if the current and the provided PeerASN are the same.
// This method is needed for cmp.Equal comparisons in unit tests.
func (pa PeerASN) Equal(pb PeerASN) bool {
	return pa.asType == pb.asType && pa.number == pb.number
}
