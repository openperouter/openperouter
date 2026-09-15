// SPDX-License-Identifier:Apache-2.0

package groutdra

import (
	"encoding/json"
	"fmt"
	"os"

	"k8s.io/apimachinery/pkg/types"
)

// cdiSpec is a minimal CDI 0.5 spec (same shape as kubevirt#18444 hostpath.go).
type cdiSpec struct {
	Version string      `json:"cdiVersion"`
	Kind    string      `json:"kind"`
	Devices []cdiDevice `json:"devices"`
}

type cdiDevice struct {
	Name           string         `json:"name"`
	ContainerEdits cdiEdits       `json:"containerEdits"`
}

type cdiEdits struct {
	Env    []string   `json:"env,omitempty"`
	Mounts []cdiMount `json:"mounts,omitempty"`
}

type cdiMount struct {
	HostPath      string   `json:"hostPath"`
	ContainerPath string   `json:"containerPath"`
	Options       []string `json:"options,omitempty"`
}

func writeCDISpec(claimUID types.UID, hostDir string) (string, error) {
	if err := os.MkdirAll(CDIDir, 0o755); err != nil {
		return "", fmt.Errorf("creating CDI dir: %w", err)
	}
	spec := cdiSpec{
		Version: "0.5.0",
		Kind:    CDIVendor + "/" + CDIClass,
		Devices: []cdiDevice{{
			Name: string(claimUID),
			ContainerEdits: cdiEdits{
				Env: []string{
					EnvHostpathMountpoint + "=" + PodVhostDir,
					EnvHostpathSocket + "=" + SocketFileName,
					EnvVhostMode + "=" + "client",
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
	data, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		return "", err
	}
	path := CDISpecPath(claimUID)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", fmt.Errorf("writing CDI spec: %w", err)
	}
	return CDIDeviceID(claimUID), nil
}

func filepathJoinPodSocket() string {
	return PodVhostDir + "/" + SocketFileName
}

func removeCDISpec(claimUID types.UID) {
	_ = os.Remove(CDISpecPath(claimUID))
}
