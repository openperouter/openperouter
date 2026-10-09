//go:build linux

// SPDX-License-Identifier:Apache-2.0

package routerconfiguration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/openperouter/openperouter/internal/conversion"
	"github.com/openperouter/openperouter/internal/hostnetwork/bridgerefresh"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
)

type benchmarkScenario struct {
	name      string
	directory string
}

var benchmarkScenarios = []benchmarkScenario{
	{name: "vxlan", directory: "testdata/benchmarks/vxlan"},
	{name: "vxlan-srv6", directory: "testdata/benchmarks/vxlan-srv6"},
}

func BenchmarkReconcile(b *testing.B) {
	logger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError})))
	defer slog.SetDefault(logger)
	for _, scenario := range benchmarkScenarios {
		b.Run(scenario.name, func(b *testing.B) {
			b.Run("FirstApplication", func(b *testing.B) { runFirstApplicationBenchmark(b, scenario) })
			b.Run("RepeatedReconcile", func(b *testing.B) { runRepeatedReconcileBenchmark(b, scenario) })
		})
	}
}

func runRepeatedReconcileBenchmark(b *testing.B, scenario benchmarkScenario) {
	b.StopTimer()
	config, err := loadBenchmarkScenario(scenario)
	if err != nil {
		b.Fatal(err)
	}
	path := benchmarkFRRFile(b)
	b.ReportAllocs()
	env, err := newBenchmarkEnvironment(config)
	if err != nil {
		b.Fatalf("%s setup: %v", b.Name(), err)
	}
	defer func() {
		b.StopTimer()
		if err := env.close(); err != nil {
			b.Error(err)
		}
	}()
	if err := reconcileBenchmark(env, config, path); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	b.StartTimer()
	for range b.N {
		if err := reconcileBenchmark(env, config, path); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
}

func runFirstApplicationBenchmark(b *testing.B, scenario benchmarkScenario) {
	b.StopTimer()
	config, err := loadBenchmarkScenario(scenario)
	if err != nil {
		b.Fatal(err)
	}
	path := benchmarkFRRFile(b)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		input, err := cloneBenchmarkConfig(config)
		if err != nil {
			b.Fatal(err)
		}
		env, err := newBenchmarkEnvironment(input)
		if err != nil {
			b.Fatalf("%s setup: %v", b.Name(), err)
		}
		b.StartTimer()
		reconcileErr := reconcileBenchmark(env, input, path)
		b.StopTimer()
		cleanupErr := env.close()
		if err := errors.Join(reconcileErr, cleanupErr); err != nil {
			b.Fatal(err)
		}
	}
}

func loadBenchmarkScenario(scenario benchmarkScenario) (conversion.APIConfigData, error) {
	config, err := readStaticConfigs(scenario.directory, "benchmark-node", "benchmark")
	if err != nil {
		return config, fmt.Errorf("scenario %s fixtures: %w", scenario.name, err)
	}
	return config, nil
}

func reconcileBenchmark(env *benchmarkEnvironment, config conversion.APIConfigData, path string) error {
	return Reconcile(env.ctx, config, 0, "", path, env.path(), noopUpdater,
		&KernelDatapathConfigurator{}, configureFRR)
}

func benchmarkFRRFile(tb testing.TB) string {
	tb.Helper()
	path := filepath.Join(tb.TempDir(), "frr.conf")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		tb.Fatal(err)
	}
	return path
}

func cloneBenchmarkConfig(config conversion.APIConfigData) (conversion.APIConfigData, error) {
	data, err := json.Marshal(config)
	if err != nil {
		return conversion.APIConfigData{}, err
	}
	var copy conversion.APIConfigData
	err = json.Unmarshal(data, &copy)
	return copy, err
}

type benchmarkEnvironment struct {
	original, source, target netns.NsHandle
	ctx                      context.Context
	cancel                   context.CancelFunc
	closed                   bool
}

func newBenchmarkEnvironment(config conversion.APIConfigData) (*benchmarkEnvironment, error) {
	runtime.LockOSThread()
	e := &benchmarkEnvironment{original: netns.None(), source: netns.None(), target: netns.None()}
	e.ctx, e.cancel = context.WithCancel(context.Background())
	if err := e.setup(config); err != nil {
		return nil, errors.Join(err, e.close())
	}
	return e, nil
}

func (e *benchmarkEnvironment) setup(config conversion.APIConfigData) error {
	var err error
	e.original, err = netns.Get()
	if err != nil {
		return fmt.Errorf("save original namespace: %w", err)
	}
	e.source, err = netns.New()
	if err != nil {
		return fmt.Errorf("create source namespace (CAP_SYS_ADMIN): %w", err)
	}
	e.target, err = netns.New()
	if err != nil {
		return fmt.Errorf("create router namespace: %w", err)
	}
	if err := netns.Set(e.source); err != nil {
		return fmt.Errorf("enter source namespace: %w", err)
	}
	lo, err := netlink.LinkByName("lo")
	if err != nil {
		return err
	}
	if err := netlink.LinkSetUp(lo); err != nil {
		return err
	}
	for _, underlay := range config.Underlays {
		for _, iface := range underlay.Spec.Interfaces {
			if iface.NetworkDevice == nil {
				return errors.New("benchmark fixtures require NetworkDevice underlay interfaces")
			}
			link := &netlink.Dummy{LinkAttrs: netlink.LinkAttrs{Name: iface.NetworkDevice.InterfaceName, MTU: 1500}}
			if err := netlink.LinkAdd(link); err != nil {
				return fmt.Errorf("create prerequisite %s (dummy/CAP_NET_ADMIN): %w", link.Name, err)
			}
			if err := netlink.LinkSetUp(link); err != nil {
				return fmt.Errorf("bring prerequisite %s up: %w", link.Name, err)
			}
		}
	}
	return nil
}

func (e *benchmarkEnvironment) path() string {
	return fmt.Sprintf("/proc/self/fd/%d", int(e.target))
}

func (e *benchmarkEnvironment) close() error {
	if e.closed {
		return nil
	}
	e.closed = true
	e.cancel()
	bridgerefresh.StopAll()
	var errs []error
	restoreErr := restoreBenchmarkNamespace(e.original)
	if !e.original.IsOpen() && e.source.IsOpen() {
		restoreErr = errors.New("original namespace handle lost")
	}
	for _, ns := range []*netns.NsHandle{&e.target, &e.source, &e.original} {
		if ns.IsOpen() {
			errs = append(errs, ns.Close())
		}
	}
	if restoreErr != nil {
		// A thread in an uncertain namespace must never return to the Go scheduler.
		panic(fmt.Sprintf("benchmark namespace restoration failed: %v", restoreErr))
	}
	runtime.UnlockOSThread()
	return errors.Join(errs...)
}

func restoreBenchmarkNamespace(original netns.NsHandle) error {
	if !original.IsOpen() {
		return nil
	}
	if err := netns.Set(original); err != nil {
		return fmt.Errorf("restore original namespace: %w", err)
	}
	current, err := netns.Get()
	if err != nil {
		return err
	}
	same := current.Equal(original)
	closeErr := current.Close()
	if !same {
		return errors.Join(errors.New("namespace restoration verification failed"), closeErr)
	}
	return closeErr
}
