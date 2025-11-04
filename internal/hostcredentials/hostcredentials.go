// SPDX-License-Identifier:Apache-2.0

package hostcredentials

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
)

const (
	ServiceAccountDir = "/var/run/secrets/kubernetes.io/serviceaccount"
	tokenFile         = "token"
	caFile            = "ca.crt"
	namespaceFile     = "namespace"

	kubeConfigFile = "kubeconfig"
)

func ApiServerAddress(port int) (string, error) {
	clusterIP, err := resolveKubernetesServiceIP()
	if err != nil {
		return "", fmt.Errorf("failed to get kubernetes service IP: %w", err)
	}
	slog.Info("Resolved kubernetes.default.svc to cluster IP", "ip", clusterIP)
	return fmt.Sprintf("https://%s:%d", clusterIP, port), nil
}

type Credentials struct {
	token     string
	ca        string
	namespace string
}

func ReadCredentials(path string) (Credentials, error) {
	token, err := read(path, tokenFile)
	if err != nil {
		return Credentials{}, fmt.Errorf("failed to read token: %w", err)
	}

	ca, err := read(path, caFile)
	if err != nil {
		return Credentials{}, fmt.Errorf("failed to read CA certificate: %w", err)
	}

	namespace, err := read(path, namespaceFile)
	if err != nil {
		return Credentials{}, fmt.Errorf("failed to read namespace: %w", err)
	}

	return Credentials{
		token:     token,
		ca:        ca,
		namespace: namespace,
	}, nil
}

func ExportCredentials(credentials Credentials, apiServer, outputPath string) error {
	kubeconfigContent := fmt.Sprintf(`apiVersion: v1
kind: Config
clusters:
- cluster:
    certificate-authority: %s
    server: %s
  name: default
contexts:
- context:
    cluster: default
    user: default
    namespace: %s
  name: default
current-context: default
users:
- name: default
  user:
    tokenFile: %s
`,
		filepath.Join(outputPath, caFile),
		apiServer,
		credentials.namespace,
		filepath.Join(outputPath, tokenFile),
	)

	if err := write(outputPath, kubeConfigFile, []byte(kubeconfigContent)); err != nil {
		return fmt.Errorf("failed to write kubeconfig: %w", err)
	}

	if err := write(outputPath, tokenFile, []byte(credentials.token)); err != nil {
		return fmt.Errorf("failed to write token: %w", err)
	}

	if err := write(outputPath, caFile, []byte(credentials.ca)); err != nil {
		return fmt.Errorf("failed to write CA: %w", err)
	}

	if err := write(outputPath, namespaceFile, []byte(credentials.namespace)); err != nil {
		return fmt.Errorf("failed to write namespace: %w", err)
	}

	return nil
}

func read(path, filename string) (string, error) {
	fullPath := filepath.Join(path, filename)
	res, err := os.ReadFile(fullPath)
	if err != nil {
		return "", fmt.Errorf("failed to read %s: %w", path, err)
	}
	return string(res), nil
}

func write(path, filename string, content []byte) error {
	fullpath := filepath.Join(path, filename)
	return os.WriteFile(fullpath, content, 0644)
}
