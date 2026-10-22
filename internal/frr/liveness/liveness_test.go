// SPDX-License-Identifier:Apache-2.0

package liveness

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openperouter/openperouter/internal/frr/vtysh"
)

func TestWatchfrr(t *testing.T) {
	tests := []struct {
		desc            string
		pidFileContent  string
		processError    error
		vtyshError      error
		expectedCommand string
		expectedError   string
	}{
		{
			desc:          "watchfrr has not started yet",
			expectedError: "watchfrr has not started",
		},
		{
			desc:            "watchfrr is alive and answers",
			pidFileContent:  "123\n",
			expectedCommand: "show watchfrr",
			expectedError:   "",
		},
		{
			desc:           "pid file contains an invalid pid",
			pidFileContent: "not-a-pid\n",
			expectedError:  "invalid pid",
		},
		{
			desc:           "watchfrr is not running",
			pidFileContent: "123\n",
			processError:   fmt.Errorf("no such process"),
			expectedError:  "watchfrr 123 is not running",
		},
		{
			desc:            "watchfrr does not answer",
			pidFileContent:  "123\n",
			vtyshError:      fmt.Errorf("timed out"),
			expectedCommand: "show watchfrr",
			expectedError:   "watchfrr 123 does not answer on its vty",
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			pidFile := filepath.Join(t.TempDir(), "watchfrr.pid")
			if test.pidFileContent != "" {
				if err := os.WriteFile(pidFile, []byte(test.pidFileContent), 0o600); err != nil {
					t.Fatalf("failed to write pid file: %v", err)
				}
			}

			var gotCmd string
			cli := vtysh.Cli(func(args string) (string, error) {
				gotCmd = args
				return "", test.vtyshError
			})

			err := WatchFRR(pidFile, cli, func(int) error {
				return test.processError
			})

			if test.expectedError == "" && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if test.expectedError != "" {
				if err == nil || !strings.Contains(err.Error(), test.expectedError) {
					t.Fatalf("expected error containing %q, got %v", test.expectedError, err)
				}
			}
			if gotCmd != test.expectedCommand {
				t.Errorf("vtysh command = %q, want %q", gotCmd, test.expectedCommand)
			}
		})
	}
}
