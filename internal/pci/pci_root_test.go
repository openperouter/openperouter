// SPDX-License-Identifier:Apache-2.0

//go:build runasroot

package pci

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
)

func TestPCIAddressForVFByPFAltName(t *testing.T) {
	const (
		pfName      = "pfprimary0"
		pfAltName   = "trunkpf"
		pfLongName  = "trunkpf-alternative-name"
		vfPCIAddr   = "0000:03:00.2"
		emulatedPCI = "0000:09:00.0"
	)
	tests := []struct {
		name     string
		emulated string
		want     string
	}{
		{name: "real VF", want: vfPCIAddr},
		{name: "emulated VF", emulated: "true", want: emulatedPCI},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(emulatedVFsEnv, tt.emulated)
			defer inPCITestNetns(t)()

			pf := &netlink.Dummy{LinkAttrs: netlink.LinkAttrs{Name: pfName}}
			require.NoError(t, netlink.LinkAdd(pf))
			require.NoError(t, netlink.LinkAddAltName(pf, pfAltName))
			require.NoError(t, netlink.LinkAddAltName(pf, pfLongName))

			sysfs := Sysfs{Root: t.TempDir()}
			deviceDir := filepath.Join(sysfs.Root, "class", "net", pfName, "device")
			require.NoError(t, os.MkdirAll(deviceDir, 0o755))
			require.NoError(t, os.Symlink("../"+vfPCIAddr, filepath.Join(deviceDir, "virtfn1")))

			vfNetDir := filepath.Join(sysfs.Root, "class", "net", pfName+"v1")
			require.NoError(t, os.MkdirAll(vfNetDir, 0o755))
			require.NoError(t, os.MkdirAll(filepath.Join(sysfs.Root, "devices", emulatedPCI, "virtio0"), 0o755))
			require.NoError(t, os.Symlink("../../../devices/"+emulatedPCI+"/virtio0", filepath.Join(vfNetDir, "device")))

			for _, name := range []string{pfName, pfAltName, pfLongName} {
				got, err := sysfs.PCIAddressForVF(name, 1)
				assert.NoError(t, err, name)
				assert.Equal(t, tt.want, got, name)
			}
		})
	}
}

func inPCITestNetns(t *testing.T) func() {
	t.Helper()

	runtime.LockOSThread()
	original, err := netns.Get()
	if err != nil {
		runtime.UnlockOSThread()
		t.Fatal(err)
	}
	isolated, err := netns.New()
	if err != nil {
		assert.NoError(t, original.Close())
		runtime.UnlockOSThread()
		t.Fatal(err)
	}
	return func() {
		assert.NoError(t, netns.Set(original))
		assert.NoError(t, isolated.Close())
		assert.NoError(t, original.Close())
		runtime.UnlockOSThread()
	}
}
