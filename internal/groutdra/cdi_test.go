// SPDX-License-Identifier:Apache-2.0

package groutdra

import (
	"encoding/json"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/types"
)

func TestPortName(t *testing.T) {
	got := PortName("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")
	if got != "vhuaaaaaaaa" {
		t.Fatalf("PortName = %q", got)
	}
}

func TestPodSocketPath(t *testing.T) {
	got := PodSocketPath("vhu")
	if got != "/var/run/grout-vhost/vhu/vhost.sock" {
		t.Fatalf("PodSocketPath = %q", got)
	}
}

func TestCDISpecHasMountNoEnv(t *testing.T) {
	uid := types.UID("claim-uid-1")
	hostDir := "/var/run/grout-vhost/" + string(uid) + "/vhu"
	spec := cdiSpec{
		Version: "0.5.0",
		Kind:    CDIVendor + "/" + CDIClass,
		Devices: []cdiDevice{{
			Name: string(uid),
			ContainerEdits: cdiEdits{
				Mounts: []cdiMount{{
					HostPath:      hostDir,
					ContainerPath: PodMountPath("vhu"),
					Options:       []string{"rbind"},
				}},
			},
		}},
	}
	b, err := json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.Contains(s, `"kind":"grout.openperouter.io/vhost"`) {
		t.Fatalf("unexpected spec: %s", b)
	}
	if strings.Contains(s, "KUBEVIRT_") || strings.Contains(s, `"env"`) {
		t.Fatalf("CDI must not inject sidecar env vars: %s", b)
	}
	if !strings.Contains(s, hostDir) || !strings.Contains(s, "/var/run/grout-vhost/vhu") {
		t.Fatalf("missing mount paths: %s", b)
	}
}
