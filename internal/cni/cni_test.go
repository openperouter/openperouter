// SPDX-License-Identifier:Apache-2.0

package cni

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const fakeConfList = `{
  "cniVersion": "1.0.0",
  "name": "underlay-test",
  "plugins": [
    {
      "type": "fake",
      "capabilities": {"mac": true}
    }
  ]
}`

const fakeConf = `{
  "cniVersion": "1.0.0",
  "name": "underlay-test",
  "type": "fake"
}`

func TestAddInvokesPluginOnceAndCaches(t *testing.T) {
	env := newFakePluginEnv(t)

	params := env.params("net1", nil)
	err := env.invoker().Add(context.Background(), params)
	if err != nil {
		t.Fatalf("first Add failed: %v", err)
	}

	err = env.invoker().Add(context.Background(), params)
	if err != nil {
		t.Fatalf("second Add failed: %v", err)
	}

	if commands := env.loggedCommands(t); len(commands) != 1 || commands[0] != "ADD net1" {
		t.Fatalf("expected a single 'ADD net1' plugin invocation, got %v", commands)
	}
}

func TestAddRejectsChangedConfig(t *testing.T) {
	env := newFakePluginEnv(t)

	if err := env.invoker().Add(context.Background(), env.params("net1", nil)); err != nil {
		t.Fatalf("first Add failed: %v", err)
	}

	changed := env.params("net1", nil)
	changed.Config = []byte(`{
  "cniVersion": "1.0.0",
  "name": "underlay-test",
  "plugins": [
    {
      "type": "fake",
      "capabilities": {"mac": true},
      "mtu": 1400
    }
  ]
}`)
	err := env.invoker().Add(context.Background(), changed)
	var mismatchErr ConfigMismatchError
	if !errors.As(err, &mismatchErr) {
		t.Fatalf("expected ConfigMismatchError for a changed config, got %v", err)
	}
	if mismatchErr.IfName != "net1" {
		t.Errorf("ConfigMismatchError.IfName = %q, want %q", mismatchErr.IfName, "net1")
	}
	if commands := env.loggedCommands(t); len(commands) != 1 {
		t.Fatalf("the changed config should not be applied, got %v", commands)
	}
}

func TestAddRejectsChangedCapabilityArgs(t *testing.T) {
	env := newFakePluginEnv(t)

	if err := env.invoker().Add(context.Background(),
		env.params("net1", map[string]any{"mac": "02:42:c0:a8:01:0a"})); err != nil {
		t.Fatalf("first Add failed: %v", err)
	}

	err := env.invoker().Add(context.Background(),
		env.params("net1", map[string]any{"mac": "02:42:c0:a8:01:0b"}))
	var mismatchErr ConfigMismatchError
	if !errors.As(err, &mismatchErr) {
		t.Fatalf("expected ConfigMismatchError for changed capability args, got %v", err)
	}
	if commands := env.loggedCommands(t); len(commands) != 1 {
		t.Fatalf("the changed capability args should not be applied, got %v", commands)
	}
}

func TestAddIgnoresConfigFormattingChanges(t *testing.T) {
	env := newFakePluginEnv(t)

	if err := env.invoker().Add(context.Background(), env.params("net1", nil)); err != nil {
		t.Fatalf("first Add failed: %v", err)
	}

	reformatted := env.params("net1", nil)
	reformatted.Config = []byte(`{"plugins":[{"capabilities":{"mac":true},"type":"fake"}],"name":"underlay-test","cniVersion":"1.0.0"}`)
	if err := env.invoker().Add(context.Background(), reformatted); err != nil {
		t.Fatalf("formatting-only config change should be idempotent, got %v", err)
	}
	if commands := env.loggedCommands(t); len(commands) != 1 {
		t.Fatalf("formatting-only config change should not invoke the plugin, got %v", commands)
	}
}

func TestAddWithSinglePluginConf(t *testing.T) {
	env := newFakePluginEnv(t)

	params := env.params("net1", nil)
	params.Config = []byte(fakeConf)
	err := env.invoker().Add(context.Background(), params)
	if err != nil {
		t.Fatalf("Add failed: %v", err)
	}
}

