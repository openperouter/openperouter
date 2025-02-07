// SPDX-License-Identifier:Apache-2.0

package pods

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	cri "k8s.io/cri-api/pkg/apis/runtime/v1"
	"k8s.io/kubelet/pkg/types"

	runtimespec "github.com/opencontainers/runtime-spec/specs-go"
)

// InfoKey if the key for PodSandboxStatusInfo in the Info map of the PodSandboxStatusResponse
// cri-o: https://github.com/cri-o/cri-o/blob/v1.29.2/server/sandbox_status.go#L114
// containerd: https://github.com/containerd/containerd/blob/v1.7.14/pkg/cri/server/sandbox_status.go#L215
// containerd v2: https://github.com/containerd/containerd/blob/v2.0.0-beta.2/pkg/cri/server/sandbox_status.go#L183
const InfoKey = "info"

// PodSandboxStatusInfo represents the value in the Info map of the PodSandboxStatusResponse with InfoKey as key
// cri-o: https://github.com/cri-o/cri-o/blob/v1.29.2/server/sandbox_status.go#L103
// containerd: https://github.com/containerd/containerd/blob/v1.7.14/pkg/cri/server/sandbox_status.go#L139
// containerd v2: https://github.com/containerd/containerd/blob/v2.0.0-beta.2/pkg/cri/types/sandbox_info.go#L44
type PodSandboxStatusInfo struct {
	RuntimeSpec *runtimespec.Spec `json:"runtimeSpec"`
}

type podSandboxer interface {
	PodSandboxStatus(ctx context.Context, in *cri.PodSandboxStatusRequest, opts ...grpc.CallOption) (*cri.PodSandboxStatusResponse, error)
	ListPodSandbox(ctx context.Context, in *cri.ListPodSandboxRequest, opts ...grpc.CallOption) (*cri.ListPodSandboxResponse, error)
}

// Runtime represents a connection to the CRI-O runtime
type Runtime struct {
	Client podSandboxer
}

// New returns a connection to the CRI runtime
func NewRuntime(socketPath string, timeout time.Duration) (*Runtime, error) {
	if socketPath == "" {
		return nil, fmt.Errorf("path to CRI socket missing")
	}

	clientConnection, err := connect(socketPath, timeout)
	if err != nil {
		return nil, fmt.Errorf("error establishing connection to CRI: %w", err)
	}

	return &Runtime{
		Client: cri.NewRuntimeServiceClient(clientConnection),
	}, nil
}

func (r *Runtime) NetworkNamespace(ctx context.Context, podUID string) (string, error) {
	podSandboxID, err := r.podSandboxID(ctx, podUID)
	if err != nil {
		return "", err
	}

	podSandboxStatus, err := r.Client.PodSandboxStatus(ctx, &cri.PodSandboxStatusRequest{
		PodSandboxId: podSandboxID,
		Verbose:      true,
	})
	if err != nil || podSandboxStatus == nil {
		return "", fmt.Errorf("failed to PodSandboxStatus for PodSandboxId %s: %w", podSandboxID, err)
	}

	sandboxInfo := &PodSandboxStatusInfo{}

	err = json.Unmarshal([]byte(podSandboxStatus.Info[InfoKey]), sandboxInfo)
	if err != nil {
		return "", fmt.Errorf("failed to Unmarshal podSandboxStatus.Info['%s']: %w", InfoKey, err)
	}

	networkNamespace := ""

	for _, namespace := range sandboxInfo.RuntimeSpec.Linux.Namespaces {
		if namespace.Type != runtimespec.NetworkNamespace {
			continue
		}

		_, networkNamespace = path.Split(namespace.Path)
		break
	}

	if networkNamespace == "" {
		return "", fmt.Errorf("failed to find network namespace for PodSandboxId %s: %w", podSandboxID, err)
	}

	return networkNamespace, nil
}

func (r *Runtime) podSandboxID(ctx context.Context, podUID string) (string, error) {
	// Labels used by Kubernetes: https://github.com/kubernetes/kubernetes/blob/v1.29.2/staging/src/k8s.io/kubelet/pkg/types/labels.go#L19
	podSandbox, err := r.Client.ListPodSandbox(ctx, &cri.ListPodSandboxRequest{
		Filter: &cri.PodSandboxFilter{
			LabelSelector: map[string]string{
				types.KubernetesPodUIDLabel: podUID,
			},
		},
	})
	if err != nil {
		return "", fmt.Errorf("failed to ListPodSandbox for pod %s: %w", podUID, err)
	}

	if podSandbox == nil || podSandbox.Items == nil || len(podSandbox.Items) == 0 {
		return "", fmt.Errorf("ListPodSandbox returned 0 item for pod %s", podUID)
	}

	if len(podSandbox.Items) > 1 {
		return "", fmt.Errorf("ListPodSandbox returned more than 1 item for pod %s", podUID)
	}

	return podSandbox.Items[0].Id, nil
}

func connect(socketPath string, timeout time.Duration) (*grpc.ClientConn, error) {
	if socketPath == "" {
		return nil, fmt.Errorf("endpoint is not set")
	}

	ctx, cancelFn := context.WithTimeout(context.Background(), timeout)
	defer cancelFn()
	conn, err := grpc.DialContext(
		ctx,
		criServerAddress(socketPath),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		return nil, fmt.Errorf("error connecting to endpoint '%s': %v", socketPath, err)
	}
	return conn, nil
}

func criServerAddress(criSocketPath string) string {
	return fmt.Sprintf("unix://%s", criSocketPath)
}
