// SPDX-License-Identifier:Apache-2.0

package groutdra

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"

	"k8s.io/apimachinery/pkg/types"
)

const (
	DriverName = "grout.openperouter.io"
	PoolName   = "grout-vhost"

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
	PodVhostRoot  = HostVhostRoot
	GroutSockPath = "/var/run/grout/grout.sock"
	CDIDir        = "/var/run/cdi"
	CDIVendor     = "grout.openperouter.io"
	CDIClass      = "vhost"
	QEMUUID       = 107
	QEMUGID       = 107

	// VhostPathAttr is the KEP-5304 attribute kubevirt/vhostuser-network-binding-plugin
	// reads from DRA device metadata (same key as ovsdpdk.k8snetworkplumbingwg.io).
	VhostPathAttr = "vhost-user-path"
	VhostMTU      = 9000
)

type pathLayout struct {
	hostRoot     string
	cdiDir       string
	metadataRoot string
	pluginRoot   string
}

var defaultPaths = pathLayout{HostVhostRoot, CDIDir, KubeVirtMetadataRoot, PluginDataRoot}

func ClaimDir(claimUID types.UID, requestName string) string {
	return defaultPaths.ClaimDir(claimUID, requestName)
}

func (p pathLayout) ClaimDir(claimUID types.UID, requestName string) string {
	if requestName == "" {
		requestName = "vhu"
	}
	return filepath.Join(p.hostRoot, string(claimUID), requestName)
}

func SocketPath(claimUID types.UID, requestName string) string {
	return defaultPaths.SocketPath(claimUID, requestName)
}

func (p pathLayout) SocketPath(claimUID types.UID, requestName string) string {
	return filepath.Join(p.ClaimDir(claimUID, requestName), SocketFileName)
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
func PortName(claimUID types.UID, requestName string) string {
	return "vhu" + allocationID(claimUID, requestName)
}

func CDIDeviceID(claimUID types.UID, requestName string) string {
	return fmt.Sprintf("%s/%s=%s", CDIVendor, CDIClass, allocationID(claimUID, requestName))
}

func CDISpecPath(claimUID types.UID, requestName string) string {
	return defaultPaths.CDISpecPath(claimUID, requestName)
}

func (p pathLayout) CDISpecPath(claimUID types.UID, requestName string) string {
	return filepath.Join(p.cdiDir, fmt.Sprintf("%s-%s-%s.json", CDIVendor, CDIClass, allocationID(claimUID, requestName)))
}

func allocationID(claimUID types.UID, requestName string) string {
	sum := sha256.Sum256([]byte(string(claimUID) + "\x00" + requestName))
	return hex.EncodeToString(sum[:6])
}

func ClaimStatePath(claimUID types.UID) string {
	return defaultPaths.ClaimStatePath(claimUID)
}

func (p pathLayout) ClaimStatePath(claimUID types.UID) string {
	return filepath.Join(p.hostRoot, string(claimUID), "state.json")
}

// MetadataSourcePath is the kubeletplugin-owned metadata stream for a claim.
func MetadataSourcePath(namespace, claimName, requestName string) string {
	return defaultPaths.MetadataSourcePath(namespace, claimName, requestName)
}

func (p pathLayout) MetadataSourcePath(namespace, claimName, requestName string) string {
	return filepath.Join(p.pluginRoot, "dra-device-metadata", namespace+"_"+claimName, requestName, "metadata.json")
}

// KubeVirtMetadataProjectionPath is the KEP-5304 path that KubeVirt uses for
// a claim created from a ResourceClaimTemplate. podClaimName is the name in
// pod.spec.resourceClaims, not the generated ResourceClaim name.
func KubeVirtMetadataProjectionPath(podClaimName, requestName string) string {
	return defaultPaths.KubeVirtMetadataProjectionPath(podClaimName, requestName)
}

func (p pathLayout) KubeVirtMetadataProjectionPath(podClaimName, requestName string) string {
	return filepath.Join(p.metadataRoot, "resourceclaimtemplates", podClaimName, requestName, DriverName+"-metadata.json")
}
