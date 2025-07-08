// SPDX-License-Identifier:Apache-2.0

package url

import (
	"testing"
)

func TestFormat(t *testing.T) {
	tests := []struct {
		name     string
		format   string
		ip       string
		expected string
	}{
		{
			name:     "IPv4 address",
			format:   "http://%s:8090/clientip",
			ip:       "192.168.1.1",
			expected: "http://192.168.1.1:8090/clientip",
		},
		{
			name:     "IPv6 address",
			format:   "http://%s:8090/clientip",
			ip:       "2001:db8::1",
			expected: "http://[2001:db8::1]:8090/clientip",
		},
		{
			name:     "IPv6 address with hostname format",
			format:   "http://%s:8090/hostname",
			ip:       "2001:db8::1",
			expected: "http://[2001:db8::1]:8090/hostname",
		},
		{
			name:     "IPv4 address with hostname format",
			format:   "http://%s:8090/hostname",
			ip:       "192.168.1.1",
			expected: "http://192.168.1.1:8090/hostname",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Format(tt.format, tt.ip)
			if result != tt.expected {
				t.Errorf("Format(%q, %q) = %q, want %q", tt.format, tt.ip, result, tt.expected)
			}
		})
	}
}
