// SPDX-License-Identifier:Apache-2.0

package grout

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/openperouter/openperouter/internal/hostnetwork"
	"github.com/stretchr/testify/assert"
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
