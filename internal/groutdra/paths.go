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
	// PluginDataRoot is where kubeletplugin persists the authoritative KEP-5304
	// metadata stream for this driver.
	PluginDataRoot = "/var/lib/kubelet/plugins/" + DriverName
	// KubeVirtMetadataRoot is the host path which virt-handler uses for DRA
	// metadata. Kubelet normally projects this into workload containers, but
	// virt-handler consumes it before the launcher exists.
	KubeVirtMetadataRoot = "/var/run/kubernetes.io/dra-device-attributes"
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

// MetadataSourcePath is the kubeletplugin-owned metadata stream for a claim.
func MetadataSourcePath(namespace, claimName, requestName string) string {
	return filepath.Join(PluginDataRoot, "dra-device-metadata", namespace+"_"+claimName, requestName, "metadata.json")
}

// KubeVirtMetadataProjectionPath is the KEP-5304 path that KubeVirt uses for
// a claim created from a ResourceClaimTemplate. podClaimName is the name in
// pod.spec.resourceClaims, not the generated ResourceClaim name.
func KubeVirtMetadataProjectionPath(podClaimName, requestName string) string {
	return filepath.Join(KubeVirtMetadataRoot, "resourceclaimtemplates", podClaimName, requestName, DriverName+"-metadata.json")
}
