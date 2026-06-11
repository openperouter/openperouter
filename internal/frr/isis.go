// SPDX-License-Identifier:Apache-2.0

package frr

import (
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/openperouter/openperouter/api/v1alpha1"
)

// ISISInterface holds the internal representation of an interface's ISIS configuration.
type ISISInterface struct {
	Name      string
	IPv4      bool
	IPv6      bool
	IsPassive bool
}

// ISISAreaID is the area ID part of an ISIS net address.
// Considering the GOSIP-specified 6-byte length of the System ID field and the 1-byte NSEL field, the Area ID may
// vary between 1 and 13 bytes. The current implementation strives to simplify by working with fixed size 10 byte
// Net addresses. This restriction should be changed in the future.
type ISISAreaID struct {
	AFI             byte // AFI identifies the Net address family. Usually 49 for the local address domain.
	AreaInformation [2]byte
}

// ISISSystemID holds the system ID part of an ISISnet address.
type ISISSystemID [6]byte

// IncrementSystemID takes an ISIS SystemID and an offset and returns the result of the sum of both.
func IncrementSystemID(si ISISSystemID, offset int) (ISISSystemID, error) {
	carry := offset
	for i := range 6 {
		if carry == 0 {
			break
		}
		idx := len(si) - 1 - i
		res := int(si[idx]) + carry
		carry = res / 256
		si[idx] = byte(res % 256)
	}
	if carry > 0 {
		return si, fmt.Errorf("overflow while incrementing SystemID %s with offset %d", si, offset)
	}
	return si, nil
}

// ISISNet stores the ISIS NET (Network Entity Title) address, a special form of NSAP (Network Service Access Point).
// We use the simplified NSAP format with a fixed AreaID length of 3 bytes and a 6 byte SystemID in compliance with the
// U.S. GOSIP version 2.0 for a total of 10 bytes. These addresses are sufficient for IP routing purposes. Future
// implementations should consider expanding this to allow for a more flexible, 20 byte long representation.
// Source: Cisco Press IS-IS Network Design Solutions, chapter: NSAP Format
type ISISNet struct {
	Area     ISISAreaID
	SystemID [6]byte // SystemID is a unique identifier for this system and must be the same in multi-area setups.
	Nsel     byte
}

// ParseISISNet takes a string representation of an ISIS net address and parses it to an ISISNet object.
func ParseISISNet(net v1alpha1.ISISNet) (ISISNet, error) {
	var in ISISNet

	fields := strings.Split(string(net), ".")
	if len(fields) != 6 {
		return in, fmt.Errorf("invalid ISIS net address %q, 10 byte representation is expected", net)
	}
	afi, err := hex.DecodeString(fields[0])
	if err != nil || len(afi) != 1 {
		return in, fmt.Errorf("invalid ISIS net address %q, could not parse AFI", net)
	}
	areaInfo, err := hex.DecodeString(fields[1])
	if err != nil || len(areaInfo) != 2 {
		return in, fmt.Errorf("invalid ISIS net address %q, could not parse areaID", net)
	}
	systemID, err := hex.DecodeString(strings.Join(fields[2:5], ""))
	if err != nil || len(systemID) != 6 {
		return in, fmt.Errorf("invalid ISIS net address %q, could not parse systemID", net)
	}
	nsel, err := hex.DecodeString(fields[5])
	if err != nil || len(nsel) != 1 {
		return in, fmt.Errorf("invalid ISIS net address %q, could not parse NSEL", net)
	}

	in.Area.AFI = afi[0]
	in.Area.AreaInformation = [2]byte(areaInfo)
	in.SystemID = [6]byte(systemID)
	in.Nsel = nsel[0]

	return in, nil
}

// MustParseISISNet is the same as ParseISISNet, but it panics on error.
func MustParseISISNet(net v1alpha1.ISISNet) ISISNet {
	in, err := ParseISISNet(net)
	if err != nil {
		panic(err)
	}
	return in
}

// String converts ISISNet into its string representation, e.g. 49.0001.0002.0003.0004.00.
func (in ISISNet) String() string {
	return fmt.Sprintf("%s.%s.%s.%s.%s.%s",
		hex.EncodeToString([]byte{in.Area.AFI}),
		hex.EncodeToString(in.Area.AreaInformation[:]),
		hex.EncodeToString(in.SystemID[:2]),
		hex.EncodeToString(in.SystemID[2:4]),
		hex.EncodeToString(in.SystemID[4:6]),
		hex.EncodeToString([]byte{in.Nsel}),
	)
}
