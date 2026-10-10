// SPDX-License-Identifier:Apache-2.0

package main

import (
	"fmt"
	"io"
	"strings"
	"testing"
)

func TestRegressionGate(t *testing.T) {
	tests := []struct {
		name      string
		old, next []float64
		fails     bool
	}{
		{"identical", repeat(100), repeat(100), false},
		{"improvement", repeat(100), repeat(80), false},
		{"regression", repeat(100), repeat(120), true},
		{"below threshold", repeat(100), repeat(105), false},
		{"at threshold", repeat(100), repeat(110), false},
		{"zero baseline", repeat(0), repeat(1), true},
		{"noise", []float64{1, 2, 3, 4, 100, 100, 200, 300, 400, 500},
			[]float64{1, 2, 3, 4, 120, 120, 200, 300, 400, 500}, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			key := metric{"BenchmarkReconcile/test-2", "sec/op"}
			old := results{samples: map[metric][]float64{key: test.old}}
			next := results{samples: map[metric][]float64{key: test.next}}
			err := compare(old, next, 10, io.Discard)
			if (err != nil) != test.fails {
				t.Fatalf("compare returned %v; fails=%v", err, test.fails)
			}
		})
	}
}

func TestResultValidation(t *testing.T) {
	valid := "goos: linux\ngoarch: amd64\npkg: example\ncpu: example CPU\n" +
		"BenchmarkReconcile/test-2 20 100 ns/op 200 B/op 3 allocs/op\n"
	tests := []struct {
		name, input string
		fails       bool
	}{
		{"valid", valid, false},
		{"empty", "", true},
		{"missing allocations", strings.ReplaceAll(valid, " 3 allocs/op", ""), true},
		{"missing environment", strings.ReplaceAll(valid, "cpu: example CPU\n", ""), true},
		{"malformed", strings.ReplaceAll(valid, "20 100", "invalid 100"), true},
		{"nonfinite", strings.ReplaceAll(valid, "100 ns/op", "NaN ns/op"), true},
		{"negative", strings.ReplaceAll(valid, "200 B/op", "-200 B/op"), true},
		{"duplicate", strings.ReplaceAll(valid, "3 allocs/op", "3 allocs/op 4 allocs/op"), true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			parsed, err := parseResults(strings.NewReader(test.input), "test")
			if (err != nil) != test.fails {
				t.Fatalf("parse returned %v; fails=%v", err, test.fails)
			}
			if !test.fails && len(parsed.samples) != 3 {
				t.Fatalf("got %d metrics", len(parsed.samples))
			}
		})
	}
}

func TestComparisonRejectsIncompleteData(t *testing.T) {
	key := metric{"BenchmarkReconcile/test-2", "sec/op"}
	baseline := results{samples: map[metric][]float64{key: repeat(100)}, config: map[string]string{"cpu": "same"}}
	for _, current := range []results{
		{samples: map[metric][]float64{}},
		{samples: map[metric][]float64{{"other", "sec/op"}: repeat(100)}, config: baseline.config},
		{samples: map[metric][]float64{key: {100}}, config: baseline.config},
		{samples: baseline.samples, config: map[string]string{"cpu": "different"}},
	} {
		if err := compare(baseline, current, 10, io.Discard); err == nil {
			t.Fatal("accepted incomplete data")
		}
	}
}

func TestAllocationRegression(t *testing.T) {
	var data strings.Builder
	fmt.Fprintln(&data, "goos: linux\ngoarch: amd64\npkg: example\ncpu: example CPU")
	for range 10 {
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
			if err == nil || err.Error() != "benchmark regression exceeds threshold" {
				t.Fatalf("expected allocation regression, got %v", err)
			}
		})
	}
}

func repeat(value float64) []float64 {
	values := make([]float64, 10)
	for i := range values {
		values[i] = value
	}
	return values
}
