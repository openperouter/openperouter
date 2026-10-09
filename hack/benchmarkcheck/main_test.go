// SPDX-License-Identifier:Apache-2.0

package main

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"golang.org/x/perf/benchfmt"
)

func TestRegressionGate(t *testing.T) {
	tests := []struct {
		name      string
		old, next []float64
		wantErr   error
	}{
		{name: "identical", old: repeat(100), next: repeat(100)},
		{name: "improvement", old: repeat(100), next: repeat(80)},
		{name: "regression", old: repeat(100), next: repeat(120), wantErr: errRegression},
		{name: "below threshold", old: repeat(100), next: repeat(105)},
		{name: "at threshold", old: repeat(100), next: repeat(110)},
		{name: "zero baseline", old: repeat(0), next: repeat(1), wantErr: errRegression},
		// The median rises by 20%, but the overlapping samples are not statistically significant.
		// Exceeding the regression threshold alone must not fail the gate.
		{
			name: "non-significant regression",
			old:  []float64{1, 2, 3, 4, 100, 100, 200, 300, 400, 500},
			next: []float64{1, 2, 3, 4, 120, 120, 200, 300, 400, 500},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			key := metric{"BenchmarkReconcile/test-2", "sec/op"}
			old := results{samples: map[metric][]float64{key: test.old}}
			next := results{samples: map[metric][]float64{key: test.next}}
			err := compare(old, next, 10, io.Discard)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("compare returned %v; want %v", err, test.wantErr)
			}
		})
	}
}

func TestResultValidation(t *testing.T) {
	valid := "goos: linux\ngoarch: amd64\npkg: example\ncpu: example CPU\n" +
		"BenchmarkReconcile/test-2 20 100 ns/op 200 B/op 3 allocs/op\n"
	tests := []struct {
		name, input   string
		wantErr       string
		wantSyntaxErr bool
	}{
		{name: "valid", input: valid},
		{name: "empty", wantErr: "test: no benchmarks found"},
		{
			name: "missing allocations", input: strings.ReplaceAll(valid, " 3 allocs/op", ""),
			wantErr: "test: benchmark lacks time or allocation metrics",
		},
		{
			name: "missing environment", input: strings.ReplaceAll(valid, "cpu: example CPU\n", ""),
			wantErr: "test: missing cpu",
		},
		{
			name: "malformed", input: strings.ReplaceAll(valid, "20 100", "invalid 100"),
			wantSyntaxErr: true,
		},
		{
			name: "nonfinite", input: strings.ReplaceAll(valid, "100 ns/op", "NaN ns/op"),
			wantErr: "test: invalid metric sec/op",
		},
		{
			name: "negative", input: strings.ReplaceAll(valid, "200 B/op", "-200 B/op"),
			wantErr: "test: invalid metric B/op",
		},
		{
			name: "duplicate", input: strings.ReplaceAll(valid, "3 allocs/op", "3 allocs/op 4 allocs/op"),
			wantErr: "test: invalid metric allocs/op",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			parsed, err := parseResults(strings.NewReader(test.input), "test")
			if test.wantSyntaxErr {
				var syntaxErr *benchfmt.SyntaxError
				if !errors.As(err, &syntaxErr) {
					t.Fatalf("parse returned %v; want a syntax error", err)
				}
				return
			}
			if test.wantErr != "" {
				if err == nil || err.Error() != test.wantErr {
					t.Fatalf("parse returned %v; want %s", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(parsed.samples) != 3 {
				t.Fatalf("got %d metrics", len(parsed.samples))
			}
		})
	}
}

func TestComparisonRejectsIncompleteData(t *testing.T) {
	key := metric{"BenchmarkReconcile/test-2", "sec/op"}
	baseline := results{samples: map[metric][]float64{key: repeat(100)}, config: map[string]string{"cpu": "same"}}
	tests := []struct {
		name    string
		current results
		wantErr string
	}{
		{
			name:    "missing metrics",
			current: results{samples: map[metric][]float64{}, config: baseline.config},
			wantErr: "benchmark metric sets differ",
		},
		{
			name: "different metric",
			current: results{
				samples: map[metric][]float64{{"other", "sec/op"}: repeat(100)}, config: baseline.config,
			},
			wantErr: "missing current metric: BenchmarkReconcile/test-2 sec/op",
		},
		{
			name:    "insufficient samples",
			current: results{samples: map[metric][]float64{key: {100}}, config: baseline.config},
			wantErr: fmt.Sprintf("BenchmarkReconcile/test-2 sec/op needs at least %d samples in each revision", minSamples),
		},
		{
			name:    "different environment",
			current: results{samples: baseline.samples, config: map[string]string{"cpu": "different"}},
			wantErr: "environment mismatch for cpu",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := compare(baseline, test.current, 10, io.Discard)
			if err == nil || err.Error() != test.wantErr {
				t.Fatalf("compare returned %v; want %s", err, test.wantErr)
			}
		})
	}
}

func TestAllocationRegression(t *testing.T) {
	var data strings.Builder
	fmt.Fprintln(&data, "goos: linux\ngoarch: amd64\npkg: example\ncpu: example CPU")
	for range minSamples {
		fmt.Fprintln(&data, "BenchmarkReconcile/test-2 20 100 ns/op 200 B/op 3 allocs/op")
	}
	baseline, err := parseResults(strings.NewReader(data.String()), "baseline")
	if err != nil {
		t.Fatal(err)
	}
	for _, unit := range []string{"B/op", "allocs/op"} {
		t.Run(unit, func(t *testing.T) {
			current, err := parseResults(strings.NewReader(data.String()), "current")
			if err != nil {
				t.Fatal(err)
			}
			current.samples[metric{"Reconcile/test-2", unit}] = repeat(300)
			err = compare(baseline, current, 10, io.Discard)
			if !errors.Is(err, errRegression) {
				t.Fatalf("expected allocation regression, got %v", err)
			}
		})
	}
}

func repeat(value float64) []float64 {
	values := make([]float64, minSamples)
	for i := range values {
		values[i] = value
	}
	return values
}
