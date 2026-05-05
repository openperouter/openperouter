// SPDX-License-Identifier:Apache-2.0

package hostcredentials

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadCredentials(t *testing.T) {
	tests := []struct {
		name          string
		path          string
		wantErr       bool
		expectedCreds Credentials
	}{
		{
			name:    "valid credentials",
			path:    "testdata/valid",
			wantErr: false,
			expectedCreds: Credentials{
				token:     "token",
				ca:        "ca.crt",
				namespace: "namespace",
			},
		},
		{
			name:    "missing token file",
			path:    "testdata/missing_token",
			wantErr: true,
		},
		{
			name:    "missing ca file",
			path:    "testdata/missing_ca",
			wantErr: true,
		},
		{
			name:    "missing namespace file",
			path:    "testdata/missing_namespace",
			wantErr: true,
		},
		{
			name:    "non-existent directory",
			path:    "testdata/nonexistent",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			creds, err := ReadCredentials(tt.path)

			if tt.wantErr {
				if err == nil {
					t.Errorf("ReadCredentials() error = nil, wantErr %v", tt.wantErr)
				}
				return
			}

			if err != nil {
				t.Errorf("ReadCredentials() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if creds.token != tt.expectedCreds.token {
				t.Errorf("ReadCredentials() token = %v, want %v", creds.token, tt.expectedCreds.token)
			}

			if creds.ca != tt.expectedCreds.ca {
				t.Errorf("ReadCredentials() ca = %v, want %v", creds.ca, tt.expectedCreds.ca)
			}

			if creds.namespace != tt.expectedCreds.namespace {
				t.Errorf("ReadCredentials() namespace = %v, want %v", creds.namespace, tt.expectedCreds.namespace)
			}
		})
	}
}

func TestExportCredentials(t *testing.T) {
	tests := []struct {
		name        string
		credentials Credentials
		apiServer   string
		wantErr     bool
		validate    func(t *testing.T, outputPath string)
	}{
		{
			name: "valid credentials export",
			credentials: Credentials{
				token:     "test-token",
				ca:        "test-ca-cert",
				namespace: "test-namespace",
			},
			apiServer: "https://test-api-server:6443",
			wantErr:   false,
			validate: func(t *testing.T, outputPath string) {
				t.Helper()
				validateExportedCredentials(t, outputPath)
			},
		},
		{
			name: "invalid output path",
			credentials: Credentials{
				token:     "test-token",
				ca:        "test-ca-cert",
				namespace: "test-namespace",
			},
			apiServer: "https://test-api-server:6443",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var outputPath string
			var err error

			if tt.name == "invalid output path" {
				// Use an invalid path that should cause write errors
				outputPath = "/invalid/nonexistent/path"
			} else {
				// Create temporary directory
				outputPath, err = os.MkdirTemp("", "hostcredentials-test-*")
				if err != nil {
					t.Fatalf("Failed to create temp dir: %v", err)
				}
				defer func() {
					_ = os.RemoveAll(outputPath)
				}()
			}

			err = ExportCredentials(tt.credentials, tt.apiServer, outputPath)

			if tt.wantErr {
				if err == nil {
					t.Errorf("ExportCredentials() error = nil, wantErr %v", tt.wantErr)
				}
				return
			}

			if err != nil {
				t.Errorf("ExportCredentials() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.validate != nil {
				tt.validate(t, outputPath)
			}
		})
	}
}

func validateExportedCredentials(t *testing.T, outputPath string) {
	t.Helper()

	for _, file := range []string{"kubeconfig", "token", "ca.crt", "namespace"} {
		if _, err := os.Stat(filepath.Join(outputPath, file)); os.IsNotExist(err) {
			t.Errorf("Expected file %s was not created", file)
		}
	}

	assertFileContent(t, outputPath, "token", "test-token")
	assertFileContent(t, outputPath, "ca.crt", "test-ca-cert")
	assertFileContent(t, outputPath, "namespace", "test-namespace")

	kubeconfigContent, err := os.ReadFile(filepath.Join(outputPath, "kubeconfig"))
	if err != nil {
		t.Fatalf("Failed to read kubeconfig file: %v", err)
	}
	kubeconfigStr := string(kubeconfigContent)
	if !strings.Contains(kubeconfigStr, "https://test-api-server:6443") {
		t.Errorf("Kubeconfig does not contain expected API server URL")
	}
	if !strings.Contains(kubeconfigStr, "test-namespace") {
		t.Errorf("Kubeconfig does not contain expected namespace")
	}
}

func assertFileContent(t *testing.T, dir, filename, expected string) {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(dir, filename))
	if err != nil {
		t.Fatalf("Failed to read %s file: %v", filename, err)
	}
	if string(content) != expected {
		t.Errorf("%s content = %v, want %v", filename, string(content), expected)
	}
}
