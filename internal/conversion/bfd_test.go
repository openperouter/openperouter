// SPDX-License-Identifier:Apache-2.0

package conversion

import (
	"testing"

	"github.com/openperouter/openperouter/api/v1alpha1"
	"k8s.io/utils/ptr"
)

func TestBFDConversion(t *testing.T) {
	nodeIndex := 0

	t.Run("BFD with custom settings", func(t *testing.T) {
		underlays := []v1alpha1.Underlay{
			{
				Spec: v1alpha1.UnderlaySpec{
					ASN:      65000,
					VTEPCIDR: "192.168.1.0/24",
					Neighbors: []v1alpha1.Neighbor{
						{
							Address: "192.168.1.100",
							ASN:     65001,
							BFD: &v1alpha1.BFDSettings{
								ReceiveInterval:  ptr.To(uint32(300)),
								TransmitInterval: ptr.To(uint32(300)),
								DetectMultiplier: ptr.To(uint32(3)),
								EchoMode:         ptr.To(false),
								PassiveMode:      ptr.To(false),
							},
						},
					},
				},
			},
		}
		vnis := []v1alpha1.L3VNI{}
		logLevel := "debug"

		result, err := APItoFRR(nodeIndex, underlays, vnis, logLevel)
		if err != nil {
			t.Fatalf("APItoFRR failed: %v", err)
		}

		// Check that the neighbor has BFD profile
		neighbor := result.Underlay.Neighbors[0]
		if neighbor.BFDProfile == "" {
			t.Errorf("Expected neighbor to have BFD profile, but it's empty")
		}
		if neighbor.BFDEnabled {
			t.Errorf("Expected neighbor to not have BFDEnabled=true when using profile")
		}

		// Check that we have exactly one BFD profile in the config
		if len(result.BFDProfiles) != 1 {
			t.Fatalf("Expected 1 BFD profile, got %d", len(result.BFDProfiles))
		}

		bfdProfile := result.BFDProfiles[0]
		if bfdProfile.Name != "neighbor-192.168.1.100" {
			t.Errorf("Expected BFD profile name 'neighbor-192.168.1.100', got '%s'", bfdProfile.Name)
		}
		if bfdProfile.ReceiveInterval == nil || *bfdProfile.ReceiveInterval != 300 {
			t.Errorf("Expected receive interval 300, got %v", bfdProfile.ReceiveInterval)
		}
	})

	t.Run("BFD enabled without settings", func(t *testing.T) {
		underlays := []v1alpha1.Underlay{
			{
				Spec: v1alpha1.UnderlaySpec{
					ASN:      65000,
					VTEPCIDR: "192.168.1.0/24",
					Neighbors: []v1alpha1.Neighbor{
						{
							Address: "192.168.1.100",
							ASN:     65001,
							BFD:     &v1alpha1.BFDSettings{}, // Empty settings
						},
					},
				},
			},
		}
		vnis := []v1alpha1.L3VNI{}
		logLevel := "debug"

		result, err := APItoFRR(nodeIndex, underlays, vnis, logLevel)
		if err != nil {
			t.Fatalf("APItoFRR failed: %v", err)
		}

		// Check that the neighbor has BFD enabled but no profile
		neighbor := result.Underlay.Neighbors[0]
		if !neighbor.BFDEnabled {
			t.Errorf("Expected neighbor to have BFDEnabled=true")
		}
		if neighbor.BFDProfile != "" {
			t.Errorf("Expected neighbor to not have BFD profile, but got '%s'", neighbor.BFDProfile)
		}

		// Check that we have no BFD profiles in the config
		if len(result.BFDProfiles) != 0 {
			t.Fatalf("Expected 0 BFD profiles, got %d", len(result.BFDProfiles))
		}
	})
}
