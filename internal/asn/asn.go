// SPDX-License-Identifier:Apache-2.0

package asn

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

	externalString = "external"
	internalString = "internal"
)

// Parse creates an ASN from the provided number if number is greater than 0.
// If number equals 0, the ASN will be parsed as external BGP, unless option WithInternal specifies otherwise.
func Parse(number uint32, optionFunctions ...optionFunction) PeerASN {
	var settings options
	for _, optionFunc := range optionFunctions {
		if optionFunc != nil {
			optionFunc(&settings)
		}
	}

	if number > 0 {
		return PeerASN{
			asType: typeNumber,
			number: number,
		}
	}

	asType := typeExternal
	if settings.isInternal {
		asType = typeInternal
	}
	return PeerASN{asType: asType}
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
		return externalString
	}
	if pa.asType == typeInternal {
		return internalString
	}
	return fmt.Sprintf("%d", pa.number)
}

// Equal determines if the current and the provided PeerASN are the same.
// This method is needed for cmp.Equal comparisons in unit tests.
func (pa PeerASN) Equal(pb PeerASN) bool {
	return pa.asType == pb.asType && pa.number == pb.number
}

type options struct {
	isInternal bool
}

type optionFunction func(opt *options)

// WithInternal requests that the ASN be of type "internal" if its ASN number is 0.
func WithInternal() optionFunction {
	return func(opt *options) {
		opt.isInternal = true
	}
}
