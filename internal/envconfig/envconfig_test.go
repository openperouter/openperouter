// SPDX-License-Identifier:Apache-2.0

package envconfig

import (
	"os"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestFromEnvironment(t *testing.T) {

	tests := []struct {
		desc        string
		setup       func()
		expected    EnvConfig
		expectedErr bool
	}{
		{
			desc: "basics",
			setup: func() {
				setBasics()
			},
			expected: EnvConfig{
				Namespace: "test-namespace",
				ControllerImage: ImageInfo{
					Repo: "test-controller-image",
					Tag:  "1",
				},
				FRRImage: ImageInfo{
					Repo: "test-frr-image",
					Tag:  "2",
				},
				KubeRBacImage: ImageInfo{
					Repo: "test-kube-rbac-proxy-image",
					Tag:  "3",
				},
				MetricsPort:    7472,
				FRRMetricsPort: 7473,
			},
		},
		{
			desc: "override ports",
			setup: func() {
				setBasics()
				_ = os.Setenv("DEPLOY_SERVICEMONITORS", "true")
				_ = os.Setenv("MEMBER_LIST_BIND_PORT", "1111")
				_ = os.Setenv("FRR_METRICS_PORT", "2222")
				_ = os.Setenv("FRR_HTTPS_METRICS_PORT", "3333")
				_ = os.Setenv("METRICS_PORT", "4444")
				_ = os.Setenv("HTTPS_METRICS_PORT", "5555")
			},
			expected: EnvConfig{
				Namespace: "test-namespace",
				ControllerImage: ImageInfo{
					Repo: "test-controller-image",
					Tag:  "1",
				},
				FRRImage: ImageInfo{
					Repo: "test-frr-image",
					Tag:  "2",
				},
				KubeRBacImage: ImageInfo{
					Repo: "test-kube-rbac-proxy-image",
					Tag:  "3",
				},
				MetricsPort:           4444,
				SecureMetricsPort:     5555,
				FRRMetricsPort:        2222,
				SecureFRRMetricsPort:  3333,
				DeployServiceMonitors: true,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			unset()
			test.setup()
			res, err := FromEnvironment(false)
			if err != nil && !test.expectedErr {
				t.Errorf("Unexpected error: %v", err)
			}
			if err == nil && test.expectedErr {
				t.Errorf("Expected error, got nil")
			}

			if res != test.expected {
				t.Errorf("res different from expected, %s", cmp.Diff(res, test.expected))
			}

		})
	}

}

func unset() {
	_ = os.Unsetenv("OPERATOR_NAMESPACE")
	_ = os.Unsetenv("CONTROLLER_IMAGE")
	_ = os.Unsetenv("FRR_IMAGE")
	_ = os.Unsetenv("KUBE_RBAC_PROXY_IMAGE")
	_ = os.Unsetenv("FRR_METRICS_PORT")
	_ = os.Unsetenv("FRR_HTTPS_METRICS_PORT")
	_ = os.Unsetenv("METRICS_PORT")
	_ = os.Unsetenv("HTTPS_METRICS_PORT")
	_ = os.Unsetenv("DEPLOY_PODMONITORS")
	_ = os.Unsetenv("DEPLOY_SERVICEMONITORS")
	_ = os.Unsetenv("KUBE_RBAC_PROXY_IMAGE")
}

func setBasics() {
	_ = os.Setenv("OPERATOR_NAMESPACE", "test-namespace")
	_ = os.Setenv("CONTROLLER_IMAGE", "test-controller-image:1")
	_ = os.Setenv("FRR_IMAGE", "test-frr-image:2")
	_ = os.Setenv("KUBE_RBAC_PROXY_IMAGE", "test-kube-rbac-proxy-image:3")
}
