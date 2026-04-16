// SPDX-License-Identifier:Apache-2.0

package frr

import (
	"strings"
	"testing"

	"github.com/openperouter/openperouter/api/v1alpha1"
)

func TestParseISISNet(t *testing.T) {
	tests := []struct {
		name        string
		input       v1alpha1.ISISNet
		expectedErr string
	}{
		// Valid cases
		{
			name:        "valid basic net",
			input:       "49.0001.0000.0000.0001.00",
			expectedErr: "",
		},
		{
			name:        "valid net with different AFI",
			input:       "48.0001.1234.5678.9abc.00",
			expectedErr: "",
		},
		{
			name:        "valid net with all hex digits",
			input:       "49.ffff.abcd.ef01.2345.ff",
			expectedErr: "",
		},
		{
			name:        "valid net with uppercase hex",
			input:       "49.ABCD.FFFF.0000.1111.00",
			expectedErr: "",
		},
		{
			name:        "valid net with mixed case hex",
			input:       "49.AbCd.FfFf.0000.1111.00",
			expectedErr: "",
		},
		{
			name:        "valid net with non-zero nsel",
			input:       "49.0001.0000.0000.0001.01",
			expectedErr: "",
		},

		// Invalid number of fields
		{
			name:        "too few fields - 5 fields",
			input:       "49.0001.0000.0000.0001",
			expectedErr: "invalid ISIS net address",
		},
		{
			name:        "too few fields - 4 fields",
			input:       "49.0001.0000.0001",
			expectedErr: "invalid ISIS net address",
		},
		{
			name:        "too many fields - 7 fields",
			input:       "49.0001.0000.0000.0001.00.00",
			expectedErr: "invalid ISIS net address",
		},
		{
			name:        "empty string",
			input:       "",
			expectedErr: "invalid ISIS net address",
		},
		{
			name:        "single field",
			input:       "49",
			expectedErr: "invalid ISIS net address",
		},

		// Invalid AFI
		{
			name:        "AFI non-hex character",
			input:       "4g.0001.0000.0000.0001.00",
			expectedErr: "could not parse AFI",
		},
		{
			name:        "AFI too short (empty)",
			input:       ".0001.0000.0000.0001.00",
			expectedErr: "could not parse AFI",
		},
		{
			name:        "AFI too long - 3 hex digits",
			input:       "490.0001.0000.0000.0001.00",
			expectedErr: "could not parse AFI",
		},
		{
			name:        "AFI too long - 4 hex digits",
			input:       "4900.0001.0000.0000.0001.00",
			expectedErr: "could not parse AFI",
		},
		{
			name:        "AFI odd number of hex digits",
			input:       "4.0001.0000.0000.0001.00",
			expectedErr: "could not parse AFI",
		},

		// Invalid areaID
		{
			name:        "areaID non-hex character",
			input:       "49.000g.0000.0000.0001.00",
			expectedErr: "could not parse areaID",
		},
		{
			name:        "areaID too short (empty)",
			input:       "49..0000.0000.0001.00",
			expectedErr: "could not parse areaID",
		},
		{
			name:        "areaID too short - 1 hex digit",
			input:       "49.0.0000.0000.0001.00",
			expectedErr: "could not parse areaID",
		},
		{
			name:        "areaID too short - 3 hex digits",
			input:       "49.000.0000.0000.0001.00",
			expectedErr: "could not parse areaID",
		},
		{
			name:        "areaID too long - 5 hex digits",
			input:       "49.00001.0000.0000.0001.00",
			expectedErr: "could not parse areaID",
		},
		{
			name:        "areaID too long - 6 hex digits",
			input:       "49.000001.0000.0000.0001.00",
			expectedErr: "could not parse areaID",
		},

		// Invalid systemID
		{
			name:        "systemID non-hex character in first field",
			input:       "49.0001.000g.0000.0001.00",
			expectedErr: "could not parse systemID",
		},
		{
			name:        "systemID non-hex character in second field",
			input:       "49.0001.0000.000g.0001.00",
			expectedErr: "could not parse systemID",
		},
		{
			name:        "systemID non-hex character in third field",
			input:       "49.0001.0000.0000.000g.00",
			expectedErr: "could not parse systemID",
		},
		{
			name:        "systemID too short - empty fields",
			input:       "49.0001....00",
			expectedErr: "could not parse systemID",
		},
		{
			name:        "systemID too short - 5 hex digits total",
			input:       "49.0001.00.00.01.00",
			expectedErr: "could not parse systemID",
		},
		{
			name:        "systemID too short - 11 hex digits (odd)",
			input:       "49.0001.000.0000.0001.00",
			expectedErr: "could not parse systemID",
		},
		{
			name:        "systemID too long - 7 hex digits total",
			input:       "49.0001.00000.00.01.00",
			expectedErr: "could not parse systemID",
		},
		{
			name:        "systemID too long - 13 hex digits (odd)",
			input:       "49.0001.00000.0000.0001.00",
			expectedErr: "could not parse systemID",
		},
		{
			name:        "systemID too long - 14 hex digits",
			input:       "49.0001.0000.000000.0001.00",
			expectedErr: "could not parse systemID",
		},

		// Invalid nsel
		{
			name:        "nsel non-hex character",
			input:       "49.0001.0000.0000.0001.0g",
			expectedErr: "could not parse AFI",
		},
		{
			name:        "nsel too short (empty)",
			input:       "49.0001.0000.0000.0001.",
			expectedErr: "could not parse AFI",
		},
		{
			name:        "nsel too short - 1 hex digit (odd)",
			input:       "49.0001.0000.0000.0001.0",
			expectedErr: "could not parse AFI",
		},
		{
			name:        "nsel too long - 3 hex digits",
			input:       "49.0001.0000.0000.0001.000",
			expectedErr: "could not parse AFI",
		},
		{
			name:        "nsel too long - 4 hex digits",
			input:       "49.0001.0000.0000.0001.0000",
			expectedErr: "could not parse AFI",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseISISNet(tt.input)

			// Check error expectations
			if tt.expectedErr == "" {
				if err != nil {
					t.Errorf("ParseISISNet(%q) unexpected error: %v", tt.input, err)
					return
				}

				// For valid inputs, test round-trip conversion
				// Convert to intermediate representation and back
				output := result.String()

				// Normalize both strings to lowercase for comparison
				normalizedInput := strings.ToLower(string(tt.input))
				normalizedOutput := strings.ToLower(output)

				if normalizedInput != normalizedOutput {
					t.Errorf("Round-trip failed:\n  input:  %q\n  output: %q\n  parsed: %+v",
						tt.input, output, result)
				}
			} else {
				if err == nil {
					t.Errorf("ParseISISNet(%q) expected error containing %q, got nil", tt.input, tt.expectedErr)
					return
				}
				if !strings.Contains(err.Error(), tt.expectedErr) {
					t.Errorf("ParseISISNet(%q) expected error containing %q, got %q", tt.input, tt.expectedErr, err.Error())
				}
			}
		})
	}
}

