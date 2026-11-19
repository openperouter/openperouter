// SPDX-License-Identifier:Apache-2.0

package tests

import (
	"errors"
	"strings"
	"testing"
)

func TestIsolationClusterPingSeparatesExecutionFailure(t *testing.T) {
	executionError := errors.New("pod exec failed")
	for _, tc := range []struct {
		name, output   string
		executionError error
		want           bool
		wantError      string
	}{
		{name: "delivered", output: "ping output\nPING_RESULT=0\n", want: true},
		{name: "unreachable", output: "ping output\nPING_RESULT=1\n"},
		{name: "local route rejection", output: "ping output\nPING_RESULT=2\n"},
		{name: "execution failed despite marker", output: "\nPING_RESULT=0\n", executionError: executionError, wantError: "pod exec failed"},
		{name: "no result", output: "connection closed", wantError: "missing tenant ping result"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := isolationClusterPing(isolationPingExecutor{output: tc.output, err: tc.executionError}, "2001:db8::2")
			if result != tc.want {
				t.Fatalf("ping result=%t; want %t", result, tc.want)
			}
			if tc.wantError == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("error=%v; want %q", err, tc.wantError)
			}
			if tc.executionError != nil && !errors.Is(err, tc.executionError) {
				t.Fatalf("lost exec error: %v", err)
			}
		})
	}
}

type isolationPingExecutor struct {
	output string
	err    error
}

func (e isolationPingExecutor) Exec(_ string, _ ...string) (string, error) { return e.output, e.err }
