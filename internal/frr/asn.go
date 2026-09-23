// SPDX-License-Identifier:Apache-2.0

package frr

import (
	"fmt"
)

type asnType int

// PeerASN is the representation of a peer ASN. It can either be numeric, "external" or "internal".
type PeerASN struct {
	asType asnType
	number int64
}

const (
	typeNumber asnType = iota
	typeExternal
	typeInternal
)

// NewPeerASN creates a PeerASN from the provided ASN number and type string pointers.
// If number is non-nil and greater than 0, it creates a numeric ASN.
// Otherwise, it creates a type-based ASN with valid values "external", "internal" or "" (alias for "external").
// Nil pointers are treated as zero-value (0 and "" respectively).
func NewPeerASN(number *int64, t *string) (PeerASN, error) {
	n := int64(0)
	if number != nil {
		n = *number
	}
	peerType := ""
	if t != nil {
		peerType = *t
	}

	asType := typeNumber
	if n > 0 {
		return PeerASN{
			asType: asType,
			number: n,
		}, nil
	}

	switch peerType {
	case "Internal":
		asType = typeInternal
	case "External", "":
		asType = typeExternal
	default:
		return PeerASN{}, fmt.Errorf("invalid PeerASN type %q, must be 'Internal', 'External' or ''", peerType)
	}
	return PeerASN{
		asType: asType,
	}, nil
}

// IsExternalTo returns true if PeerASN's type is "external" or if the provided ASN number differs from the peer's.
func (pa PeerASN) IsExternalTo(a int64) bool {
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
