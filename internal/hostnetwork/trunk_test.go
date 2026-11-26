// SPDX-License-Identifier:Apache-2.0

package hostnetwork

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTrunkID(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
		trunkName string
	}{
		{name: "simple", namespace: "default", trunkName: "workloads"},
		{name: "namespaced", namespace: "tenant-a", trunkName: "workloads"},
		{name: "maximum length name", namespace: "default", trunkName: strings.Repeat("a", 253)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id := TrunkID(tt.namespace, tt.trunkName)
			require.Len(t, id, trunkIDLength)
			require.Equal(t, id, TrunkID(tt.namespace, tt.trunkName), "the identifier must be stable")

			names := TrunkDeviceNamesFor(tt.namespace, tt.trunkName)
			for _, deviceName := range []string{names.HostSide, names.NamespaceSide, names.Bridge, names.VXLan} {
				require.LessOrEqual(t, len(deviceName), 15, "device name %q exceeds IFNAMSIZ", deviceName)
			}
		})
	}
}

func TestTrunkIDIsNamespaced(t *testing.T) {
	first := TrunkID("tenant-a", "workloads")
	second := TrunkID("tenant-b", "workloads")
	require.NotEqual(t, first, second,
		"two trunks named workloads in different namespaces must not share devices")

	require.NotEqual(t, TrunkID("default", "one"), TrunkID("default", "two"))
}

func TestTrunkNameParsers(t *testing.T) {
	names := TrunkDeviceNamesFor("tenant-a", "workloads")
	id := TrunkID("tenant-a", "workloads")

	tests := []struct {
		name   string
		parse  func(string) (string, error)
		device string
	}{
		{name: "host side", parse: trunkIDFromHostName, device: names.HostSide},
		{name: "router side", parse: trunkIDFromPEName, device: names.NamespaceSide},
		{name: "bridge", parse: trunkIDFromBridgeName, device: names.Bridge},
		{name: "vxlan", parse: trunkIDFromVXLanName, device: names.VXLan},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.parse(tt.device)
			require.NoError(t, err)
			require.Equal(t, id, got)

			_, err = tt.parse("vni100")
			var notRouter NotRouterInterfaceError
			require.True(t, errors.As(err, &notRouter), "expected NotRouterInterfaceError, got %v", err)

			_, err = tt.parse(strings.TrimSuffix(tt.device, "b") + "bb")
			require.Error(t, err)
		})
	}
}

func TestVniParsersIgnoreTrunkDevices(t *testing.T) {
	names := TrunkDeviceNamesFor("tenant-a", "workloads")

	tests := []struct {
		name  string
		parse func(string) (int32, error)
	}{
		{name: "bridge", parse: vniFromBridgeName},
		{name: "vxlan", parse: vniFromVXLanName},
		{name: "switching bridge", parse: vniFromSwitchingBridgeName},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, device := range []string{names.Bridge, names.VXLan} {
				_, err := tt.parse(device)
				var notRouter NotRouterInterfaceError
				require.True(t, errors.As(err, &notRouter),
					"vni parsers must ignore trunk device %q, got %v", device, err)
			}
		})
	}
}
