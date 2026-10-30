// SPDX-License-Identifier:Apache-2.0

package hostnetwork

import (
	"net"
	"reflect"
	"testing"
)

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
