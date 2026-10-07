// SPDX-License-Identifier:Apache-2.0

package routerconfiguration

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	"k8s.io/apimachinery/pkg/util/sets"

	"github.com/openperouter/openperouter/internal/hostnetwork"
)

func TestSetupL2VNIs(t *testing.T) {
	// SetupL2VNI fails for every L2VNI, because the target namespace does not exist.
	missingNS := filepath.Join(t.TempDir(), "missing")
	red := hostnetwork.L2VNIParams{
		VNIParams: hostnetwork.VNIParams{VRF: "red", TargetNS: missingNS, VNI: 100},
		Name:      "red",
	}
	blue := hostnetwork.L2VNIParams{
		VNIParams: hostnetwork.VNIParams{VRF: "blue", TargetNS: missingNS, VNI: 200},
		Name:      "blue",
	}

	tests := []struct {
		name            string
		failedL3Domains sets.Set[string]
		wantKept        []hostnetwork.L2VNIParams
	}{
		{
			name:            "L2VNIs whose setup fails are kept",
			failedL3Domains: sets.New[string](),
			wantKept:        []hostnetwork.L2VNIParams{red, blue},
		},
		{
			name:            "L2VNIs of a failed L3 domain are not kept",
			failedL3Domains: sets.New("red"),
			wantKept:        []hostnetwork.L2VNIParams{blue},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			kept, errs := setupL2VNIs(context.Background(), []hostnetwork.L2VNIParams{red, blue}, tc.failedL3Domains)
			if diff := cmp.Diff(tc.wantKept, kept); diff != "" {
				t.Errorf("kept L2VNIs mismatch (-want +got):\n%s", diff)
			}
			if len(errs) != 2 {
				t.Errorf("expected an error for each L2VNI, got %v", errs)
			}
		})
	}
}
