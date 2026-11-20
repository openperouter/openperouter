// SPDX-License-Identifier:Apache-2.0

package systemd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRebootNodeVerifiesRestart(t *testing.T) {
	tests := []struct {
		name    string
		nextPID string
		wantErr string
	}{
		{name: "new init process", nextPID: "202"},
		{name: "unchanged init process", nextPID: "101", wantErr: "init PID has not changed"},
		{name: "stopped node", nextPID: "0", wantErr: "is not running"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			pidFile := filepath.Join(dir, "pid")
			if err := os.WriteFile(pidFile, []byte("101"), 0o600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("REBOOT_TEST_PID_FILE", pidFile)
			t.Setenv("REBOOT_TEST_NEXT_PID", tt.nextPID)
			runtime := filepath.Join(dir, "runtime")
			if err := os.WriteFile(runtime, []byte(`#!/bin/sh
case "$1" in
inspect) /bin/cat "$REBOOT_TEST_PID_FILE" ;;
exec) exit 0 ;;
*) exit 1 ;;
esac
`), 0o700); err != nil {
				t.Fatal(err)
			}
			restart := filepath.Join(dir, "restart.sh")
			restartScript := `printf %s "$REBOOT_TEST_NEXT_PID" > "$REBOOT_TEST_PID_FILE"`
			if err := os.WriteFile(restart, []byte(restartScript), 0o600); err != nil {
				t.Fatal(err)
			}
			node := RebootNode{Name: "test-node", Runtime: runtime, RestartScript: restart}
			err := node.Reboot()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("want error containing %q, got %v", tt.wantErr, err)
			}
		})
	}
}
