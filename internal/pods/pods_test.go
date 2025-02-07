// SPDX-License-Identifier:Apache-2.0

package pods

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	runtimespec "github.com/opencontainers/runtime-spec/specs-go"
	"google.golang.org/grpc"
	cri "k8s.io/cri-api/pkg/apis/runtime/v1"
)

type mockCRI struct {
	listPodSandboxRes      *cri.ListPodSandboxResponse
	shouldErrList          bool
	podSandboxStatusRes    *cri.PodSandboxStatusResponse
	shouldErrSandboxStatus bool
}

func (m mockCRI) PodSandboxStatus(ctx context.Context, in *cri.PodSandboxStatusRequest, opts ...grpc.CallOption) (*cri.PodSandboxStatusResponse, error) {
	if m.shouldErrSandboxStatus {
		return nil, errors.New("failed to sandbox status")
	}
	return m.podSandboxStatusRes, nil
}

func (m mockCRI) ListPodSandbox(ctx context.Context, in *cri.ListPodSandboxRequest, opts ...grpc.CallOption) (*cri.ListPodSandboxResponse, error) {
	if m.shouldErrList {
		return nil, errors.New("failed to list sandbox")
	}
	return m.listPodSandboxRes, nil
}

func TestNamespaceForPod(t *testing.T) {
	validNamespaceJson, err := json.Marshal(PodSandboxStatusInfo{
		RuntimeSpec: &runtimespec.Spec{
			Linux: &runtimespec.Linux{
				Namespaces: []runtimespec.LinuxNamespace{
					{
						Type: runtimespec.NetworkNamespace,
						Path: "myNamespace",
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("failed to marshal valid namespace")
	}
	// invalidNamespace := `"foo" : "bar"}`

	tests := []struct {
		name          string
		mock          mockCRI
		expectedError string
		expectedNS    string
	}{
		{
			name: "plain namespace",
			mock: mockCRI{
				listPodSandboxRes: &cri.ListPodSandboxResponse{
					Items: []*cri.PodSandbox{
						{
							Id: "MyID",
						},
					},
				},
				podSandboxStatusRes: &cri.PodSandboxStatusResponse{
					Info: map[string]string{
						InfoKey: string(validNamespaceJson),
					},
				},
			},
			expectedNS: "myNamespace",
		},
		{
			name: "list with multiple results",
			mock: mockCRI{
				listPodSandboxRes: &cri.ListPodSandboxResponse{
					Items: []*cri.PodSandbox{
						{
							Id: "MyID",
						}, {
							Id: "MyID1",
						},
					},
				},
			},
			expectedError: "more than 1 item",
		},
		{
			name: "list no results",
			mock: mockCRI{
				listPodSandboxRes: &cri.ListPodSandboxResponse{
					Items: []*cri.PodSandbox{},
				},
			},
			expectedError: "returned 0 item",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := Runtime{tc.mock}
			namespace, err := r.NetworkNamespace(context.Background(), "podUID")
			if err != nil && !strings.Contains(err.Error(), tc.expectedError) {
				t.Fatalf("got error %v, expecting %s", err, tc.expectedError)
			}
			if err == nil && tc.expectedError != "" {
				t.Fatalf("expecting %s, got nil error", tc.expectedError)
			}
			if namespace != tc.expectedNS {
				t.Fatalf("expecting ns %s, got %s", tc.expectedNS, namespace)
			}

		})
	}

}
