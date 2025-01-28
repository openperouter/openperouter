// SPDX-License-Identifier:Apache-2.0

package frrconfig

import (
	"fmt"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"
)

var tests = map[string]struct {
	failValidate bool
	failReload   bool
}{
	"/tmp/shouldPass": {
		failValidate: false,
		failReload:   false,
	},
	"/tmp/failValidate": {
		failValidate: true,
		failReload:   false,
	},
	"/tmp/failReload": {
		failValidate: false,
		failReload:   true,
	},
}

func TestReload(t *testing.T) {
	execCommand = fakeExecCommand
	defer func() { execCommand = exec.Command }()

	for tc, params := range tests {
		t.Run(fmt.Sprintf("reload %s", tc), func(t *testing.T) {
			err := Update(tc)
			if (params.failReload || params.failValidate) && err == nil {
				t.Fatalf("expecting failure, got no error")
			}
			if params.failReload && !strings.Contains(err.Error(), "reload") {
				t.Fatalf("expecting reload error, got %v", err)
			}
			if params.failValidate && !strings.Contains(err.Error(), "test") {
				t.Fatalf("expecting test error, got %v", err)
			}
			if !params.failReload && !params.failValidate && err != nil {
				t.Fatalf("expecting no error, got %v", err)
			}
		})
	}
}

// helper function that redirects the execution to a mock process implemented by
// TestHelperProcess
func fakeExecCommand(name string, args ...string) *exec.Cmd {
	cs := []string{"-test.run=TestFakeReloadHelper", "--"}
	cs = append(cs, args...)
	env := []string{
		"WANT_FAKE_PYTHON=true",
	}

	cmd := exec.Command(os.Args[0], cs...)
	cmd.Env = append(env, os.Environ()...)
	return cmd
}

// This is not a real test. It's used in case fakeExecCommand is used in place of exec.Command.
// In that case the command execution is redirected to this function.
func TestFakeReloadHelper(t *testing.T) {
	if os.Getenv("WANT_FAKE_PYTHON") != "true" {
		return
	}

	args := os.Args

	for len(args) > 0 {
		if args[0] == "--" {
			args = args[1:]
			break
		}
		args = args[1:]
	}
	if len(args) != 4 {
		fmt.Printf("expecting 4 args, got %v", args)
		os.Exit(1)
	}

	if !reflect.DeepEqual(args[:2], []string{"-c", reloaderPath}) {
		fmt.Println("expected to be called with -c reloader args", args)
		os.Exit(1)
	}
	actionParam, _ := strings.CutPrefix(args[2], "--")
	action := Action(actionParam)
	path := args[3]

	params, ok := tests[path]
	if !ok {
		fmt.Println("received untestable path", path)
		os.Exit(1)
	}
	if params.failReload && action == Reload {
		fmt.Println("reload failed")
		os.Exit(1)
	}
	if params.failValidate && action == Test {
		fmt.Println("test failed")
		os.Exit(1)
	}

	os.Exit(0)
}
