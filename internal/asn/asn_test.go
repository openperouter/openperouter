// SPDX-License-Identifier:Apache-2.0

package asn

import (
	"testing"
)

func TestPeerASN(t *testing.T) {
	tcs := []struct {
		name             string
		number           uint32
		fn               optionFunction
		expectedString   string
		referenceASN     uint32
		expectedExternal bool
		expectedEqual    bool
	}{
		{
			name:             "numbered",
			number:           10,
			fn:               nil,
			expectedString:   "10",
			referenceASN:     11,
			expectedExternal: true,
			expectedEqual:    false,
		},
		{
			name:             "numbered same ASN",
			number:           10,
			fn:               nil,
			expectedString:   "10",
			referenceASN:     10,
			expectedExternal: false,
			expectedEqual:    true,
		},
		{
			name:             "numbered (internal ignored)",
			number:           10,
			fn:               WithInternal(),
			expectedString:   "10",
			referenceASN:     11,
			expectedExternal: true,
			expectedEqual:    false,
		},
		{
			name:             "unnumbered external",
			number:           0,
			fn:               nil,
			expectedString:   "external",
			referenceASN:     11,
			expectedExternal: true,
			expectedEqual:    false,
		},
		{
			name:             "unnumbered internal",
			number:           0,
			fn:               WithInternal(),
			expectedString:   "internal",
			referenceASN:     11,
			expectedExternal: false,
			expectedEqual:    false,
		},
	}

	for _, tc := range tcs {
		peerASN := Parse(tc.number, tc.fn)
		if peerASN.String() != tc.expectedString {
			t.Fatalf("%s: String(): %q does not match %q",
				tc.name, peerASN.String(), tc.expectedString)
		}
		if peerASN.IsExternalTo(tc.referenceASN) != tc.expectedExternal {
			t.Fatalf("%s: IsExternal(): %t does not match %t",
				tc.name, peerASN.IsExternalTo(tc.referenceASN), tc.expectedExternal)
		}
		if peerASN.Equal(Parse(tc.referenceASN)) != tc.expectedEqual {
			t.Fatalf("%s: Equal(): %t does not match %t",
				tc.name, peerASN.Equal(Parse(tc.referenceASN)), tc.expectedEqual)
		}
	}
}
