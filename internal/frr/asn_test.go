// SPDX-License-Identifier:Apache-2.0

package frr

import (
	"testing"
)

func TestPeerASNToString(t *testing.T) {
	tcs := []struct {
		name          string
		peerASNNumber int64
		peerASNType   string
		wantString    string // String representation of peerASN.
	}{
		{
			name:          "numbered",
			peerASNNumber: 10,
			peerASNType:   "",
			wantString:    "10",
		},
		{
			name:          "numbered (internal ignored)",
			peerASNNumber: 10,
			peerASNType:   "internal",
			wantString:    "10",
		},
		{
			name:          "unnumbered defaults to external",
			peerASNNumber: 0,
			peerASNType:   "",
			wantString:    "external",
		},
		{
			name:          "unnumbered external",
			peerASNNumber: 0,
			peerASNType:   "external",
			wantString:    "external",
		},
		{
			name:          "unnumbered internal",
			peerASNNumber: 0,
			peerASNType:   "internal",
			wantString:    "internal",
		},
	}

	for _, tc := range tcs {
		peerASN, err := NewPeerASN(tc.peerASNNumber, tc.peerASNType)
		if err != nil {
			t.Fatalf("%s: unexpected error, %v", tc.name, err)
		}
		if peerASN.String() != tc.wantString {
			t.Fatalf("%s: String(): %q does not match %q",
				tc.name, peerASN.String(), tc.wantString)
		}
	}
}

func TestPeerASNEqual(t *testing.T) {
	tcs := []struct {
		name           string
		peerASNNumber  int64
		peerASNType    string
		otherASNNumber int64
		wantEqual      bool // Want peer and other ASN to be equal (test for Equal()).
	}{
		{
			name:           "numbered",
			peerASNNumber:  10,
			peerASNType:    "",
			otherASNNumber: 11,
			wantEqual:      false,
		},
		{
			name:           "numbered same ASN",
			peerASNNumber:  10,
			peerASNType:    "",
			otherASNNumber: 10,
			wantEqual:      true,
		},
		{
			name:           "numbered (internal ignored)",
			peerASNNumber:  10,
			peerASNType:    "internal",
			otherASNNumber: 11,
			wantEqual:      false,
		},
		{
			name:           "unnumbered defaults to external",
			peerASNNumber:  0,
			peerASNType:    "",
			otherASNNumber: 11,
			wantEqual:      false,
		},
		{
			name:           "unnumbered external",
			peerASNNumber:  0,
			peerASNType:    "external",
			otherASNNumber: 11,
			wantEqual:      false,
		},
		{
			name:           "unnumbered internal",
			peerASNNumber:  0,
			peerASNType:    "internal",
			otherASNNumber: 11,
			wantEqual:      false,
		},
	}

	for _, tc := range tcs {
		peerASN, err := NewPeerASN(tc.peerASNNumber, tc.peerASNType)
		if err != nil {
			t.Fatalf("%s: unexpected error, %v", tc.name, err)
		}
		if peerASN.Equal(mustNewPeerASNFromNumber(tc.otherASNNumber)) != tc.wantEqual {
			t.Fatalf("%s: Equal(): %t does not match %t",
				tc.name, peerASN.Equal(mustNewPeerASNFromNumber(tc.otherASNNumber)), tc.wantEqual)
		}
	}
}

func TestPeerASNIsExternalTo(t *testing.T) {
	tcs := []struct {
		name             string
		peerASNNumber    int64
		peerASNType      string
		otherASNNumber   int64
		wantIsExternalTo bool // Want peer and other ASN to be external to each other (test for IsExternalTo()).
	}{
		{
			name:             "numbered",
			peerASNNumber:    10,
			peerASNType:      "",
			otherASNNumber:   11,
			wantIsExternalTo: true,
		},
		{
			name:             "numbered same ASN",
			peerASNNumber:    10,
			peerASNType:      "",
			otherASNNumber:   10,
			wantIsExternalTo: false,
		},
		{
			name:             "numbered (internal ignored)",
			peerASNNumber:    10,
			peerASNType:      "internal",
			otherASNNumber:   11,
			wantIsExternalTo: true,
		},
		{
			name:             "unnumbered defaults to external",
			peerASNNumber:    0,
			peerASNType:      "",
			otherASNNumber:   11,
			wantIsExternalTo: true,
		},
		{
			name:             "unnumbered external",
			peerASNNumber:    0,
			peerASNType:      "external",
			otherASNNumber:   11,
			wantIsExternalTo: true,
		},
		{
			name:             "unnumbered internal",
			peerASNNumber:    0,
			peerASNType:      "internal",
			otherASNNumber:   11,
			wantIsExternalTo: false,
		},
	}

	for _, tc := range tcs {
		peerASN, err := NewPeerASN(tc.peerASNNumber, tc.peerASNType)
		if err != nil {
			t.Fatalf("%s: unexpected error, %v", tc.name, err)
		}
		if peerASN.IsExternalTo(tc.otherASNNumber) != tc.wantIsExternalTo {
			t.Fatalf("%s: IsExternal(): %t does not match %t",
				tc.name, peerASN.IsExternalTo(tc.otherASNNumber), tc.wantIsExternalTo)
		}
	}
}
