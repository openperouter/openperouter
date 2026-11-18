// SPDX-License-Identifier:Apache-2.0

package conversion

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/internal/hostnetwork"
)

func dualStackUnderlay() []v1alpha1.Underlay {
	return []v1alpha1.Underlay{{
		Spec: v1alpha1.UnderlaySpec{
			Interfaces: []v1alpha1.UnderlayInterface{{
				Type:          v1alpha1.UnderlayInterfaceTypeNetworkDevice,
				NetworkDevice: &v1alpha1.NetworkDevice{InterfaceName: "eth0"},
			}},
			TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{
				CIDRs: []string{"10.0.0.0/24", "2001:db8::/128"},
			},
		},
	}}
}

func TestAPItoHostConfigHostTrunks(t *testing.T) {
	tests := []struct {
		name   string
		trunks []v1alpha1.L2Trunk
		l2vnis []v1alpha1.L2VNI
		want   []hostnetwork.L2TrunkParams
	}{
		{
			name: "empty trunk uses the node defaults",
			trunks: []v1alpha1.L2Trunk{{
				ObjectMeta: metav1.ObjectMeta{Name: "workloads", Namespace: "tenant-a"},
				Spec:       v1alpha1.L2TrunkSpec{Type: v1alpha1.L2TrunkTypeHost},
			}},
			want: []hostnetwork.L2TrunkParams{{
				Name:           "workloads",
				Namespace:      "tenant-a",
				TargetNS:       "namespace",
				VTEPIP:         "10.0.0.0/32",
				VXLanPort:      4789,
				TunnelOverhead: hostnetwork.VXLanOverhead,
			}},
		},
		{
			name: "external host master is converted",
			trunks: []v1alpha1.L2Trunk{{
				ObjectMeta: metav1.ObjectMeta{Name: "workloads", Namespace: "tenant-a"},
				Spec: v1alpha1.L2TrunkSpec{
					Type: v1alpha1.L2TrunkTypeHost,
					HostMaster: &v1alpha1.HostMaster{
						Type: v1alpha1.LinuxBridge,
						LinuxBridge: &v1alpha1.LinuxBridgeConfig{
							Lifecycle: v1alpha1.BridgeLifecycleExternal,
							Name:      ptr.To("br-external"),
						},
					},
				},
			}},
			want: []hostnetwork.L2TrunkParams{{
				Name:      "workloads",
				Namespace: "tenant-a",
				TargetNS:  "namespace",
				HostMaster: &hostnetwork.HostMaster{
					Name:       ptr.To("br-external"),
					Type:       v1alpha1.LinuxBridge,
					AutoCreate: new(false),
				},
				VTEPIP:         "10.0.0.0/32",
				VXLanPort:      4789,
				TunnelOverhead: hostnetwork.VXLanOverhead,
			}},
		},
		{
			name: "referencing L2VNI selects family, port and overhead",
			trunks: []v1alpha1.L2Trunk{{
				ObjectMeta: metav1.ObjectMeta{Name: "workloads", Namespace: "tenant-a"},
				Spec:       v1alpha1.L2TrunkSpec{Type: v1alpha1.L2TrunkTypeHost},
			}},
			l2vnis: []v1alpha1.L2VNI{{
				ObjectMeta: metav1.ObjectMeta{Name: "l2", Namespace: "tenant-a"},
				Spec: v1alpha1.L2VNISpec{
					VNI:                   10100,
					UnderlayAddressFamily: ptr.To("IPv6"),
					VXLanPort:             ptr.To(int32(4790)),
					Trunk: &v1alpha1.L2TrunkReference{
						Name: "workloads",
						VLAN: v1alpha1.VLANConfig{Tag: 100},
					},
				},
			}},
			want: []hostnetwork.L2TrunkParams{{
				Name:           "workloads",
				Namespace:      "tenant-a",
				TargetNS:       "namespace",
				VTEPIP:         "2001:db8::/128",
				VXLanPort:      4790,
				TunnelOverhead: hostnetwork.IPv6VXLanOverhead,
			}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := APItoHostConfig(0, "namespace", APIConfigData{
				Underlays: dualStackUnderlay(),
				L2Trunks:  tt.trunks,
				L2VNIs:    tt.l2vnis,
			})
			require.NoError(t, err)
			require.Equal(t, tt.want, got.L2Trunks)
		})
	}
}

func TestAPItoHostConfigL2VNITrunkReference(t *testing.T) {
	trunked := v1alpha1.L2VNI{
		ObjectMeta: metav1.ObjectMeta{Name: "trunked", Namespace: "tenant-a"},
		Spec: v1alpha1.L2VNISpec{
			VNI: 10100,
			Trunk: &v1alpha1.L2TrunkReference{
				Name: "workloads",
				VLAN: v1alpha1.VLANConfig{Tag: 100},
			},
		},
	}
	standalone := v1alpha1.L2VNI{
		ObjectMeta: metav1.ObjectMeta{Name: "standalone", Namespace: "tenant-a"},
		Spec:       v1alpha1.L2VNISpec{VNI: 10200},
	}

	got, err := APItoHostConfig(0, "namespace", APIConfigData{
		Underlays: dualStackUnderlay(),
		L2Trunks: []v1alpha1.L2Trunk{{
			ObjectMeta: metav1.ObjectMeta{Name: "workloads", Namespace: "tenant-a"},
			Spec:       v1alpha1.L2TrunkSpec{Type: v1alpha1.L2TrunkTypeHost},
		}},
		L2VNIs: []v1alpha1.L2VNI{trunked, standalone},
	})
	require.NoError(t, err)
	require.Len(t, got.L2VNIs, 2)

	require.Equal(t, hostnetwork.TrunkID("tenant-a", "workloads"), got.L2VNIs[0].Trunk.TrunkID)
	require.Equal(t, int32(100), got.L2VNIs[0].Trunk.VLAN)
	require.Nil(t, got.L2VNIs[1].Trunk)
}

func TestMergeAPIConfigsL2Trunks(t *testing.T) {
	first := APIConfigData{L2Trunks: []v1alpha1.L2Trunk{{
		ObjectMeta: metav1.ObjectMeta{Name: "one", Namespace: "tenant-a"},
	}}}
	second := APIConfigData{L2Trunks: []v1alpha1.L2Trunk{{
		ObjectMeta: metav1.ObjectMeta{Name: "two", Namespace: "tenant-b"},
	}}}

	merged, err := MergeAPIConfigs(first, second)
	require.NoError(t, err)
	require.Len(t, merged.L2Trunks, 2)
	require.Equal(t, "one", merged.L2Trunks[0].Name)
	require.Equal(t, "two", merged.L2Trunks[1].Name)
}
