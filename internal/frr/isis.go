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
	Name string
	IPv4 bool
	IPv6 bool
}

// ISISArea is the area part of an ISIS net address.
type ISISArea struct {
	AFI    byte
	AreaID [2]byte
}

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

// ISISNet stores the ISIS net address.
type ISISNet struct {
	Area     ISISArea
	SystemID [6]byte
	Nsel     byte
}

// ParseISISNet takes a string representation of an ISIS net address and parses it to an ISISNet object.
func ParseISISNet(net v1alpha1.ISISNet) (ISISNet, error) {
	var in ISISNet

	fields := strings.Split(string(net), ".")
	if len(fields) != 6 {
		return in, fmt.Errorf("invalid ISIS net address %q", net)
	}
	afi, err := hex.DecodeString(fields[0])
	if err != nil || len(afi) != 1 {
		return in, fmt.Errorf("invalid ISIS net address %q, could not parse AFI", net)
	}
	areaID, err := hex.DecodeString(fields[1])
	if err != nil || len(areaID) != 2 {
		return in, fmt.Errorf("invalid ISIS net address %q, could not parse areaID", net)
	}
	systemID, err := hex.DecodeString(strings.Join(fields[2:5], ""))
	if err != nil || len(systemID) != 6 {
		return in, fmt.Errorf("invalid ISIS net address %q, could not parse systemID", net)
	}
	nsel, err := hex.DecodeString(fields[5])
	if err != nil || len(nsel) != 1 {
		return in, fmt.Errorf("invalid ISIS net address %q, could not parse AFI", net)
	}

	in.Area.AFI = afi[0]
	in.Area.AreaID = [2]byte(areaID)
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
		hex.EncodeToString(in.Area.AreaID[:]),
		hex.EncodeToString(in.SystemID[:2]),
		hex.EncodeToString(in.SystemID[2:4]),
		hex.EncodeToString(in.SystemID[4:6]),
		hex.EncodeToString([]byte{in.Nsel}),
	)
}
