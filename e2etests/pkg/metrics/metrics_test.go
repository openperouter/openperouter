// SPDX-License-Identifier:Apache-2.0

package metrics

import (
	"testing"
)

func TestParseCPU(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    float64
		wantErr bool
	}{
		{name: "millicores", input: "50m", want: 50},
		{name: "millicores zero", input: "0m", want: 0},
		{name: "millicores large", input: "2500m", want: 2500},
		{name: "whole cores", input: "1", want: 1000},
		{name: "whole cores zero", input: "0", want: 0},
		{name: "whole cores multi", input: "4", want: 4000},
		{name: "invalid empty", input: "", wantErr: true},
		{name: "invalid unit", input: "50x", wantErr: true},
		{name: "invalid text", input: "abc", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseCPU(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseCPU(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("parseCPU(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseMemory(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    float64
		wantErr bool
	}{
		{name: "mebibytes", input: "128Mi", want: 128},
		{name: "kibibytes", input: "1024Ki", want: 1},
		{name: "gibibytes", input: "2Gi", want: 2048},
		{name: "bytes", input: "1048576", want: 1},
		{name: "zero Mi", input: "0Mi", want: 0},
		{name: "zero Ki", input: "0Ki", want: 0},
		{name: "invalid empty", input: "", wantErr: true},
		{name: "invalid unit", input: "128Xi", wantErr: true},
		{name: "invalid text", input: "abc", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseMemory(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseMemory(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("parseMemory(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}
