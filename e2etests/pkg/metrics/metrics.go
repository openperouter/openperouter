// SPDX-License-Identifier:Apache-2.0

package metrics

import (
	"fmt"
	"math"
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

// Aggregated contains aggregate statistics for a set of pods.
type Aggregated struct {
	TotalCPU float64
	TotalMem float64
	AvgCPU   float64
	AvgMem   float64
}

// MemoryConvergenceConfig controls the polling behavior of FetchMetricsAggregation.
type MemoryConvergenceConfig struct {
	PollInterval time.Duration
	Timeout      time.Duration
	ToleranceMB  float64
}

func DefaultMemoryConvergenceConfig() MemoryConvergenceConfig {
	return MemoryConvergenceConfig{
		PollInterval: 5 * time.Second,
		Timeout:      90 * time.Second,
		ToleranceMB:  1.0,
	}
}

// CheckAvailability verifies that metrics-server is running by attempting
// to collect pod metrics for the given label selector.
func CheckAvailability(kubectl, namespace, labelSelector string) error {
	_, err := forPod(kubectl, namespace, labelSelector)
	return err
}

// FetchMetricsAggregation polls kubectl top until two consecutive total-memory
// readings are within ToleranceMB of each other, ensuring at least one
// metrics-server refresh has been observed.
func FetchMetricsAggregation(kubectl, namespace, labelSelector string, cfg MemoryConvergenceConfig) (Aggregated, error) {
	fetchAggregation := func() (Aggregated, error) {
		pods, err := forPod(kubectl, namespace, labelSelector)
		if err != nil {
			return Aggregated{}, err
		}
		return summarize(pods), nil
	}

	prev, err := fetchAggregation()
	if err != nil {
		return Aggregated{}, fmt.Errorf("initial metrics poll failed: %w", err)
	}

	ticker := time.NewTicker(cfg.PollInterval)
	defer ticker.Stop()
	deadline := time.After(cfg.Timeout)

	for {
		select {
		case <-deadline:
			return prev, fmt.Errorf("metrics did not stabilize within %s (last two readings: %.2f MB, %.2f MB)",
				cfg.Timeout, prev.TotalMem, prev.TotalMem)
		case <-ticker.C:
			curr, err := fetchAggregation()
			if err != nil {
				return Aggregated{}, fmt.Errorf("metrics poll failed: %w", err)
			}
			if math.Abs(curr.TotalMem-prev.TotalMem) <= cfg.ToleranceMB {
				return curr, nil
			}
			prev = curr
		}
	}
}

// summarize calculates aggregate statistics for a slice of podMetrics.
func summarize(podsMetrics []podMetrics) Aggregated {
	if len(podsMetrics) == 0 {
		return Aggregated{}
	}

	var result Aggregated
	for _, p := range podsMetrics {
		result.TotalCPU += p.cpuMillicores
		result.TotalMem += p.memoryMB
	}

	result.AvgCPU = result.TotalCPU / float64(len(podsMetrics))
	result.AvgMem = result.TotalMem / float64(len(podsMetrics))
	return result
}

// --- private helpers and types ---

type podMetrics struct {
	podName       string
	namespace     string
	cpuMillicores float64
	memoryMB      float64
	timestamp     time.Time
}

func forPod(kubectl, namespace, labelSelector string) ([]podMetrics, error) {
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
func parseKubectlTopOutput(output, namespace string) ([]podMetrics, error) {
	var metrics []podMetrics
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

		metrics = append(metrics, podMetrics{
			podName:       podName,
			namespace:     namespace,
			cpuMillicores: cpu,
			memoryMB:      mem,
			timestamp:     time.Now(),
		})
	}

	return metrics, nil
}

func parseCPU(s string) (float64, error) {
	matches := cpuRegex.FindStringSubmatch(s)
	if len(matches) < 2 {
		return 0, fmt.Errorf("invalid CPU format: %q", s)
	}
	val, err := strconv.ParseFloat(matches[1], 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse CPU value %q: %w", matches[1], err)
	}
	if len(matches) > 2 && matches[2] == "m" {
		return val, nil
	}
	return val * 1000, nil
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
		return val / 1024, nil
	case "Mi":
		return val, nil
	case "Gi":
		return val * 1024, nil
	default:
		return val / (1024 * 1024), nil
	}
}
