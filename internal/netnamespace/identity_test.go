// SPDX-License-Identifier:Apache-2.0

package netnamespace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsCurrent(t *testing.T) {
	alias := filepath.Join(t.TempDir(), "host-netns")
	if err := os.Symlink("/proc/self/ns/net", alias); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/proc/self/ns/net", alias} {
		got, err := IsCurrent(path)
		if err != nil || !got {
			t.Fatalf("IsCurrent(%q) = %v, %v; want true", path, got, err)
		}
	}
	if _, err := IsCurrent(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("missing namespace must return an error")
	}
}
