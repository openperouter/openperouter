// SPDX-License-Identifier:Apache-2.0

package testutils

import (
	"testing"

	"github.com/onsi/ginkgo/v2"
	"golang.org/x/sys/unix"
)

const (
	capSysAdmin    = 21 // CAP_SYS_ADMIN capability bit
	skipPrivileged = "Skipping test: requires CAP_SYS_ADMIN (run as root or with sudo)"
)

// HasCapSysAdmin checks if the current process has CAP_SYS_ADMIN capability,
// which is required to create network namespaces and perform other privileged operations.
func HasCapSysAdmin() bool {
	hdr := unix.CapUserHeader{
		Version: unix.LINUX_CAPABILITY_VERSION_3,
		Pid:     0, // 0 means current process
	}
	var data [2]unix.CapUserData

	if err := unix.Capget(&hdr, &data[0]); err != nil {
		return false
	}

	// CAP_SYS_ADMIN is capability 21, which falls in the first 32-bit word
	return (data[0].Effective & (1 << capSysAdmin)) != 0
}

// SkipUnlessPrivileged skips the current Ginkgo test if CAP_SYS_ADMIN is not available.
func SkipUnlessPrivileged() {
	if !HasCapSysAdmin() {
		ginkgo.Skip(skipPrivileged)
	}
}

// SkipUnlessPrivilegedT skips the current standard Go test if CAP_SYS_ADMIN is not available.
func SkipUnlessPrivilegedT(t *testing.T) {
	t.Helper()
	if !HasCapSysAdmin() {
		t.Skip(skipPrivileged)
	}
}
