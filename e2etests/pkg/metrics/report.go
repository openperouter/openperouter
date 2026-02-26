// SPDX-License-Identifier:Apache-2.0

package metrics

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// ScaleReport contains all collected metrics from scale tests.
type ScaleReport struct {
	TestRunTime time.Time        `json:"test_run_time"`
	Environment EnvironmentInfo  `json:"environment"`
	DataPoints  []ScaleDataPoint `json:"data_points"`
}

// EnvironmentInfo captures information about the test environment.
type EnvironmentInfo struct {
	KubernetesVersion      string `json:"kubernetes_version,omitempty"`
	NodeCount              int    `json:"node_count"`
	RouterPodCount         int    `json:"router_pod_count"`
	ControllerPodCount     int    `json:"controller_pod_count"`
	MetricsServerAvailable bool   `json:"metrics_server_available"`
}

// ScaleDataPoint represents metrics at a specific scale level.
type ScaleDataPoint struct {
	VNICount int    `json:"vni_count"`
	TestType string `json:"test_type"`
	Duration string `json:"duration"`

	// Baseline (before VNI creation)
	Baseline MetricsSummary `json:"baseline"`

	// After VNI creation
	Scaled MetricsSummary `json:"scaled"`

	// Delta (difference)
	Delta MetricsSummary `json:"delta"`
}

// MetricsSummary aggregates metrics for router and controller pods.
type MetricsSummary struct {
	Router     PodGroupMetrics `json:"router"`
	Controller PodGroupMetrics `json:"controller"`
}

// PodGroupMetrics contains aggregate metrics for a group of pods.
type PodGroupMetrics struct {
	TotalCPUMillicores float64      `json:"total_cpu_millicores"`
	TotalMemoryMB      float64      `json:"total_memory_mb"`
	AvgCPUMillicores   float64      `json:"avg_cpu_millicores"`
	AvgMemoryMB        float64      `json:"avg_memory_mb"`
	PodCount           int          `json:"pod_count"`
	PerPod             []PodMetrics `json:"per_pod,omitempty"`
}

// NewScaleReport creates a new scale report.
func NewScaleReport() *ScaleReport {
	return &ScaleReport{
		TestRunTime: time.Now(),
		DataPoints:  []ScaleDataPoint{},
	}
}

// SetEnvironment sets the environment information.
func (r *ScaleReport) SetEnvironment(env EnvironmentInfo) {
	r.Environment = env
}

// AddDataPoint adds a new data point with baseline and scaled metrics.
func (r *ScaleReport) AddDataPoint(baseline, scaled *ScaleMetrics) {
	baselineSummary := summarizeScaleMetrics(baseline)
	scaledSummary := summarizeScaleMetrics(scaled)

	delta := MetricsSummary{
		Router: PodGroupMetrics{
			TotalCPUMillicores: scaledSummary.Router.TotalCPUMillicores - baselineSummary.Router.TotalCPUMillicores,
			TotalMemoryMB:      scaledSummary.Router.TotalMemoryMB - baselineSummary.Router.TotalMemoryMB,
			AvgCPUMillicores:   scaledSummary.Router.AvgCPUMillicores - baselineSummary.Router.AvgCPUMillicores,
			AvgMemoryMB:        scaledSummary.Router.AvgMemoryMB - baselineSummary.Router.AvgMemoryMB,
			PodCount:           scaledSummary.Router.PodCount,
		},
		Controller: PodGroupMetrics{
			TotalCPUMillicores: scaledSummary.Controller.TotalCPUMillicores - baselineSummary.Controller.TotalCPUMillicores,
			TotalMemoryMB:      scaledSummary.Controller.TotalMemoryMB - baselineSummary.Controller.TotalMemoryMB,
			AvgCPUMillicores:   scaledSummary.Controller.AvgCPUMillicores - baselineSummary.Controller.AvgCPUMillicores,
			AvgMemoryMB:        scaledSummary.Controller.AvgMemoryMB - baselineSummary.Controller.AvgMemoryMB,
			PodCount:           scaledSummary.Controller.PodCount,
		},
	}

	r.DataPoints = append(r.DataPoints, ScaleDataPoint{
		VNICount: scaled.VNICount,
		TestType: scaled.TestType,
		Duration: scaled.Duration.String(),
		Baseline: baselineSummary,
		Scaled:   scaledSummary,
		Delta:    delta,
	})
}

