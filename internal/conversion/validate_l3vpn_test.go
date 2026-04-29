// SPDX-License-Identifier:Apache-2.0

package conversion

import (
	"strings"
	"testing"

	"github.com/openperouter/openperouter/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestFilterUniqueVRFsForL3VPN_DuplicateVRF(t *testing.T) {
	l3vpns := []v1alpha1.L3VPN{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "vni1", Namespace: "test"},
			Spec: v1alpha1.L3VPNSpec{
				RDAssignedNumber: 1001,
				VRF:              "vni1",
				HostSession:      &v1alpha1.HostSession{ASN: 65001, HostASN: new(int64(65002)), LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: new("192.168.1.0/24")}},
			},
			Status: &v1alpha1.L3VPNStatus{},
		},
		{
			ObjectMeta: metav1.ObjectMeta{Name: "vni2", Namespace: "test"},
			Spec: v1alpha1.L3VPNSpec{
				RDAssignedNumber: 1002,
				HostSession:      &v1alpha1.HostSession{ASN: 65003, HostASN: new(int64(65004)), LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: new("192.168.2.0/24")}},
				VRF:              "vni1",
			},
			Status: &v1alpha1.L3VPNStatus{},
		},
	}

	valid, _, err := FilterUniqueVRFsForL3VPNs(l3vpns)
	if err == nil {
		t.Fatal("expected error for duplicate VRF")
	}
	wantErr := "more than one L3VPN detected in VRF \"vni1\": \"test/vni1\" already exists"
	if !strings.Contains(err.Error(), wantErr) {
		t.Errorf("error = %q, want %q", err, wantErr)
	}
	if len(valid) != 1 || valid[0].Name != "vni1" {
		t.Errorf("expected first L3VPN to be valid, got %v", valid)
	}
}
