// SPDX-License-Identifier:Apache-2.0

package hostnetwork

import (
	"fmt"
	"net"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRouterMACForNode(t *testing.T) {
	tests := []struct {
		name      string
		prefix    string
		index     int
		want      string
		wantError string
	}{
		{name: "omitted", index: 1},
		{name: "first node", prefix: "02:00:00:00:00:00/32", index: 1, want: "02:00:00:00:00:01"},
		{name: "second node", prefix: "02:00:00:00:00:00/32", index: 2, want: "02:00:00:00:00:02"},
		{name: "zero index", prefix: "02:00:00:00:00:00/32", want: "02:00:00:00:00:00"},
		{name: "maximum index", prefix: "02:00:00:00:00:00/32", index: 65535, want: "02:00:00:00:ff:ff"},
		{name: "non byte boundary", prefix: "02:00:00:00:80:00/33", index: 32767, want: "02:00:00:00:ff:ff"},
		{name: "non byte overflow", prefix: "02:00:00:00:80:00/33", index: 32768,
			wantError: `node index 32768 does not fit router MAC prefix "02:00:00:00:80:00/33"`},
		{name: "one host bit", prefix: "02:00:00:00:00:00/47", index: 1, want: "02:00:00:00:00:01"},
		{name: "whole first byte", prefix: "02:00:00:00:00:00/8", index: 65536, want: "02:00:00:01:00:00"},
		{name: "normalization", prefix: "02-AB-CD-00-00-00/24", index: 256, want: "02:ab:cd:00:01:00"},
		{name: "missing address", prefix: "/32",
			wantError: `router MAC prefix "/32" is missing a MAC address`},
		{name: "missing length", prefix: "02:00:00:00:00:00",
			wantError: `router MAC prefix "02:00:00:00:00:00" must include a prefix length`},
		{name: "malformed address", prefix: "invalid/32",
			wantError: `invalid router MAC "invalid": address invalid: invalid MAC address`},
		{name: "multicast", prefix: "01:00:00:00:00:00/32",
			wantError: `router MAC "01:00:00:00:00:00" must be a nonzero 6-byte unicast address`},
		{name: "nonzero host bits", prefix: "02:00:00:00:00:01/32",
			wantError: `router MAC prefix "02:00:00:00:00:01/32" has nonzero host bits`},
		{name: "negative index", prefix: "02:00:00:00:00:00/32", index: -1,
			wantError: `node index -1 does not fit router MAC prefix "02:00:00:00:00:00/32"`},
		{name: "overflow", prefix: "02:00:00:00:00:00/32", index: 65536,
			wantError: `node index 65536 does not fit router MAC prefix "02:00:00:00:00:00/32"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := RouterMACForNode(tt.prefix, tt.index)
			if tt.wantError != "" {
				require.EqualError(t, err, tt.wantError)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got.String())
		})
	}
}

func TestRouterMACPrefixLength(t *testing.T) {
	for _, length := range []string{"", "invalid", "7", "48", "49", "-1", "32/1"} {
		t.Run(length, func(t *testing.T) {
			prefix := "02:00:00:00:00:00/" + length
			_, err := RouterMACForNode(prefix, 0)
			require.EqualError(t, err, fmt.Sprintf("router MAC prefix %q must have a length between 8 and 47", prefix))
		})
	}
}

func TestParseRouterMAC(t *testing.T) {
	tests := []struct {
		name      string
		value     string
		want      net.HardwareAddr
		wantError bool
	}{
		{name: "omitted"},
		{name: "local unicast", value: "02:00:00:00:00:01", want: net.HardwareAddr{2, 0, 0, 0, 0, 1}},
		{name: "global unicast", value: "00:11:22:AA:BB:CC", want: net.HardwareAddr{0, 0x11, 0x22, 0xaa, 0xbb, 0xcc}},
		{name: "malformed", value: "not-a-mac", wantError: true},
		{name: "zero", value: "00:00:00:00:00:00", wantError: true},
		{name: "multicast", value: "01:00:5e:00:00:01", wantError: true},
		{name: "broadcast", value: "ff:ff:ff:ff:ff:ff", wantError: true},
		{name: "EUI-64", value: "02:00:00:00:00:00:00:01", wantError: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseRouterMAC(tt.value)
			if (err != nil) != tt.wantError {
				t.Fatalf("ParseRouterMAC(%q) error = %v, wantError %v", tt.value, err, tt.wantError)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseRouterMAC(%q) = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}
