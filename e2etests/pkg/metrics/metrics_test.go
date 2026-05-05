// SPDX-License-Identifier:Apache-2.0

package metrics

import (
	"math"
	"testing"
)

func TestParseMetricsAPIResponse(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantPods  int
		wantCPU   []float64
		wantMemMB []float64
		wantErr   bool
	}{
		{
			name: "single pod single container",
			input: `{
				"items": [{
					"metadata": {"name": "router-abc", "namespace": "openperouter-system"},
					"containers": [{
						"usage": {"cpu": "50m", "memory": "131072Ki"}
					}]
				}]
			}`,
			wantPods:  1,
			wantCPU:   []float64{50},
			wantMemMB: []float64{128},
		},
		{
			name: "single pod multiple containers",
			input: `{
				"items": [{
					"metadata": {"name": "router-abc", "namespace": "openperouter-system"},
					"containers": [
						{"usage": {"cpu": "30m", "memory": "64Mi"}},
						{"usage": {"cpu": "20m", "memory": "64Mi"}}
					]
				}]
			}`,
			wantPods:  1,
			wantCPU:   []float64{50},
			wantMemMB: []float64{128},
		},
		{
			name: "multiple pods",
			input: `{
				"items": [
					{
						"metadata": {"name": "router-abc", "namespace": "ns"},
						"containers": [{"usage": {"cpu": "100m", "memory": "256Mi"}}]
					},
					{
						"metadata": {"name": "router-def", "namespace": "ns"},
						"containers": [{"usage": {"cpu": "200m", "memory": "512Mi"}}]
					}
				]
			}`,
			wantPods:  2,
			wantCPU:   []float64{100, 200},
			wantMemMB: []float64{256, 512},
		},
		{
			name: "zero usage",
			input: `{
				"items": [{
					"metadata": {"name": "idle-pod", "namespace": "ns"},
					"containers": [{"usage": {"cpu": "0", "memory": "0"}}]
				}]
			}`,
			wantPods:  1,
			wantCPU:   []float64{0},
			wantMemMB: []float64{0},
		},
		{
			name: "empty items",
			input: `{"items": []}`,
			wantPods: 0,
		},
		{
			name:    "invalid json",
			input:   `not json`,
			wantErr: true,
		},
		{
			name: "invalid cpu quantity",
			input: `{
				"items": [{
					"metadata": {"name": "pod", "namespace": "ns"},
					"containers": [{"usage": {"cpu": "bogus-value", "memory": "128Mi"}}]
				}]
			}`,
			wantErr: true,
		},
		{
			name: "nanocores from metrics server",
			input: `{
				"items": [{
					"metadata": {"name": "router-abc", "namespace": "ns"},
					"containers": [{"usage": {"cpu": "3456789n", "memory": "134217728"}}]
				}]
			}`,
			wantPods:  1,
			wantCPU:   []float64{3},
			wantMemMB: []float64{128},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseMetricsAPIResponse([]byte(tt.input))
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseMetricsAPIResponse() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if len(got) != tt.wantPods {
				t.Fatalf("got %d pods, want %d", len(got), tt.wantPods)
			}
			for i, pod := range got {
				if math.Abs(pod.cpuMillicores-tt.wantCPU[i]) > 1 {
					t.Errorf("pod[%d] cpuMillicores = %v, want %v", i, pod.cpuMillicores, tt.wantCPU[i])
				}
				if math.Abs(pod.memoryMB-tt.wantMemMB[i]) > 0.01 {
					t.Errorf("pod[%d] memoryMB = %v, want %v", i, pod.memoryMB, tt.wantMemMB[i])
				}
			}
		})
	}
}
