// SPDX-License-Identifier:Apache-2.0

package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"slices"
	"strings"

	"golang.org/x/perf/benchfmt"
	"golang.org/x/perf/benchmath"
)

type metric struct{ name, unit string }
type results struct {
	samples map[metric][]float64
	config  map[string]string
}

func main() {
	limit := flag.Float64("max-regression", 10, "maximum significant regression percentage")
	flag.Parse()
	if flag.NArg() != 2 || math.IsNaN(*limit) || math.IsInf(*limit, 0) || *limit < 0 {
		fmt.Fprintln(os.Stderr, "usage: benchmarkcheck [-max-regression percentage] baseline.txt current.txt")
		os.Exit(2)
	}
	if err := run(flag.Arg(0), flag.Arg(1), *limit, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(baselinePath, currentPath string, limit float64, output io.Writer) error {
	baseline, err := readResults(baselinePath)
	if err != nil {
		return err
	}
	current, err := readResults(currentPath)
	if err != nil {
		return err
	}
	return compare(baseline, current, limit, output)
}

func compare(baseline, current results, limit float64, output io.Writer) error {
	for key, value := range baseline.config {
		if current.config[key] != value {
			return fmt.Errorf("environment mismatch for %s", key)
		}
	}
	if len(baseline.samples) != len(current.samples) {
		return errors.New("benchmark metric sets differ")
	}
	keys := make([]metric, 0, len(baseline.samples))
	for key, values := range baseline.samples {
		newer, exists := current.samples[key]
		if !exists {
			return fmt.Errorf("missing current metric: %s %s", key.name, key.unit)
		}
		if len(values) < 10 || len(newer) < 10 {
			return fmt.Errorf("%s %s needs at least 10 samples in each revision", key.name, key.unit)
		}
		keys = append(keys, key)
	}
	slices.SortFunc(keys, func(a, b metric) int {
		if order := strings.Compare(a.name, b.name); order != 0 {
			return order
		}
		return strings.Compare(a.unit, b.unit)
	})
	if _, err := fmt.Fprintf(output, "Fail above %.1f%% regression with p < 0.05 (Mann-Whitney U).\n", limit); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(output, "benchmark metric baseline-median current-median delta p status"); err != nil {
		return err
	}
	failed := false
	for _, key := range keys {
		old := benchmath.NewSample(baseline.samples[key], &benchmath.DefaultThresholds)
		next := benchmath.NewSample(current.samples[key], &benchmath.DefaultThresholds)
		oldMedian := benchmath.AssumeNothing.Summary(old, 0.95).Center
		newMedian := benchmath.AssumeNothing.Summary(next, 0.95).Center
		comparison := benchmath.AssumeNothing.Compare(old, next)
		delta := float64(0)
		if oldMedian != 0 {
			delta = (newMedian/oldMedian - 1) * 100
		}
		if oldMedian == 0 && newMedian > 0 {
			delta = math.Inf(1)
		}
		status := "PASS"
		if newMedian > oldMedian*(1+limit/100) && comparison.P < comparison.Alpha {
			status = "FAIL"
			failed = true
		}
		if _, err := fmt.Fprintf(output, "%s %s %.6g %.6g %+.2f%% %.4g %s\n",
			key.name, key.unit, oldMedian, newMedian, delta, comparison.P, status); err != nil {
			return err
		}
	}
	if failed {
		return errors.New("benchmark regression exceeds threshold")
	}
	return nil
}

func readResults(path string) (results, error) {
	file, err := os.Open(path)
	if err != nil {
		return results{}, fmt.Errorf("open benchmark results: %w", err)
	}
	defer func() { _ = file.Close() }()
	return parseResults(file, path)
}

func parseResults(input io.Reader, name string) (results, error) {
	parsed := results{samples: make(map[metric][]float64), config: make(map[string]string)}
	reader := benchfmt.NewReader(input, name)
	for reader.Scan() {
		switch record := reader.Result().(type) {
		case *benchfmt.SyntaxError:
			return results{}, record
		case *benchfmt.Result:
			if err := addResult(&parsed, record, name); err != nil {
				return results{}, err
			}
		}
	}
	if err := reader.Err(); err != nil {
		return results{}, fmt.Errorf("read benchmark results: %w", err)
	}
	if len(parsed.samples) == 0 {
		return results{}, fmt.Errorf("%s: no benchmarks found", name)
	}
	return parsed, nil
}

func addResult(parsed *results, record *benchfmt.Result, name string) error {
	for _, key := range []string{"goos", "goarch", "cpu", "pkg"} {
		value := record.GetConfig(key)
		if value == "" {
			return fmt.Errorf("%s: missing %s", name, key)
		}
		previous, exists := parsed.config[key]
		if exists && previous != value {
			return fmt.Errorf("%s: inconsistent %s", name, key)
		}
		parsed.config[key] = value
	}
	units := make(map[string]bool)
	for _, value := range record.Values {
		if value.Unit != "sec/op" && value.Unit != "B/op" && value.Unit != "allocs/op" {
			continue
		}
		if units[value.Unit] || math.IsNaN(value.Value) || math.IsInf(value.Value, 0) || value.Value < 0 {
			return fmt.Errorf("%s: invalid metric %s", name, value.Unit)
		}
		units[value.Unit] = true
		key := metric{string(record.Name), value.Unit}
		parsed.samples[key] = append(parsed.samples[key], value.Value)
	}
	if len(units) != 3 {
		return fmt.Errorf("%s: benchmark lacks time or allocation metrics", name)
	}
	return nil
}
