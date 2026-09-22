// SPDX-License-Identifier:Apache-2.0

package groutdra

import (
	"fmt"
	"path/filepath"
	"strings"

	"k8s.io/apimachinery/pkg/types"
)

const (
	DriverName      = "grout.openperouter.io"
	PoolName        = "grout-vhost"
	DeviceClassName = "grout-vhostuser"

	SocketFileName = "vhost.sock"
	HostVhostRoot  = "/var/run/grout-vhost"
	// PodVhostRoot is the container mount root. Per-request sockets land at
	// PodVhostRoot/<requestName>/vhost.sock (same shape as ovsdpdk DRA).
	PodVhostRoot  = "/var/run/grout-vhost"
	GroutSockPath = "/var/run/grout/grout.sock"
	CDIDir        = "/var/run/cdi"
	CDIVendor     = "grout.openperouter.io"
	CDIClass      = "vhost"
	MaxDevices    = 8
	QEMUUID       = 107
	QEMUGID       = 107

	// VhostPathAttr is the KEP-5304 attribute kubevirt/vhostuser-network-binding-plugin
	// reads from DRA device metadata (same key as ovsdpdk.k8snetworkplumbingwg.io).
	VhostPathAttr = "vhost-user-path"
)

func ClaimDir(claimUID types.UID, requestName string) string {
	if requestName == "" {
		requestName = "vhu"
	}
	return filepath.Join(HostVhostRoot, string(claimUID), requestName)
}

func SocketPath(claimUID types.UID, requestName string) string {
	return filepath.Join(ClaimDir(claimUID, requestName), SocketFileName)
}

func PodSocketPath(requestName string) string {
	if requestName == "" {
		requestName = "vhu"
	}
	return filepath.Join(PodVhostRoot, requestName, SocketFileName)
}

func PodMountPath(requestName string) string {
	if requestName == "" {
		requestName = "vhu"
	}
	return filepath.Join(PodVhostRoot, requestName)
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
