// SPDX-License-Identifier:Apache-2.0

package groutdra

import "testing"

func TestKubeVirtMetadataPaths(t *testing.T) {
	if got, want := MetadataSourcePath("default", "generated-claim", "vhu"), "/var/lib/kubelet/plugins/grout.openperouter.io/dra-device-metadata/default_generated-claim/vhu/metadata.json"; got != want {
		t.Fatalf("MetadataSourcePath() = %q, want %q", got, want)
	}
	if got, want := KubeVirtMetadataProjectionPath("grout-vhu", "vhu"), "/var/run/kubernetes.io/dra-device-attributes/resourceclaimtemplates/grout-vhu/vhu/grout.openperouter.io-metadata.json"; got != want {
		t.Fatalf("KubeVirtMetadataProjectionPath() = %q, want %q", got, want)
	}
}
