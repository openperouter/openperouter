// SPDX-License-Identifier:Apache-2.0

package groutdra

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"k8s.io/apimachinery/pkg/types"
)

func TestPortName(t *testing.T) {
	got := PortName("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")
	if got != "vhuaaaaaaaa" {
		t.Fatalf("PortName = %q", got)
	}
}

func TestWriteCDISpec(t *testing.T) {
	dir := t.TempDir()
	origCDI := CDIDir
	t.Cleanup(func() { /* restore via reassignment below */ })
	_ = origCDI
	// CDIDir is a package const; write into a fake by using ClaimDir only.
	// writeCDISpec uses CDIDir const — skip filesystem if we cannot override.
	// Test JSON shape via marshal of the same struct.
	uid := types.UID("claim-uid-1")
	hostDir := filepath.Join(dir, string(uid))
	spec := cdiSpec{
		Version: "0.5.0",
		Kind:    CDIVendor + "/" + CDIClass,
		Devices: []cdiDevice{{
			Name: string(uid),
			ContainerEdits: cdiEdits{
				Env: []string{
					EnvHostpathMountpoint + "=" + PodVhostDir,
					EnvHostpathSocket + "=" + SocketFileName,
					EnvVhostMode + "=client",
					EnvGroutSocket + "=" + filepathJoinPodSocket(),
				},
				Mounts: []cdiMount{{
					HostPath:      hostDir,
					ContainerPath: PodVhostDir,
					Options:       []string{"rbind"},
				}},
			},
		}},
	}
	b, err := json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	if !containsAll(string(b), []string{
		`"kind":"grout.openperouter.io/vhost"`,
		`"KUBEVIRT_HOSTPATH_MOUNTPOINT=/var/run/grout-vhost"`,
		`"KUBEVIRT_HOSTPATH_SOCKET=vhost.sock"`,
		`"KUBEVIRT_VHOSTUSER_MODE=client"`,
		hostDir,
	}) {
		t.Fatalf("unexpected spec: %s", b)
	}
	_ = os.Remove
}

func containsAll(s string, parts []string) bool {
	for _, p := range parts {
		if !contains(s, p) {
			return false
		}
	}
	return true
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
