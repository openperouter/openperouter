// SPDX-License-Identifier:Apache-2.0

package routerconfiguration

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/internal/conversion"
	"github.com/stretchr/testify/require"
	ctrl "sigs.k8s.io/controller-runtime"
)

func TestReconcileSkipsIdenticalFRRConfig(t *testing.T) {
	type reconciler interface {
		Reconcile(context.Context, ctrl.Request) (ctrl.Result, error)
	}
	tests := []struct {
		name          string
		newReconciler func(t *testing.T, socketPath, configFile string) reconciler
	}{
		{
			name: "Kubernetes configuration",
			newReconciler: func(t *testing.T, socketPath, configFile string) reconciler {
				r := newTestPERouterReconciler(t, &noopDatapathConfigurator{}, testNode())
				r.FRRReloadSocket = socketPath
				r.FRRConfigPath = configFile
				return r
			},
		},
		{
			name: "static configuration",
			newReconciler: func(t *testing.T, socketPath, configFile string) reconciler {
				configDir := t.TempDir()
				writeYAMLFile(t, configDir, "openpe_router.yaml", "{}")
				return &StaticConfigReconciler{
					Logger:               testLogger(),
					RouterProvider:       &mockRouterProvider{},
					DatapathConfigurator: &noopDatapathConfigurator{},
					ConfigDir:            configDir,
					FRRReloadSocket:      socketPath,
					FRRConfigPath:        configFile,
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetFRRConfigCache(t)
			dir := t.TempDir()
			socketPath := filepath.Join(dir, "reload.sock")
			listener, err := net.Listen("unix", socketPath)
			require.NoError(t, err)
			var calls atomic.Int32
			server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				w.WriteHeader(http.StatusOK)
			})}
			go func() { _ = server.Serve(listener) }()
			t.Cleanup(func() { require.NoError(t, server.Close()) })

			r := tt.newReconciler(t, socketPath, filepath.Join(dir, "frr.conf"))
			for range 2 {
				_, err := r.Reconcile(context.Background(), ctrl.Request{})
				require.NoError(t, err)
				require.Equal(t, int32(1), calls.Load())
			}
		})
	}
}

func TestConfigureFRR(t *testing.T) {
	type step struct {
		rawConfig  string
		logLevel   string
		failReload bool
		wantCalls  int
	}
	tests := []struct {
		name  string
		steps []step
	}{
		{
			name: "identical configuration skips reload",
			steps: []step{
				{rawConfig: "! config A", wantCalls: 1},
				{rawConfig: "! config A", wantCalls: 1},
			},
		},
		{
			name: "changed configuration reloads including a previous value",
			steps: []step{
				{rawConfig: "! config A", wantCalls: 1},
				{rawConfig: "! config B", wantCalls: 2},
				{rawConfig: "! config A", wantCalls: 3},
			},
		},
		{
			name: "log level change reloads",
			steps: []step{
				{rawConfig: "! config A", logLevel: "info", wantCalls: 1},
				{rawConfig: "! config A", logLevel: "debug", wantCalls: 2},
			},
		},
		{
			name: "failed reload is retried",
			steps: []step{
				{rawConfig: "! config A", failReload: true, wantCalls: 1},
				{rawConfig: "! config A", wantCalls: 2},
				{rawConfig: "! config A", wantCalls: 2},
			},
		},
		{
			name: "failed change invalidates previous configuration",
			steps: []step{
				{rawConfig: "! config A", wantCalls: 1},
				{rawConfig: "! config B", failReload: true, wantCalls: 2},
				{rawConfig: "! config A", wantCalls: 3},
			},
		},
		{
			name: "empty configuration is applied and deduplicated",
			steps: []step{
				{wantCalls: 1},
				{wantCalls: 1},
				{rawConfig: "! config A", wantCalls: 2},
				{wantCalls: 3},
				{wantCalls: 3},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetFRRConfigCache(t)
			configFile := filepath.Join(t.TempDir(), "frr.conf")
			calls := 0
			for i, step := range tt.steps {
				apiConfig := conversion.APIConfigData{}
				if step.rawConfig != "" {
					apiConfig.RawFRRConfigs = []v1alpha1.RawFRRConfig{
						{Spec: v1alpha1.RawFRRConfigSpec{RawConfig: step.rawConfig}},
					}
				}
				err := configureFRR(context.Background(), frrConfigData{
					configFile:    configFile,
					APIConfigData: apiConfig,
					logLevel:      step.logLevel,
					updater: func(_ context.Context, config string) error {
						calls++
						if err := os.WriteFile(configFile, []byte(config), 0600); err != nil {
							return err
						}
						if step.failReload {
							return errors.New("reload failed")
						}
						return nil
					},
				})
				if step.failReload {
					require.EqualError(t, err, "failed to update the frr configuration: reload failed", "step %d", i)
					require.Nil(t, lastAppliedFRRConfig, "step %d", i)
				} else {
					require.NoError(t, err, "step %d", i)
					require.NotNil(t, lastAppliedFRRConfig, "step %d", i)
				}
				require.Equal(t, step.wantCalls, calls, "step %d", i)
			}
		})
	}
}

func resetFRRConfigCache(t testing.TB) {
	t.Helper()
	frrConfigMutex.Lock()
	lastAppliedFRRConfig = nil
	frrConfigMutex.Unlock()
	t.Cleanup(func() {
		frrConfigMutex.Lock()
		lastAppliedFRRConfig = nil
		frrConfigMutex.Unlock()
	})
}
