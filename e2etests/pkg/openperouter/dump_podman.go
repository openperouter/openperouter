// SPDX-License-Identifier:Apache-2.0

package openperouter

import (
	"errors"
	"fmt"
	"strings"

	"github.com/openperouter/openperouter/e2etests/pkg/executor"
	corev1 "k8s.io/api/core/v1"
)

// DumpPodmanLogs collects logs from all podman containers running on the specified nodes.
// For each node, it lists all containers in the router pod and dumps their logs.
func DumpPodmanLogs(nodes []corev1.Node) (string, error) {
	allerrs := errors.New("")
	res := ""

	for _, node := range nodes {
		res += fmt.Sprintf("####### Node: %s\n", node.Name)

		exec := executor.ForContainer(node.Name)

		res += fmt.Sprintf("### Podman pods on %s:\n", node.Name)
		podList, err := exec.Exec("podman", "pod", "ps", "--format", "{{.Name}}")
		if err != nil {
			allerrs = errors.Join(allerrs, fmt.Errorf("\nFailed to list pods on node %s: %v", node.Name, err))
			res += fmt.Sprintf("Failed to list pods: %v\n", err)
			continue
		}
		res += podList + "\n"

		res += fmt.Sprintf("### Podman containers on %s:\n", node.Name)
		containerList, err := exec.Exec("podman", "ps", "--format", "{{.Names}}")
		if err != nil {
			allerrs = errors.Join(allerrs, fmt.Errorf("\nFailed to list containers on node %s: %v", node.Name, err))
			res += fmt.Sprintf("Failed to list containers: %v\n", err)
			continue
		}
		res += containerList + "\n"

		containers := strings.Split(strings.TrimSpace(containerList), "\n")
		for _, containerName := range containers {
			containerName = strings.TrimSpace(containerName)
			if containerName == "" {
				continue
			}

			res += fmt.Sprintf("\n### %s container logs on %s:\n", containerName, node.Name)
			logs, err := exec.Exec("podman", "logs", "--since", "10m", containerName)
			if err != nil {
				allerrs = errors.Join(allerrs, fmt.Errorf("\nFailed to get %s logs on node %s: %v", containerName, node.Name, err))
				res += fmt.Sprintf("Failed to get %s logs: %v\n", containerName, err)
			} else {
				res += logs + "\n"
			}
		}

		res += "\n"
	}

	if allerrs.Error() == "" {
		allerrs = nil
	}

	return res, allerrs
}
