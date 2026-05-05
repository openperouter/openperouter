// SPDX-License-Identifier:Apache-2.0

package metrics

import (
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"os/exec"
	"time"

	"k8s.io/apimachinery/pkg/api/resource"
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
func FetchMetricsAggregation(
	kubectl, namespace, labelSelector string,
	cfg MemoryConvergenceConfig,
) (Aggregated, error) {
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

type podMetrics struct {
	podName       string
	namespace     string
	cpuMillicores float64
	memoryMB      float64
	timestamp     time.Time
}

type podMetricsList struct {
	Items []podMetricsItem `json:"items"`
}

type podMetricsItem struct {
	Metadata   podMetricsMetadata   `json:"metadata"`
	Containers []containerMetrics   `json:"containers"`
}

type podMetricsMetadata struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}

type containerMetrics struct {
	Usage map[string]string `json:"usage"`
}

func forPod(kubectl, namespace, labelSelector string) ([]podMetrics, error) {
	path := fmt.Sprintf("/apis/metrics.k8s.io/v1beta1/namespaces/%s/pods?labelSelector=%s",
		namespace, url.QueryEscape(labelSelector))
	out, err := exec.Command(kubectl, "get", "--raw", path).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("metrics API request failed (is metrics-server running?): %s: %w", string(out), err)
	}

	return parseMetricsAPIResponse(out)
}

func parseMetricsAPIResponse(data []byte) ([]podMetrics, error) {
	var list podMetricsList
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, fmt.Errorf("failed to parse metrics API response: %w", err)
	}

	result := make([]podMetrics, 0, len(list.Items))
	for _, item := range list.Items {
		var totalCPUMilli int64
		var totalMemBytes int64

		for _, c := range item.Containers {
			if cpuStr, ok := c.Usage["cpu"]; ok {
				q, err := resource.ParseQuantity(cpuStr)
				if err != nil {
					return nil, fmt.Errorf("failed to parse CPU quantity %q for pod %s: %w", cpuStr, item.Metadata.Name, err)
				}
				totalCPUMilli += q.MilliValue()
			}
			if memStr, ok := c.Usage["memory"]; ok {
				q, err := resource.ParseQuantity(memStr)
				if err != nil {
					return nil, fmt.Errorf("failed to parse memory quantity %q for pod %s: %w", memStr, item.Metadata.Name, err)
				}
				totalMemBytes += q.Value()
			}
		}

		result = append(result, podMetrics{
			podName:       item.Metadata.Name,
			namespace:     item.Metadata.Namespace,
			cpuMillicores: float64(totalCPUMilli),
			memoryMB:      float64(totalMemBytes) / (1024 * 1024),
			timestamp:     time.Now(),
		})
	}

	return result, nil
}