func TestISISSystemIDIncrement(t *testing.T) {
	tests := []struct {
		name          string
		input         ISISSystemID
		offset        int
		expected      ISISSystemID
		expectedError bool
	}{
		{
			name:     "increment by 1 - no overflow",
			input:    ISISSystemID{0x00, 0x00, 0x00, 0x00, 0x00, 0x05},
			offset:   1,
			expected: ISISSystemID{0x00, 0x00, 0x00, 0x00, 0x00, 0x06},
		},
		{
			name:     "increment by 0 - no change",
			input:    ISISSystemID{0x00, 0x00, 0x00, 0x00, 0x00, 0x05},
			offset:   0,
			expected: ISISSystemID{0x00, 0x00, 0x00, 0x00, 0x00, 0x05},
		},
		{
			name:     "increment with overflow in last byte",
			input:    ISISSystemID{0x00, 0x00, 0x00, 0x00, 0x00, 0xFF},
			offset:   1,
			expected: ISISSystemID{0x00, 0x00, 0x00, 0x00, 0x01, 0x00},
		},
		{
			name:     "increment with overflow in last byte by 5",
			input:    ISISSystemID{0x00, 0x00, 0x00, 0x00, 0x00, 0xFE},
			offset:   5,
			expected: ISISSystemID{0x00, 0x00, 0x00, 0x00, 0x01, 0x03},
		},
		{
			name:     "increment by 257 (0x101)",
			input:    ISISSystemID{0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
			offset:   257,
			expected: ISISSystemID{0x00, 0x00, 0x00, 0x00, 0x01, 0x01},
		},
		{
			name:     "increment by 256 (exactly one byte)",
			input:    ISISSystemID{0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
			offset:   256,
			expected: ISISSystemID{0x00, 0x00, 0x00, 0x00, 0x01, 0x00},
		},
		{
			name:     "carry propagation through multiple bytes",
			input:    ISISSystemID{0x00, 0x00, 0x00, 0x00, 0xFF, 0xFF},
			offset:   1,
			expected: ISISSystemID{0x00, 0x00, 0x00, 0x01, 0x00, 0x00},
		},
		{
			name:     "carry propagation through all bytes",
			input:    ISISSystemID{0x00, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF},
			offset:   1,
			expected: ISISSystemID{0x01, 0x00, 0x00, 0x00, 0x00, 0x00},
		},
		{
			name:          "overflow entire systemID (wrap around)",
			input:         ISISSystemID{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF},
			offset:        1,
			expected:      ISISSystemID{0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
			expectedError: true,
		},
		{
			name:     "large increment with multiple byte overflow",
			input:    ISISSystemID{0x00, 0x00, 0x00, 0x00, 0xFF, 0xFE},
			offset:   257,
			expected: ISISSystemID{0x00, 0x00, 0x00, 0x01, 0x00, 0xFF},
		},
		{
			name:     "increment middle of range",
			input:    ISISSystemID{0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC},
			offset:   100,
			expected: ISISSystemID{0x12, 0x34, 0x56, 0x78, 0x9B, 0x20},
		},
		{
			name:     "increment by large value",
			input:    ISISSystemID{0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
			offset:   0x10000, // 65536
			expected: ISISSystemID{0x00, 0x00, 0x00, 0x01, 0x00, 0x00},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := IncrementSystemID(tt.input, tt.offset)
			if tt.expectedError != (err != nil) {
				t.Errorf("Increment failed, expected error present: (%t), but got error present: (%t)",
					tt.expectedError, err != nil)
			}

			if result != tt.expected {
				t.Errorf("Increment failed:\n  input:    %#v\n  offset:   %d (0x%X)\n  expected: %#v\n  got:      %#v",
					tt.input, tt.offset, tt.offset, tt.expected, result)
			}
		})
	}
}