func TestAddWithInvalidConfig(t *testing.T) {
	env := newFakePluginEnv(t)

	params := env.params("net1", nil)
	params.Config = []byte(`{"cniVersion": `)
	if err := env.invoker().Add(context.Background(), params); err == nil {
		t.Fatal("expected error for malformed CNI config")
	}
}

func TestAddForwardsDeclaredCapabilitiesOnly(t *testing.T) {
	env := newFakePluginEnv(t)

	params := env.params("net1", map[string]any{
		"mac":        "02:42:c0:a8:01:0a",
		"undeclared": "stripped",
	})
	if err := env.invoker().Add(context.Background(), params); err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	stdin := env.pluginStdin(t, "ADD", "net1")
	runtimeConfig, hasRuntimeConfig := stdin["runtimeConfig"].(map[string]any)
	if !hasRuntimeConfig {
		t.Fatalf("plugin stdin has no runtimeConfig: %v", stdin)
	}
	if runtimeConfig["mac"] != "02:42:c0:a8:01:0a" {
		t.Errorf("declared capability 'mac' not forwarded, runtimeConfig: %v", runtimeConfig)
	}
	if _, hasUndeclared := runtimeConfig["undeclared"]; hasUndeclared {
		t.Errorf("undeclared capability forwarded to the plugin, runtimeConfig: %v", runtimeConfig)
	}
}

func TestDelTearsDownAndClearsCache(t *testing.T) {
	env := newFakePluginEnv(t)

	if err := env.invoker().Add(context.Background(), env.params("net1", nil)); err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	if err := env.invoker().Del(context.Background(), "net1"); err != nil {
		t.Fatalf("Del failed: %v", err)
	}
	commands := env.loggedCommands(t)
	if len(commands) != 2 || commands[1] != "DEL net1" {
		t.Fatalf("expected 'DEL net1' after the ADD, got %v", commands)
	}

	// The cache entry is gone: deleting again invokes nothing.
	if err := env.invoker().Del(context.Background(), "net1"); err != nil {
		t.Fatalf("second Del failed: %v", err)
	}
	if commands := env.loggedCommands(t); len(commands) != 2 {
		t.Fatalf("second Del should not invoke the plugin, got %v", commands)
	}
}

func TestDelLeavesOtherInterfacesUntouched(t *testing.T) {
	env := newFakePluginEnv(t)

	for _, ifName := range []string{"net1", "net2"} {
		if err := env.invoker().Add(context.Background(), env.params(ifName, nil)); err != nil {
			t.Fatalf("Add %s failed: %v", ifName, err)
		}
	}

	if err := env.invoker().Del(context.Background(), "net2"); err != nil {
		t.Fatalf("Del failed: %v", err)
	}

	commands := env.loggedCommands(t)
	if len(commands) != 3 || commands[2] != "DEL net2" {
		t.Fatalf("expected only 'DEL net2' after the ADDs, got %v", commands)
	}

	// net1 is still cached, so a new Add is a no-op.
	if err := env.invoker().Add(context.Background(), env.params("net1", nil)); err != nil {
		t.Fatalf("Add after Del failed: %v", err)
	}
}

func TestDelWithEmptyCache(t *testing.T) {
	env := newFakePluginEnv(t)

	if err := env.invoker().Del(context.Background(), "net1"); err != nil {
		t.Fatalf("Del on empty cache failed: %v", err)
	}
	if commands := env.loggedCommands(t); len(commands) != 0 {
		t.Fatalf("Del on empty cache should not invoke the plugin, got %v", commands)
	}
}

func TestValidateConfig(t *testing.T) {
	tests := []struct {
		name    string
		config  string
		wantErr string
	}{
		{name: "valid conflist", config: fakeConfList},
		{name: "valid single conf", config: fakeConf},
		{
			name:    "malformed json",
			config:  `{"cniVersion": `,
			wantErr: "error parsing configuration list: unexpected end of JSON input",
		},
		{
			name:    "conflist with broken plugins",
			config:  `{"cniVersion": "1.0.0", "name": "u", "plugins": "notalist"}`,
			wantErr: "error parsing configuration list: invalid 'plugins' type string"},
		{
			name:    "unsupported cni version",
			config:  `{"cniVersion": "0.4.0", "name": "u", "plugins": [{"type": "fake"}]}`,
			wantErr: "cniVersion \"0.4.0\" is not supported, only CNI >= 1.0.0 is",
		},
		{
			name:    "missing cni version",
			config:  `{"name": "u", "plugins": [{"type": "fake"}]}`,
			wantErr: "cniVersion \"\" is not supported, only CNI >= 1.0.0 is",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateConfig([]byte(tc.config))
			if err != nil && tc.wantErr != err.Error() {
				t.Fatalf("expected error %q, got %q", tc.wantErr, err.Error())
			}
			if err == nil && tc.wantErr != "" {
				t.Fatalf("expected error %q, got nil", tc.wantErr)
			}
		})
	}
}

