// SPDX-License-Identifier:Apache-2.0

package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"text/template"
)

// WorkerNode holds the addressing and naming for a single worker node.
type WorkerNode struct {
	// Name is the kind node name (pe-kind-worker, pe-kind-worker2, …).
	Name string
	// Index is 1-based.
	Index int
	// IPv4/IPv6 addresses on each leaf switch subnet.
	IPv4Sw1, IPv4Sw2 string
	IPv6Sw1, IPv6Sw2 string
	// BridgeSw1/BridgeSw2 are the bridge-side endpoint names on
	// leafkind1-sw / leafkind2-sw.
	BridgeSw1, BridgeSw2 string
	// LeafIface is the interface name on each leafkind toward this worker
	// (same name on both leafkind1 and leafkind2).
	LeafIface string
}

// TopologyData is passed to all templates.
type TopologyData struct {
	Workers []WorkerNode
}

func main() {
	numWorkers := flag.Int("num-workers", 1, "Number of worker nodes in the kind cluster")
	outputDir := flag.String("output-dir", "", "Directory to write generated files (required)")
	flag.Parse()

	if *outputDir == "" {
		log.Fatal("-output-dir is required")
	}

	workers := make([]WorkerNode, *numWorkers)
	for i := range workers {
		idx := i + 1 // 1-based
		workers[i] = WorkerNode{
			Index:     idx,
			Name:      workerName(idx),
			IPv4Sw1:   fmt.Sprintf("192.168.11.%d", 3+idx),
			IPv4Sw2:   fmt.Sprintf("192.168.12.%d", 3+idx),
			IPv6Sw1:   fmt.Sprintf("2001:db8:11::%d", 3+idx),
			IPv6Sw2:   fmt.Sprintf("2001:db8:12::%d", 3+idx),
			BridgeSw1: bridgeEndpoint(idx, 1),
			BridgeSw2: bridgeEndpoint(idx, 2),
			LeafIface: leafIface(idx),
		}
	}

	data := TopologyData{Workers: workers}

	_, thisFile, _, _ := runtime.Caller(0)
	tmplDir := filepath.Join(filepath.Dir(thisFile), "templates")

	for _, t := range []struct {
		tmpl, out string
	}{
		{"kind.clab.yml.tmpl", "kind.clab.yml"},
		{"ip_map.txt.tmpl", "ip_map.txt"},
		{"check-veths.yaml.tmpl", "check-veths.yaml"},
	} {
		if err := renderTemplate(filepath.Join(tmplDir, t.tmpl), filepath.Join(*outputDir, t.out), data); err != nil {
			log.Fatalf("failed to render %s: %v", t.tmpl, err)
		}
		fmt.Printf("Generated %s\n", filepath.Join(*outputDir, t.out))
	}
}

func renderTemplate(tmplPath, outPath string, data any) error {
	content, err := os.ReadFile(tmplPath)
	if err != nil {
		return fmt.Errorf("reading template %s: %w", tmplPath, err)
	}

	tmpl, err := template.New(filepath.Base(tmplPath)).Parse(string(content))
	if err != nil {
		return fmt.Errorf("parsing template %s: %w", tmplPath, err)
	}

	if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}

	f, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("creating output file %s: %w", outPath, err)
	}
	defer func() {
		_ = f.Close()
	}()

	return tmpl.Execute(f, data)
}

// workerName returns the kind node name for worker i (1-based).
// Worker 1 keeps the legacy name "pe-kind-worker"; subsequent workers
// are "pe-kind-worker2", "pe-kind-worker3", etc.
func workerName(i int) string {
	if i == 1 {
		return "pe-kind-worker"
	}
	return fmt.Sprintf("pe-kind-worker%d", i)
}

// bridgeEndpoint returns the bridge-side veth endpoint name for worker i
// on switch sw (1 or 2). Worker 1 keeps the legacy names "kindworker1" /
// "kindworker2"; subsequent workers use "kindwrk{i}sw{sw}" to avoid
// collision with the legacy "kindworker2" (which is worker 1 on switch 2).
func bridgeEndpoint(i, sw int) string {
	if i == 1 {
		return fmt.Sprintf("kindworker%d", sw)
	}
	return fmt.Sprintf("kindwrk%dsw%d", i, sw)
}

// leafIface returns the interface name on each leafkind container toward
// worker i. Worker 1 keeps the legacy "tokindworker"; subsequent workers
// use "tokindworker{i}".
func leafIface(i int) string {
	if i == 1 {
		return "tokindworker"
	}
	return fmt.Sprintf("tokindworker%d", i)
}