func summarizeScaleMetrics(m *ScaleMetrics) MetricsSummary {
	return MetricsSummary{
		Router:     summarizePodGroup(m.RouterPodMetrics),
		Controller: summarizePodGroup(m.ControllerPodMetrics),
	}
}

func summarizePodGroup(pods []PodMetrics) PodGroupMetrics {
	if len(pods) == 0 {
		return PodGroupMetrics{}
	}

	summary := SummarizeMetrics(pods)

	return PodGroupMetrics{
		TotalCPUMillicores: summary.TotalCPU,
		TotalMemoryMB:      summary.TotalMem,
		AvgCPUMillicores:   summary.AvgCPU,
		AvgMemoryMB:        summary.AvgMem,
		PodCount:           len(pods),
		PerPod:             pods,
	}
}

// WriteJSON writes the report to a JSON file.
func (r *ScaleReport) WriteJSON(path string) error {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal report: %w", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write report to %s: %w", path, err)
	}
	return nil
}

// PrintConsole prints a summary of the report to stdout.
func (r *ScaleReport) PrintConsole() {
	fmt.Println("\n========== VNI SCALE TEST REPORT ==========")
	fmt.Printf("Test Run: %s\n", r.TestRunTime.Format(time.RFC3339))
	fmt.Printf("Environment: %d nodes, %d router pods, %d controller pods\n",
		r.Environment.NodeCount,
		r.Environment.RouterPodCount,
		r.Environment.ControllerPodCount)
	if !r.Environment.MetricsServerAvailable {
		fmt.Println("WARNING: metrics-server not available, CPU/memory values are zero")
		fmt.Println("         Install metrics-server for actual resource measurements")
	}
	fmt.Println()

	for _, dp := range r.DataPoints {
		fmt.Printf("--- %s: %d VNIs ---\n", dp.TestType, dp.VNICount)
		fmt.Printf("Duration: %s\n", dp.Duration)

		fmt.Printf("\nRouter Pods (Delta from baseline):\n")
		fmt.Printf("  CPU:    %+.2f millicores (total), %+.2f millicores (avg/pod)\n",
			dp.Delta.Router.TotalCPUMillicores, dp.Delta.Router.AvgCPUMillicores)
		fmt.Printf("  Memory: %+.2f MB (total), %+.2f MB (avg/pod)\n",
			dp.Delta.Router.TotalMemoryMB, dp.Delta.Router.AvgMemoryMB)

		fmt.Printf("\nController Pods (Delta from baseline):\n")
		fmt.Printf("  CPU:    %+.2f millicores (total), %+.2f millicores (avg/pod)\n",
			dp.Delta.Controller.TotalCPUMillicores, dp.Delta.Controller.AvgCPUMillicores)
		fmt.Printf("  Memory: %+.2f MB (total), %+.2f MB (avg/pod)\n",
			dp.Delta.Controller.TotalMemoryMB, dp.Delta.Controller.AvgMemoryMB)

		fmt.Printf("\nAbsolute Values (Scaled):\n")
		fmt.Printf("  Router:     %.2f millicores CPU, %.2f MB memory\n",
			dp.Scaled.Router.TotalCPUMillicores, dp.Scaled.Router.TotalMemoryMB)
		fmt.Printf("  Controller: %.2f millicores CPU, %.2f MB memory\n",
			dp.Scaled.Controller.TotalCPUMillicores, dp.Scaled.Controller.TotalMemoryMB)
		fmt.Println()
	}
	fmt.Println("============================================")
}
