// SPDX-License-Identifier:Apache-2.0

package frr

import (
	"testing"
)

func TestPeerASN(t *testing.T) {
	tcs := []struct {
		name             string
		number           uint32
		asnType          string
		expectedString   string
		referenceASN     uint32
		expectedExternal bool
		expectedEqual    bool
	}{
		{
			name:             "numbered",
			number:           10,
			asnType:          "",
			expectedString:   "10",
			referenceASN:     11,
			expectedExternal: true,
			expectedEqual:    false,
		},
		{
			name:             "numbered same ASN",
			number:           10,
			asnType:          "",
			expectedString:   "10",
			referenceASN:     10,
			expectedExternal: false,
			expectedEqual:    true,
		},
		{
			name:             "numbered (internal ignored)",
			number:           10,
			asnType:          "internal",
			expectedString:   "10",
			referenceASN:     11,
			expectedExternal: true,
			expectedEqual:    false,
		},
		{
			name:             "unnumbered external",
			number:           0,
			asnType:          "",
			expectedString:   "external",
			referenceASN:     11,
			expectedExternal: true,
			expectedEqual:    false,
		},
		{
			name:             "unnumbered internal",
			number:           0,
			asnType:          "internal",
			expectedString:   "internal",
			referenceASN:     11,
			expectedExternal: false,
			expectedEqual:    false,
		},
	}

	for _, tc := range tcs {
		peerASN := NewPeerASN(tc.number, tc.asnType)
		if peerASN.String() != tc.expectedString {
			t.Fatalf("%s: String(): %q does not match %q",
				tc.name, peerASN.String(), tc.expectedString)
		}
		if peerASN.IsExternalTo(tc.referenceASN) != tc.expectedExternal {
			t.Fatalf("%s: IsExternal(): %t does not match %t",
				tc.name, peerASN.IsExternalTo(tc.referenceASN), tc.expectedExternal)
		}
		if peerASN.Equal(NewPeerASNFromNumber(tc.referenceASN)) != tc.expectedEqual {
			t.Fatalf("%s: Equal(): %t does not match %t",
				tc.name, peerASN.Equal(NewPeerASNFromNumber(tc.referenceASN)), tc.expectedEqual)
		}
	}
}
