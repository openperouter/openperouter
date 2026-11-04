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
	const showVLAN = grcliPrefix + "interface show name t000003001.33"
	tests := []struct {
		name     string
		existing string
		want     []string
	}{
		{
			name: "creates the VLAN before attaching it",
			want: []string{
				showVLAN,
				grcliPrefix + "interface add vlan t000003001.33 parent t_000003001 vlan_id 33",
				grcliPrefix + "interface set vlan t000003001.33 domain br-pe-100",
				grcliPrefix + "interface show",
			},
		},
		{
			name: "attaches an existing VLAN",
			existing: `{"name":"t000003001.33","type":"vlan",` +
				`"vlan_parent":"t_000003001","vlan_id":33}`,
			want: []string{
				showVLAN,
				grcliPrefix + "interface set vlan t000003001.33 domain br-pe-100",
				grcliPrefix + "interface show",
			},
		},
		{
			name: "moves an existing VLAN to the requested bridge",
			existing: `{"name":"t000003001.33","type":"vlan","domain":"br-pe-200",` +
				`"vlan_parent":"t_000003001","vlan_id":33}`,
			want: []string{
				showVLAN,
				grcliPrefix + "interface set vlan t000003001.33 domain br-pe-100",
				grcliPrefix + "interface show",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := recordGrcli(t, map[string]string{showVLAN: tt.existing})
			params := hostnetwork.L2VNIParams{Trunk: &hostnetwork.L2VNITrunkParams{Name: "workloads", VLAN: 33}}
			err := attachL2VNIBridge(context.Background(), NewClient("sock"), nil, params,
				"br-pe-100", map[string]string{"workloads": "t_000003001"})
			require.NoError(t, err)
			require.Equal(t, tt.want, *got)
		})
	}
}

func TestAttachL2VNIBridgeRetriesAfterAttachmentFailure(t *testing.T) {
	const showVLAN = grcliPrefix + "interface show name t000003001.33"
	const addVLAN = grcliPrefix + "interface add vlan t000003001.33 parent t_000003001 vlan_id 33"
	const attachVLAN = grcliPrefix + "interface set vlan t000003001.33 domain br-pe-100"
	outputs := map[string]string{}
	got := recordGrcli(t, outputs)
	mocked := execCmd
	failAttachment := true
	execCmd = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		out, err := mocked(ctx, name, args...)
		cmd := name + " " + strings.Join(args, " ")
		if cmd == addVLAN {
			outputs[showVLAN] = `{"name":"t000003001.33","type":"vlan",` +
				`"vlan_parent":"t_000003001","vlan_id":33}`
		}
		if cmd == attachVLAN && failAttachment {
			failAttachment = false
			return []byte(`{"error":"attachment failed","errno":5}`), fmt.Errorf("exit status 1")
		}
		return out, err
	}
	params := hostnetwork.L2VNIParams{Trunk: &hostnetwork.L2VNITrunkParams{Name: "workloads", VLAN: 33}}
	attach := func() error {
		return attachL2VNIBridge(context.Background(), NewClient("sock"), nil, params,
			"br-pe-100", map[string]string{"workloads": "t_000003001"})
	}
	require.ErrorContains(t, attach(), "failed to attach trunk workloads to bridge br-pe-100")
	require.Equal(t, []string{showVLAN, addVLAN, attachVLAN}, *got, "cleanup must wait for attachment")
	require.NoError(t, attach())
	require.Equal(t, []string{
		showVLAN, addVLAN, attachVLAN,
		showVLAN, attachVLAN, grcliPrefix + "interface show",
	}, *got)
}
