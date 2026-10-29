// SPDX-License-Identifier:Apache-2.0

package routerconfiguration

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

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
