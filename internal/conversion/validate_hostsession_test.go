// SPDX-License-Identifier:Apache-2.0

package conversion

import (
	"testing"

	v1alpha1 "github.com/openperouter/openperouter/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestValidateHostSessions(t *testing.T) {
	tests := []struct {
		name          string
		l3VNIs        []v1alpha1.L3VNI
		l3Passthrough []v1alpha1.L3Passthrough
		wantErr       bool
	}{
		{
			name: "valid host sessions",
			l3VNIs: []v1alpha1.L3VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1"},
					Spec: v1alpha1.L3VNISpec{
						VNI:         1001,
						HostSession: &v1alpha1.HostSession{ASN: 65001, HostASN: 65002, LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: "192.168.1.0/24"}},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni2"},
					Spec: v1alpha1.L3VNISpec{
						VNI:         1002,
						HostSession: &v1alpha1.HostSession{ASN: 65003, HostASN: 65004, LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: "192.168.2.0/24"}},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "overlapping IPv4 CIDRs",
			l3VNIs: []v1alpha1.L3VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1"},
					Spec: v1alpha1.L3VNISpec{
						VNI:         1001,
						HostSession: &v1alpha1.HostSession{ASN: 65001, HostASN: 65002, LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: "192.168.1.0/24"}},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni2"},
					Spec: v1alpha1.L3VNISpec{
						VNI:         1002,
						HostSession: &v1alpha1.HostSession{ASN: 65003, HostASN: 65004, LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: "192.168.1.128/25"}},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "overlapping IPv6 CIDRs",
			l3VNIs: []v1alpha1.L3VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1"},
					Spec: v1alpha1.L3VNISpec{
						VNI:         1001,
						HostSession: &v1alpha1.HostSession{ASN: 65001, HostASN: 65002, LocalCIDR: v1alpha1.LocalCIDRConfig{IPv6: "2001:db8::/64"}},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni2"},
					Spec: v1alpha1.L3VNISpec{
						VNI:         1002,
						HostSession: &v1alpha1.HostSession{ASN: 65003, HostASN: 65004, LocalCIDR: v1alpha1.LocalCIDRConfig{IPv6: "2001:db8::/80"}},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "invalid IPv4 localcidr",
			l3VNIs: []v1alpha1.L3VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1"},
					Spec: v1alpha1.L3VNISpec{
						VNI:         100,
						HostSession: &v1alpha1.HostSession{ASN: 65001, HostASN: 65002, LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: "not-a-cidr"}},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "invalid IPv6 localcidr",
			l3VNIs: []v1alpha1.L3VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1"},
					Spec: v1alpha1.L3VNISpec{
						VNI:         100,
						HostSession: &v1alpha1.HostSession{ASN: 65001, HostASN: 65002, LocalCIDR: v1alpha1.LocalCIDRConfig{IPv6: "not-a-cidr"}},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "no CIDR provided",
			l3VNIs: []v1alpha1.L3VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1"},
					Spec: v1alpha1.L3VNISpec{
						VNI:         100,
						HostSession: &v1alpha1.HostSession{ASN: 65001, HostASN: 65002, LocalCIDR: v1alpha1.LocalCIDRConfig{}},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "same local and remote ASN",
			l3VNIs: []v1alpha1.L3VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1"},
					Spec: v1alpha1.L3VNISpec{
						VNI: 100,
						HostSession: &v1alpha1.HostSession{
							ASN:       65001,
							HostASN:   65001,
							LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: "192.168.1.0/24"},
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "no host session",
			l3VNIs: []v1alpha1.L3VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1"},
					Spec: v1alpha1.L3VNISpec{
						VNI: 100,
					},
				},
			},
			wantErr: false,
		},
		{
			name: "mixed IPv4 and IPv6",
			l3VNIs: []v1alpha1.L3VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1"},
					Spec: v1alpha1.L3VNISpec{
						VNI:         1001,
						HostSession: &v1alpha1.HostSession{ASN: 65001, HostASN: 65002, LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: "192.168.1.0/24"}},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni2"},
					Spec: v1alpha1.L3VNISpec{
						VNI:         1002,
						HostSession: &v1alpha1.HostSession{ASN: 65003, HostASN: 65004, LocalCIDR: v1alpha1.LocalCIDRConfig{IPv6: "2001:db8::/64"}},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "dual stack host session",
			l3VNIs: []v1alpha1.L3VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1"},
					Spec: v1alpha1.L3VNISpec{
						VNI: 1001,
						HostSession: &v1alpha1.HostSession{
							ASN:     65001,
							HostASN: 65002,
							LocalCIDR: v1alpha1.LocalCIDRConfig{
								IPv4: "192.168.1.0/24",
								IPv6: "2001:db8::/64",
							},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "l3passthrough with host session",
			l3Passthrough: []v1alpha1.L3Passthrough{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "passthrough1"},
					Spec: v1alpha1.L3PassthroughSpec{
						HostSession: v1alpha1.HostSession{ASN: 65001, HostASN: 65002, LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: "192.168.1.0/24"}},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "mixed l3vni and l3passthrough",
			l3VNIs: []v1alpha1.L3VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1"},
					Spec: v1alpha1.L3VNISpec{
						VNI:         1001,
						HostSession: &v1alpha1.HostSession{ASN: 65001, HostASN: 65002, LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: "192.168.1.0/24"}},
					},
				},
			},
			l3Passthrough: []v1alpha1.L3Passthrough{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "passthrough1"},
					Spec: v1alpha1.L3PassthroughSpec{
						HostSession: v1alpha1.HostSession{ASN: 65003, HostASN: 65004, LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: "192.168.2.0/24"}},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "overlapping CIDRs between l3vni and l3passthrough",
			l3VNIs: []v1alpha1.L3VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1"},
					Spec: v1alpha1.L3VNISpec{
						VNI:         1001,
						HostSession: &v1alpha1.HostSession{ASN: 65001, HostASN: 65002, LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: "192.168.1.0/24"}},
					},
				},
			},
			l3Passthrough: []v1alpha1.L3Passthrough{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "passthrough1"},
					Spec: v1alpha1.L3PassthroughSpec{
						HostSession: v1alpha1.HostSession{ASN: 65003, HostASN: 65004, LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: "192.168.1.128/25"}},
					},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateHostSessions(tt.l3VNIs, tt.l3Passthrough, &NoOpStatusReporter{})
			if tt.wantErr && err == nil {
				t.Errorf("ValidateHostSessions() expected error but got none")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("ValidateHostSessions() unexpected error: %v", err)
			}
		})
	}
}