const (
	netNS = "/var/run/netns/perouter-test"
)

// fakePluginEnv provides a fake CNI plugin executable that records its
// invocations (command + interface name) and the stdin it received, so tests
// can assert on how libcni drove it without requiring real plugins or root.
type fakePluginEnv struct {
	binDir   string
	cacheDir string
	logPath  string
	stdinDir string
}

func newFakePluginEnv(t *testing.T) *fakePluginEnv {
	t.Helper()
	env := &fakePluginEnv{
		binDir:   t.TempDir(),
		cacheDir: t.TempDir(),
		logPath:  filepath.Join(t.TempDir(), "invocations.log"),
		stdinDir: t.TempDir(),
	}

	// The fake plugin script logs "COMMAND IFNAME", dumps its stdin and, on
	// ADD, emits a canned CNI result. A repeated ADD for the same interface
	// fails, so tests catch broken Add idempotency. DEL clears the marker so
	// a later ADD is allowed again. Environment variables are inherited
	// from the test process (t.Setenv) since libcni execs plugins with
	// os.Environ.
	script := `#!/bin/sh
echo "$CNI_COMMAND $CNI_IFNAME" >> "$CNI_TEST_LOG"
if [ "$CNI_COMMAND" = "ADD" ] && [ -e "$CNI_TEST_STDIN_DIR/ADD-$CNI_IFNAME" ]; then
  echo '{"cniVersion":"1.0.0","code":11,"msg":"duplicate ADD for '"$CNI_IFNAME"'"}'
  exit 1
fi
cat > "$CNI_TEST_STDIN_DIR/$CNI_COMMAND-$CNI_IFNAME"
if [ "$CNI_COMMAND" = "ADD" ]; then
  echo '{"cniVersion":"1.0.0","interfaces":[{"name":"'"$CNI_IFNAME"'","sandbox":"'"$CNI_NETNS"'"}],"ips":[{"address":"192.168.1.10/24","interface":0}]}'
fi
if [ "$CNI_COMMAND" = "DEL" ]; then
  rm -f "$CNI_TEST_STDIN_DIR/ADD-$CNI_IFNAME"
fi
`
	if err := os.WriteFile(filepath.Join(env.binDir, "fake"), []byte(script), 0o755); err != nil {
		t.Fatalf("failed to write fake plugin: %v", err)
	}
	t.Setenv("CNI_TEST_LOG", env.logPath)
	t.Setenv("CNI_TEST_STDIN_DIR", env.stdinDir)
	return env
}

func (e *fakePluginEnv) params(ifName string, capabilityArgs map[string]any) AddParams {
	return AddParams{
		Config:         []byte(fakeConfList),
		NetNS:          netNS,
		IfName:         ifName,
		CapabilityArgs: capabilityArgs,
	}
}

func (e *fakePluginEnv) invoker() *Invoker {
	return newInvoker([]string{e.binDir}, e.cacheDir, "node1")
}

func (e *fakePluginEnv) loggedCommands(t *testing.T) []string {
	t.Helper()
	content, err := os.ReadFile(e.logPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatalf("failed to read invocation log: %v", err)
	}
	return strings.Split(strings.TrimSpace(string(content)), "\n")
}

func (e *fakePluginEnv) pluginStdin(t *testing.T, command, ifName string) map[string]any {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(e.stdinDir, fmt.Sprintf("%s-%s", command, ifName)))
	if err != nil {
		t.Fatalf("failed to read plugin stdin: %v", err)
	}
	stdin := map[string]any{}
	if err := json.Unmarshal(content, &stdin); err != nil {
		t.Fatalf("failed to parse plugin stdin %q: %v", string(content), err)
	}
	return stdin
}
