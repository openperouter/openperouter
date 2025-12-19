// SPDX-License-Identifier:Apache-2.0

package metrics

import (
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	cpuRegex = regexp.MustCompile(`^(\d+)(m)?$`)
	memRegex = regexp.MustCompile(`^(\d+)(Ki|Mi|Gi)?$`)
)

// Pod represents CPU and memory metrics for a single pod.
type Pod struct {
	PodName   string    `json:"pod_name"`
	Namespace string    `json:"namespace"`
	CPUMillicores  float64   `json:"cpu_millicores"` // In millicores (e.g., 250 = 250m)
	MemoryMB  float64   `json:"memory_mb"`      // In megabytes
	Timestamp time.Time `json:"timestamp"`
}

// ForPod uses kubectl top to collect metrics for pods matching the label.
func ForPod(kubectl, namespace, labelSelector string) ([]Pod, error) {
	args := []string{"top", "pods", "-n", namespace, "-l", labelSelector, "--no-headers"}
	out, err := exec.Command(kubectl, args...).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("kubectl top pods failed (is metrics-server running?): %s: %w", string(out), err)
	}

	return parseKubectlTopOutput(string(out), namespace)
}

// parseKubectlTopOutput parses "kubectl top pods" output.
// Format: PODNAME    CPU(cores)   MEMORY(bytes)
// Example: router-xyz   50m          128Mi
func parseKubectlTopOutput(output, namespace string) ([]Pod, error) {
	var metrics []Pod
	lines := strings.Split(strings.TrimSpace(output), "\n")

	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}

		podName := fields[0]
		cpuStr := fields[1]
		memStr := fields[2]

		cpu, err := parseCPU(cpuStr)
		if err != nil {
			return nil, fmt.Errorf("failed to parse CPU for pod %s: %w", podName, err)
		}
		mem, err := parseMemory(memStr)
		if err != nil {
			return nil, fmt.Errorf("failed to parse memory for pod %s: %w", podName, err)
		}

		metrics = append(metrics, Pod{
			PodName:   podName,
			Namespace: namespace,
			CPUMillicores:  cpu,
			MemoryMB:  mem,
			Timestamp: time.Now(),
		})
	}

	return metrics, nil
}

func parseCPU(s string) (float64, error) {
	// Handle "50m" (millicores) or "1" (cores)
	matches := cpuRegex.FindStringSubmatch(s)
	if len(matches) < 2 {
		return 0, fmt.Errorf("invalid CPU format: %q", s)
	}
	val, err := strconv.ParseFloat(matches[1], 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse CPU value %q: %w", matches[1], err)
	}
	if len(matches) > 2 && matches[2] == "m" {
		return val, nil // Already in millicores
	}
	return val * 1000, nil // Convert cores to millicores
}

func parseMemory(s string) (float64, error) {
	matches := memRegex.FindStringSubmatch(s)
	if len(matches) < 2 {
		return 0, fmt.Errorf("invalid memory format: %q", s)
	}
	val, err := strconv.ParseFloat(matches[1], 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse memory value %q: %w", matches[1], err)
	}
	unit := ""
	if len(matches) > 2 {
		unit = matches[2]
	}

	switch unit {
	case "Ki":
		return val / 1024, nil // Convert KiB to MiB
	case "Mi":
		return val, nil
	case "Gi":
		return val * 1024, nil
	default:
		return val / (1024 * 1024), nil // Bytes to MiB
	}
}

// MetricsSummaryResult contains aggregate statistics for a slice of Pod.
type MetricsSummaryResult struct {
	TotalCPU float64
	TotalMem float64
	AvgCPU   float64
	AvgMem   float64
}

// SummarizeMetrics calculates aggregate statistics for a slice of Pod.
func SummarizeMetrics(pods []Pod) MetricsSummaryResult {
	if len(pods) == 0 {
		return MetricsSummaryResult{}
	}

	var result MetricsSummaryResult
	for _, p := range pods {
		result.TotalCPU += p.CPUMillicores
		result.TotalMem += p.MemoryMB
	}

	result.AvgCPU = result.TotalCPU / float64(len(pods))
	result.AvgMem = result.TotalMem / float64(len(pods))
	return result
}
