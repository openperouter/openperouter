// SPDX-License-Identifier:Apache-2.0

package grout

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/openperouter/openperouter/internal/hostnetwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetupL2GatewaySetsMACBeforeAddresses(t *testing.T) {
	original := execCmd
	t.Cleanup(func() { execCmd = original })

	want := []string{
		"grcli --err-exit --json --socket sock interface set bridge br-pe-100 mac 00:f3:00:00:00:65",
		"grcli --err-exit --json --socket sock address add 192.0.2.1/24 iface br-pe-100",
		"grcli --err-exit --json --socket sock address add 2001:db8::1/64 iface br-pe-100",
	}
	var got []string
	execCmd = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		got = append(got, name+" "+strings.Join(args, " "))
		return nil, nil
	}

	err := setupL2Gateway(context.Background(), NewClient("sock"), "br-pe-100", hostnetwork.L2VNIParams{
		VNIParams:    hostnetwork.VNIParams{VNI: 100},
		L2GatewayIPs: []string{"192.0.2.1/24", "2001:db8::1/64"},
	})
	assert.NoError(t, err)
	assert.Equal(t, want, got, fmt.Sprintf("unexpected grcli sequence: %v", got))
}

func TestAttachL2VNIBridgeToTrunk(t *testing.T) {
	const showVLAN = grcliPrefix + "interface show name enp1s001021"
	tests := []struct {
		name     string
		existing string
		want     []string
	}{
		{
			name: "creates the VLAN before attaching it",
			want: []string{
				showVLAN,
				grcliPrefix + "interface add vlan enp1s001021 parent enp1s001 vlan_id 33",
				grcliPrefix + "interface set vlan enp1s001021 domain br-pe-100",
			},
		},
		{
			name: "attaches an existing VLAN",
			existing: `{"name":"enp1s001021","type":"vlan",` +
				`"vlan_parent":"enp1s001","vlan_id":33}`,
			want: []string{
				showVLAN,
				grcliPrefix + "interface set vlan enp1s001021 domain br-pe-100",
			},
		},
		{
			name: "moves an existing VLAN to the requested bridge",
			existing: `{"name":"enp1s001021","type":"vlan","domain":"br-pe-200",` +
				`"vlan_parent":"enp1s001","vlan_id":33}`,
			want: []string{
				showVLAN,
				grcliPrefix + "interface set vlan enp1s001021 domain br-pe-100",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := recordGrcli(t, map[string]string{showVLAN: tt.existing})
			params := hostnetwork.L2VNIParams{Trunk: &hostnetwork.L2VNITrunkParams{
				Name: "workloads", VLAN: 33,
			}}
			trunks := ConfiguredTrunks{
				"workloads": {
					Name:  "workloads",
					SRIOV: &hostnetwork.SRIOVTrunkParams{PFName: "enp1s0", VFIndex: 1},
				},
			}
			err := attachL2VNIBridge(context.Background(), NewClient("sock"), nil, params, "br-pe-100", trunks)
			require.NoError(t, err)
			require.Equal(t, tt.want, *got)
		})
	}
}

func TestAttachL2VNIBridgeWithUnconfiguredTrunk(t *testing.T) {
	tests := []struct {
		name   string
		trunks ConfiguredTrunks
	}{
		{name: "no configured trunks"},
		{
			name: "only another trunk is configured",
			trunks: ConfiguredTrunks{
				"other": {
					Name:  "other",
					SRIOV: &hostnetwork.SRIOVTrunkParams{PFName: "enp1s0", VFIndex: 2},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := recordGrcli(t, nil)
			params := hostnetwork.L2VNIParams{Trunk: &hostnetwork.L2VNITrunkParams{
				Name: "workloads", VLAN: 33,
			}}
			err := attachL2VNIBridge(context.Background(), NewClient("sock"), nil, params, "br-pe-100", tt.trunks)
			require.EqualError(t, err, `L2Trunk "workloads" failed provisioning`)
			require.Empty(t, *got, "an unconfigured trunk must not create or attach a VLAN")
		})
	}
}
