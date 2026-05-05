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
// If the provided number is larger than 0, it creates a numeric ASN.
// Otherwise, it creates a type-based ASN with valid values "external", "internal" or "" (alias for "external").
func NewPeerASN(number uint32, t string) (PeerASN, error) {
	asType := typeNumber
	if number > 0 {
		return PeerASN{
			asType: asType,
			number: number,
		}, nil
	}

	switch t {
	case "internal":
		asType = typeInternal
	case "external", "":
		asType = typeExternal
	default:
		return PeerASN{}, fmt.Errorf("invalid PeerASN type %q, must be 'internal', 'external' or ''", t)
	}
	return PeerASN{
		asType: asType,
	}, nil
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
