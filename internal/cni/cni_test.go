// SPDX-License-Identifier:Apache-2.0

package cni

import (
	"encoding/json"
	"testing"
)

func TestIPAMType(t *testing.T) {
	cases := []struct {
		name    string
		config  string
		want    string
		wantErr bool
	}{
		{
			name:   "single conf dhcp",
			config: `{"cniVersion":"1.0.0","name":"n","type":"macvlan","ipam":{"type":"dhcp"}}`,
			want:   "dhcp",
		},
		{
			name:   "conflist dhcp",
			config: `{"cniVersion":"1.0.0","name":"n","plugins":[{"type":"macvlan","ipam":{"type":"dhcp"}}]}`,
			want:   "dhcp",
		},
		{
			name:   "conflist static",
			config: `{"cniVersion":"1.0.0","name":"n","plugins":[{"type":"macvlan","ipam":{"type":"static"}}]}`,
			want:   "static",
		},
		{
			name:   "no ipam",
			config: `{"cniVersion":"1.0.0","name":"n","plugins":[{"type":"bridge"}]}`,
			want:   "",
		},
		{
			name:    "invalid json",
			config:  `{`,
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := IPAMType([]byte(tc.config))
			if (err != nil) != tc.wantErr {
				t.Fatalf("IPAMType() err=%v wantErr=%v", err, tc.wantErr)
			}
			if got != tc.want {
				t.Errorf("IPAMType()=%q want %q", got, tc.want)
			}
		})
	}
}

// TestIpamPluginConfigConflist verifies that for a conflist the network-level
// name/cniVersion are injected into the selected plugin config, so the dhcp
// daemon derives the same clientID it used for the original chained ADD.
func TestIpamPluginConfigConflist(t *testing.T) {
	config := `{"cniVersion":"1.0.0","name":"underlay-nad","plugins":[{"type":"macvlan","master":"toswitch1","ipam":{"type":"dhcp"}}]}`
	netconf, ipamType, err := ipamPluginConfig([]byte(config))
	if err != nil {
		t.Fatal(err)
	}
	if ipamType != "dhcp" {
		t.Fatalf("ipamType=%q want dhcp", ipamType)
	}
	var got map[string]any
	if err := json.Unmarshal(netconf, &got); err != nil {
		t.Fatal(err)
	}
	if got["name"] != "underlay-nad" {
		t.Errorf("name not injected: %v", got["name"])
	}
	if got["cniVersion"] != "1.0.0" {
		t.Errorf("cniVersion not injected: %v", got["cniVersion"])
	}
	if got["type"] != "macvlan" {
		t.Errorf("plugin type lost: %v", got["type"])
	}
}

// TestIpamPluginConfigSingleConf verifies a single-plugin conf is returned
// unchanged with its ipam type.
func TestIpamPluginConfigSingleConf(t *testing.T) {
	config := `{"cniVersion":"1.0.0","name":"n","type":"macvlan","ipam":{"type":"dhcp"}}`
	netconf, ipamType, err := ipamPluginConfig([]byte(config))
	if err != nil {
		t.Fatal(err)
	}
	if ipamType != "dhcp" {
		t.Fatalf("ipamType=%q want dhcp", ipamType)
	}
	if string(netconf) != config {
		t.Errorf("single conf altered:\n got %s\nwant %s", netconf, config)
	}
}
