// SPDX-License-Identifier:Apache-2.0

package groutdra

import (
	"encoding/json"
	"fmt"
	"os"

	"k8s.io/apimachinery/pkg/types"
)

// cdiSpec bind-mounts the vhost socket directory. Socket path is advertised
// via KEP-5304 device metadata (vhost-user-path), not CDI env vars.
type cdiSpec struct {
	Version string      `json:"cdiVersion"`
	Kind    string      `json:"kind"`
	Devices []cdiDevice `json:"devices"`
}

type cdiDevice struct {
	Name           string   `json:"name"`
	ContainerEdits cdiEdits `json:"containerEdits"`
}

type cdiEdits struct {
	Mounts []cdiMount `json:"mounts,omitempty"`
}

type cdiMount struct {
	HostPath      string   `json:"hostPath"`
	ContainerPath string   `json:"containerPath"`
	Options       []string `json:"options,omitempty"`
}

func writeCDISpec(paths pathLayout, claimUID types.UID, requestName, hostDir, containerDir string) (string, error) {
	if err := os.MkdirAll(paths.cdiDir, 0o755); err != nil {
		return "", fmt.Errorf("creating CDI dir: %w", err)
	}
	spec := cdiSpec{
		Version: "0.5.0",
		Kind:    CDIVendor + "/" + CDIClass,
		Devices: []cdiDevice{{
			Name: allocationID(claimUID, requestName),
			ContainerEdits: cdiEdits{
				Mounts: []cdiMount{
					{HostPath: hostDir, ContainerPath: containerDir, Options: []string{"rbind"}},
					// KubeVirt's vhostuser binding is evaluated in virt-launcher.
					// Mount the KEP-5304 metadata root alongside the socket directory.
					{HostPath: KubeVirtMetadataRoot, ContainerPath: KubeVirtMetadataRoot, Options: []string{"rbind", "ro"}},
				},
			},
		}},
	}
	data, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		return "", err
	}
	path := paths.CDISpecPath(claimUID, requestName)
	if err := writeAtomic(path, data, 0o644); err != nil {
		return "", fmt.Errorf("writing CDI spec: %w", err)
	}
	return CDIDeviceID(claimUID, requestName), nil
}

func removeCDISpec(paths pathLayout, claimUID types.UID, requestName string) error {
	err := os.Remove(paths.CDISpecPath(claimUID, requestName))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
