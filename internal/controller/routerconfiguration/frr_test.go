// SPDX-License-Identifier:Apache-2.0

package routerconfiguration

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/internal/conversion"
	"github.com/stretchr/testify/require"
)

func TestConfigureFRR(t *testing.T) {
	rawConfigA := conversion.APIConfigData{
		RawFRRConfigs: []v1alpha1.RawFRRConfig{
			{Spec: v1alpha1.RawFRRConfigSpec{RawConfig: "! config A"}},
		},
	}
	rawConfigB := conversion.APIConfigData{
		RawFRRConfigs: []v1alpha1.RawFRRConfig{
			{Spec: v1alpha1.RawFRRConfigSpec{RawConfig: "! config B"}},
		},
	}
	apiConfigA := conversion.APIConfigData{
		Underlays: []v1alpha1.Underlay{
			{Spec: v1alpha1.UnderlaySpec{ASN: 65000, RouterIDCIDR: new("10.0.0.0/24")}},
		},
	}
	apiConfigB := conversion.APIConfigData{
		Underlays: []v1alpha1.Underlay{
			{Spec: v1alpha1.UnderlaySpec{ASN: 65001, RouterIDCIDR: new("10.0.0.0/24")}},
		},
	}
	type step struct {
		apiConfig  conversion.APIConfigData
		instanceID string
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
				{apiConfig: rawConfigA, wantCalls: 1},
				{apiConfig: rawConfigA, wantCalls: 1},
			},
		},
		{
			name: "changed configuration reloads including a previous value",
			steps: []step{
				{apiConfig: rawConfigA, wantCalls: 1},
				{apiConfig: rawConfigB, wantCalls: 2},
				{apiConfig: rawConfigA, wantCalls: 3},
			},
		},
		{
			name: "identical API configuration skips reload",
			steps: []step{
				{apiConfig: apiConfigA, wantCalls: 1},
				{apiConfig: apiConfigA, wantCalls: 1},
			},
		},
		{
			name: "reconciliation observes FRR instance changes without a restart event",
			steps: []step{
				{apiConfig: apiConfigA, instanceID: "router-1", wantCalls: 1},
				{apiConfig: apiConfigA, instanceID: "router-1", wantCalls: 1},
				{apiConfig: apiConfigA, instanceID: "router-2", wantCalls: 2},
				{apiConfig: apiConfigA, instanceID: "router-2", wantCalls: 2},
			},
		},
		{
			name: "changed API configuration reloads including a previous value",
			steps: []step{
				{apiConfig: apiConfigA, wantCalls: 1},
				{apiConfig: apiConfigB, wantCalls: 2},
				{apiConfig: apiConfigA, wantCalls: 3},
			},
		},
		{
			name: "log level change reloads",
			steps: []step{
				{apiConfig: rawConfigA, logLevel: "info", wantCalls: 1},
				{apiConfig: rawConfigA, logLevel: "debug", wantCalls: 2},
			},
		},
		{
			name: "failed reload is retried",
			steps: []step{
				{apiConfig: rawConfigA, failReload: true, wantCalls: 1},
				{apiConfig: rawConfigA, wantCalls: 2},
				{apiConfig: rawConfigA, wantCalls: 2},
			},
		},
		{
			name: "failed change invalidates previous configuration",
			steps: []step{
				{apiConfig: rawConfigA, wantCalls: 1},
				{apiConfig: rawConfigB, failReload: true, wantCalls: 2},
				{apiConfig: rawConfigA, wantCalls: 3},
			},
		},
		{
			name: "empty configuration is applied and deduplicated",
			steps: []step{
				{wantCalls: 1},
				{wantCalls: 1},
				{apiConfig: rawConfigA, wantCalls: 2},
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
				t.Run(fmt.Sprintf("step %d", i), func(t *testing.T) {
					if step.instanceID != "" {
						router := &mockRouter{instanceID: func(context.Context) (string, error) {
							return step.instanceID, nil
						}}
						require.NoError(t, invalidateFRRConfigCacheOnRestart(context.Background(), router))
					}
					err := configureFRR(context.Background(), frrConfigData{
						configFile:    configFile,
						APIConfigData: step.apiConfig,
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
					require.Equal(t, step.wantCalls, calls, "step %d", i)
					if step.failReload {
						require.EqualError(t, err, "failed to update the frr configuration: reload failed", "step %d", i)
						require.Nil(t, lastAppliedFRRConfig, "step %d", i)
						return
					}
					require.NoError(t, err, "step %d", i)
					require.NotNil(t, lastAppliedFRRConfig, "step %d", i)
				})
			}
		})
	}
}

func TestFRRRestartWatcherReappliesConfiguration(t *testing.T) {
	resetFRRConfigCache(t)
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "router.pid")
	require.NoError(t, os.WriteFile(pidFile, []byte("100"), 0600))
	provider := &RouterHostProvider{RouterPidFilePath: pidFile}
	router := &RouterHostContainer{manager: provider}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	restarted := make(chan struct{}, 1)
	require.NoError(t, provider.StartFRRRestartWatcher(ctx, func() {
		select {
		case restarted <- struct{}{}:
		default:
		}
	}))
	calls := 0
	data := frrConfigData{
		configFile: filepath.Join(dir, "frr.conf"),
		updater: func(_ context.Context, config string) error {
			calls++
			return os.WriteFile(filepath.Join(dir, "frr.conf"), []byte(config), 0600)
		},
	}
	require.NoError(t, invalidateFRRConfigCacheOnRestart(ctx, router))
	require.NoError(t, configureFRR(ctx, data))
	require.NoError(t, invalidateFRRConfigCacheOnRestart(ctx, router))
	require.NoError(t, configureFRR(ctx, data))
	require.Equal(t, 1, calls)

	require.NoError(t, os.WriteFile(pidFile, []byte("200"), 0600))
	select {
	case <-restarted:
	case <-time.After(5 * time.Second):
		t.Fatal("router restart did not trigger reconciliation")
	}
	require.NotNil(t, lastAppliedFRRConfig, "watcher must only enqueue reconciliation")
	require.NoError(t, invalidateFRRConfigCacheOnRestart(ctx, router))
	require.NoError(t, configureFRR(ctx, data))
	require.Equal(t, 2, calls, "identical configuration must be applied to the restarted FRR instance")
}

func resetFRRConfigCache(t testing.TB) {
	t.Helper()
	frrConfigMutex.Lock()
	lastAppliedFRRConfig = nil
	lastFRRInstanceID = ""
	frrConfigMutex.Unlock()
	t.Cleanup(func() {
		frrConfigMutex.Lock()
		lastAppliedFRRConfig = nil
		lastFRRInstanceID = ""
		frrConfigMutex.Unlock()
	})
}
