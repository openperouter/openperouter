// SPDX-License-Identifier:Apache-2.0

package conversion

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/openperouter/openperouter/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestFilterUniqueVRFs(t *testing.T) {
	tcs := []struct {
		name     string
		l3vnis   []v1alpha1.L3VNI
		l3vpns   []v1alpha1.L3VPN
		wantVNIs []v1alpha1.L3VNI
		wantVPNs []v1alpha1.L3VPN
		wantErrs []string
	}{
		{
			name: "all L3VNIs in different VRFs",
			l3vnis: []v1alpha1.L3VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1", Namespace: "test"},
					Spec: v1alpha1.L3VNISpec{
						VNI:         1001,
						VRF:         "vrf1",
						HostSession: &v1alpha1.HostSession{ASN: 65001, HostASN: new(int64(65002)), LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: new("192.168.1.0/24")}},
					},
					Status: &v1alpha1.L3VNIStatus{},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni2", Namespace: "test"},
					Spec: v1alpha1.L3VNISpec{
						VNI:         1002,
						VRF:         "vrf2",
						HostSession: &v1alpha1.HostSession{ASN: 65003, HostASN: new(int64(65004)), LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: new("192.168.2.0/24")}},
					},
					Status: &v1alpha1.L3VNIStatus{},
				},
			},
			wantVNIs: []v1alpha1.L3VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1", Namespace: "test"},
					Spec: v1alpha1.L3VNISpec{
						VNI:         1001,
						VRF:         "vrf1",
						HostSession: &v1alpha1.HostSession{ASN: 65001, HostASN: new(int64(65002)), LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: new("192.168.1.0/24")}},
					},
					Status: &v1alpha1.L3VNIStatus{},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni2", Namespace: "test"},
					Spec: v1alpha1.L3VNISpec{
						VNI:         1002,
						VRF:         "vrf2",
						HostSession: &v1alpha1.HostSession{ASN: 65003, HostASN: new(int64(65004)), LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: new("192.168.2.0/24")}},
					},
					Status: &v1alpha1.L3VNIStatus{},
				},
			},
		},
		{
			name: "duplicate VRF across L3VNIs",
			l3vnis: []v1alpha1.L3VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1", Namespace: "test"},
					Spec: v1alpha1.L3VNISpec{
						VNI:         1001,
						VRF:         "vrf1",
						HostSession: &v1alpha1.HostSession{ASN: 65001, HostASN: new(int64(65002)), LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: new("192.168.1.0/24")}},
					},
					Status: &v1alpha1.L3VNIStatus{},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni2", Namespace: "test"},
					Spec: v1alpha1.L3VNISpec{
						VNI:         1002,
						VRF:         "vrf1",
						HostSession: &v1alpha1.HostSession{ASN: 65003, HostASN: new(int64(65004)), LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: new("192.168.2.0/24")}},
					},
					Status: &v1alpha1.L3VNIStatus{},
				},
			},
			wantVNIs: []v1alpha1.L3VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1", Namespace: "test"},
					Spec: v1alpha1.L3VNISpec{
						VNI:         1001,
						VRF:         "vrf1",
						HostSession: &v1alpha1.HostSession{ASN: 65001, HostASN: new(int64(65002)), LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: new("192.168.1.0/24")}},
					},
					Status: &v1alpha1.L3VNIStatus{},
				},
			},
			wantErrs: []string{
				`L3VNI/vni2: more than one L3VNI detected in VRF "vrf1": "test/vni1" already exists`,
			},
		},
		{
			name: "all L3VPNs in different VRFs",
			l3vpns: []v1alpha1.L3VPN{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1", Namespace: "test"},
					Spec: v1alpha1.L3VPNSpec{
						RDAssignedNumber: 1001,
						VRF:              "vrf1",
						HostSession:      &v1alpha1.HostSession{ASN: 65001, HostASN: new(int64(65002)), LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: new("192.168.1.0/24")}},
					},
					Status: &v1alpha1.L3VPNStatus{},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni2", Namespace: "test"},
					Spec: v1alpha1.L3VPNSpec{
						RDAssignedNumber: 1002,
						VRF:              "vrf2",
						HostSession:      &v1alpha1.HostSession{ASN: 65003, HostASN: new(int64(65004)), LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: new("192.168.2.0/24")}},
					},
					Status: &v1alpha1.L3VPNStatus{},
				},
			},
			wantVPNs: []v1alpha1.L3VPN{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1", Namespace: "test"},
					Spec: v1alpha1.L3VPNSpec{
						RDAssignedNumber: 1001,
						VRF:              "vrf1",
						HostSession:      &v1alpha1.HostSession{ASN: 65001, HostASN: new(int64(65002)), LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: new("192.168.1.0/24")}},
					},
					Status: &v1alpha1.L3VPNStatus{},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni2", Namespace: "test"},
					Spec: v1alpha1.L3VPNSpec{
						RDAssignedNumber: 1002,
						VRF:              "vrf2",
						HostSession:      &v1alpha1.HostSession{ASN: 65003, HostASN: new(int64(65004)), LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: new("192.168.2.0/24")}},
					},
					Status: &v1alpha1.L3VPNStatus{},
				},
			},
		},
		{
			name: "duplicate VRF across L3VPNs",
			l3vpns: []v1alpha1.L3VPN{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1", Namespace: "test"},
					Spec: v1alpha1.L3VPNSpec{
						RDAssignedNumber: 1001,
						VRF:              "vrf1",
						HostSession:      &v1alpha1.HostSession{ASN: 65001, HostASN: new(int64(65002)), LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: new("192.168.1.0/24")}},
					},
					Status: &v1alpha1.L3VPNStatus{},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni2", Namespace: "test"},
					Spec: v1alpha1.L3VPNSpec{
						RDAssignedNumber: 1002,
						VRF:              "vrf2",
						HostSession:      &v1alpha1.HostSession{ASN: 65003, HostASN: new(int64(65004)), LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: new("192.168.2.0/24")}},
					},
					Status: &v1alpha1.L3VPNStatus{},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni3", Namespace: "test"},
					Spec: v1alpha1.L3VPNSpec{
						RDAssignedNumber: 1003,
						VRF:              "vrf1",
						HostSession:      &v1alpha1.HostSession{ASN: 65005, HostASN: new(int64(65006)), LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: new("192.168.3.0/24")}},
					},
					Status: &v1alpha1.L3VPNStatus{},
				},
			},
			wantVPNs: []v1alpha1.L3VPN{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1", Namespace: "test"},
					Spec: v1alpha1.L3VPNSpec{
						RDAssignedNumber: 1001,
						VRF:              "vrf1",
						HostSession:      &v1alpha1.HostSession{ASN: 65001, HostASN: new(int64(65002)), LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: new("192.168.1.0/24")}},
					},
					Status: &v1alpha1.L3VPNStatus{},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni2", Namespace: "test"},
					Spec: v1alpha1.L3VPNSpec{
						RDAssignedNumber: 1002,
						VRF:              "vrf2",
						HostSession:      &v1alpha1.HostSession{ASN: 65003, HostASN: new(int64(65004)), LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: new("192.168.2.0/24")}},
					},
					Status: &v1alpha1.L3VPNStatus{},
				},
			},
			wantErrs: []string{
				`L3VPN/vni3: more than one L3VPN detected in VRF "vrf1": "test/vni1" already exists`,
			},
		},
		{
			name: "both empty",
		},
		{
			name: "L3VNI and L3VPN in same VRF",
			l3vnis: []v1alpha1.L3VNI{
				{ObjectMeta: metav1.ObjectMeta{Name: "vni1", Namespace: "test"}, Spec: v1alpha1.L3VNISpec{VRF: "red"}},
			},
			l3vpns: []v1alpha1.L3VPN{
				{ObjectMeta: metav1.ObjectMeta{Name: "vpn1", Namespace: "test"}, Spec: v1alpha1.L3VPNSpec{VRF: "red"}},
			},
			wantVNIs: []v1alpha1.L3VNI{
				{ObjectMeta: metav1.ObjectMeta{Name: "vni1", Namespace: "test"}, Spec: v1alpha1.L3VNISpec{VRF: "red"}},
			},
			wantErrs: []string{
				`L3VPN/vpn1: conflict with L3VNI detected in VRF "red": "test/vni1" already exists`,
			},
		},
		{
			name: "L3VNI and L3VPN in different VRFs",
			l3vnis: []v1alpha1.L3VNI{
				{ObjectMeta: metav1.ObjectMeta{Name: "vni1"}, Spec: v1alpha1.L3VNISpec{VRF: "red"}},
			},
			l3vpns: []v1alpha1.L3VPN{
				{ObjectMeta: metav1.ObjectMeta{Name: "vpn1"}, Spec: v1alpha1.L3VPNSpec{VRF: "blue"}},
			},
			wantVNIs: []v1alpha1.L3VNI{
				{ObjectMeta: metav1.ObjectMeta{Name: "vni1"}, Spec: v1alpha1.L3VNISpec{VRF: "red"}},
			},
			wantVPNs: []v1alpha1.L3VPN{
				{ObjectMeta: metav1.ObjectMeta{Name: "vpn1"}, Spec: v1alpha1.L3VPNSpec{VRF: "blue"}},
			},
		},
		{
			name: "mixed: one conflicting VRF and one non-conflicting",
			l3vnis: []v1alpha1.L3VNI{
				{ObjectMeta: metav1.ObjectMeta{Name: "vni-green", Namespace: "test"}, Spec: v1alpha1.L3VNISpec{VRF: "green"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "vni-red", Namespace: "test"}, Spec: v1alpha1.L3VNISpec{VRF: "red"}},
			},
			l3vpns: []v1alpha1.L3VPN{
				{ObjectMeta: metav1.ObjectMeta{Name: "vpn-blue", Namespace: "test"}, Spec: v1alpha1.L3VPNSpec{VRF: "blue"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "vpn-red", Namespace: "test"}, Spec: v1alpha1.L3VPNSpec{VRF: "red"}},
			},
			wantVNIs: []v1alpha1.L3VNI{
				{ObjectMeta: metav1.ObjectMeta{Name: "vni-green", Namespace: "test"}, Spec: v1alpha1.L3VNISpec{VRF: "green"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "vni-red", Namespace: "test"}, Spec: v1alpha1.L3VNISpec{VRF: "red"}},
			},
			wantVPNs: []v1alpha1.L3VPN{
				{ObjectMeta: metav1.ObjectMeta{Name: "vpn-blue", Namespace: "test"}, Spec: v1alpha1.L3VPNSpec{VRF: "blue"}},
			},
			wantErrs: []string{
				`L3VPN/vpn-red: conflict with L3VNI detected in VRF "red": "test/vni-red" already exists`,
			},
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			gotVNIs, gotVPNs, err := FilterUniqueVRFs(tc.l3vnis, tc.l3vpns)
			if diff := cmp.Diff(tc.wantVNIs, gotVNIs); diff != "" {
				t.Errorf("L3VNIs mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tc.wantVPNs, gotVPNs); diff != "" {
				t.Errorf("L3VPNs mismatch (-want +got):\n%s", diff)
			}

			if len(tc.wantErrs) == 0 && err != nil {
				t.Fatalf("expected no error but got %q", err)
			}
			if len(tc.wantErrs) == 0 {
				return
			}

			if err == nil {
				t.Fatalf("got nil but expected errors: %+v", tc.wantErrs)
			}
			for _, wantErr := range tc.wantErrs {
				if !strings.Contains(err.Error(), wantErr) {
					t.Errorf("got error = %q, want it to contain %q", err, wantErr)
				}
			}
		})
	}
}
