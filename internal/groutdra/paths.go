// SPDX-License-Identifier:Apache-2.0

package groutdra

import (
	"fmt"
	"path/filepath"
	"strings"

	"k8s.io/apimachinery/pkg/types"
)

const (
	DriverName = "grout.openperouter.io"
	PoolName   = "grout-vhost"
	DeviceClassName = "grout-vhostuser"

	// Align with kubevirt#19044 DefaultSocketFileName so quay.io/anbanerj/test-vhostuser-nbp:client
	// can join socketDir + "vhost.sock" without extra env.
	SocketFileName = "vhost.sock"
	HostVhostRoot  = "/var/run/grout-vhost"
	PodVhostDir    = "/var/run/grout-vhost"
	GroutSockPath  = "/var/run/grout/grout.sock"
	CDIDir         = "/var/run/cdi"
	CDIVendor      = "grout.openperouter.io"
	CDIClass       = "vhost"
	MaxDevices     = 8
	GuestMAC       = "52:54:00:67:72:01"
	QEMUUID        = 107
	QEMUGID        = 107

	EnvHostpathMountpoint = "KUBEVIRT_HOSTPATH_MOUNTPOINT"
	EnvHostpathSocket     = "KUBEVIRT_HOSTPATH_SOCKET"
	EnvVhostMode          = "KUBEVIRT_VHOSTUSER_MODE"
	EnvGroutSocket        = "GROUT_VHOST_SOCKET"
)

func ClaimDir(claimUID types.UID) string {
	return filepath.Join(HostVhostRoot, string(claimUID))
}

func SocketPath(claimUID types.UID) string {
	return filepath.Join(ClaimDir(claimUID), SocketFileName)
}

// PortName is a grout interface name unique per claim (DNS-ish, short).
func PortName(claimUID types.UID) string {
	id := strings.ReplaceAll(string(claimUID), "-", "")
	if len(id) > 8 {
		id = id[:8]
	}
	return "vhu" + id
}

func CDIDeviceID(claimUID types.UID) string {
	return fmt.Sprintf("%s/%s=%s", CDIVendor, CDIClass, claimUID)
}

func CDISpecPath(claimUID types.UID) string {
	return filepath.Join(CDIDir, fmt.Sprintf("%s-%s-%s.json", CDIVendor, CDIClass, claimUID))
}
