// SPDX-License-Identifier:Apache-2.0

package conversion

import (
	"testing"

	"github.com/openperouter/openperouter/api/v1alpha1"
)

func TestBFDProfileSessionMode(t *testing.T) {
	tests := []struct {
		name            string
		sessionMode     *v1alpha1.BFDSessionMode
		wantPassiveMode bool
	}{
		{name: "unset defaults to active", sessionMode: nil, wantPassiveMode: false},
		{name: "active", sessionMode: new(v1alpha1.BFDSessionModeActive), wantPassiveMode: false},
		{name: "passive", sessionMode: new(v1alpha1.BFDSessionModePassive), wantPassiveMode: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			n := v1alpha1.Neighbor{
				Address: new("192.168.1.1"),
				BFD: &v1alpha1.BFDSettings{
					SessionMode:     tc.sessionMode,
					ReceiveInterval: new(int32(300)),
				},
			}
			profile := bfdProfileForNeighbor(n)
			if profile == nil {
				t.Fatal("expected a BFD profile, got nil")
				return
			}
			if profile.PassiveMode != tc.wantPassiveMode {
				t.Errorf("PassiveMode = %v, want %v", profile.PassiveMode, tc.wantPassiveMode)
			}
		})
	}
}

func TestBFDProfileNilForEmptySettings(t *testing.T) {
	n := v1alpha1.Neighbor{Address: new("192.168.1.1"), BFD: &v1alpha1.BFDSettings{}}
	if profile := bfdProfileForNeighbor(n); profile != nil {
		t.Errorf("expected no BFD profile for empty settings, got %+v", profile)
	}
}
