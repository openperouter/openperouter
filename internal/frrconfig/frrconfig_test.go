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

func TestNeedsISISTeardown(t *testing.T) {
	const runningWithISIS = `interface lo
 ipv6 router isis ISIS
 isis passive
exit
!
interface toswitch1
 ipv6 router isis ISIS
exit
!
router isis ISIS
 net 49.0001.0002.0003.0004.00
exit
!`

	for name, tc := range map[string]struct {
		running string
		desired string
		want    []string
	}{
		"no isis in running config": {
			running: "router bgp 64512\nexit\n",
			desired: "router bgp 64512\nexit\n",
			want:    nil,
		},
		"isis kept in desired config": {
			running: runningWithISIS,
			desired: runningWithISIS,
			want:    nil,
		},
		"isis removed clears passive before instance": {
			running: runningWithISIS,
			desired: "router bgp 64512\nexit\n",
			want: []string{
				"configure terminal",
				"interface lo",
				"no isis passive",
				"exit",
				"no router isis ISIS",
				"end",
			},
		},
		"desired keeps only interface reference without instance": {
			running: runningWithISIS,
			desired: "interface lo\n ipv6 router isis ISIS\nexit\n",
			want: []string{
				"configure terminal",
				"interface lo",
				"no isis passive",
				"exit",
				"no router isis ISIS",
				"end",
			},
		},
		"multiple instances are all removed": {
			running: `interface lo
 isis passive
exit
!
router isis ISIS
 net 49.0001.0002.0003.0004.00
exit
!
router isis OTHER
 net 49.0002.0002.0003.0004.00
exit
!`,
			desired: "router bgp 64512\nexit\n",
			want: []string{
				"configure terminal",
				"interface lo",
				"no isis passive",
				"exit",
				"no router isis ISIS",
				"no router isis OTHER",
				"end",
			},
		},
		"multiple passive interfaces": {
			running: `interface lo
 isis passive
exit
!
interface toswitch1
 isis passive
exit
!
router isis ISIS
 net 49.0001.0002.0003.0004.00
exit
!`,
			desired: "router bgp 64512\nexit\n",
			want: []string{
				"configure terminal",
				"interface lo",
				"no isis passive",
				"exit",
				"interface toswitch1",
				"no isis passive",
				"exit",
				"no router isis ISIS",
				"end",
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			got := needsISISTeardown(tc.running, tc.desired)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("expected %v, got %v", tc.want, got)
			}
		})
	}
}

// helper function that redirects the execution to a mock process implemented by
// TestHelperProcess
func fakeExecCommand(name string, args ...string) *exec.Cmd {
	//nolint:prealloc
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

	// vtysh calls are invoked as "-c <command>". The reload tests never have
	// ISIS in the running config, so return an empty running config and let the
	// stale-ISIS teardown short-circuit.
	if len(args) > 0 && args[0] == "-c" {
		os.Exit(0)
	}

	if len(args) != 5 {
		fmt.Printf("expecting 5 args, got %v", args)
		os.Exit(1)
	}

	if !reflect.DeepEqual(args[0], reloaderPath) {
		fmt.Println("expected to be called with -c reloader args", args)
		os.Exit(1)
	}
	action, _ := strings.CutPrefix(args[1], "--")
	path := args[4]

	params, ok := tests[path]
	if !ok {
		fmt.Println("received untestable path", path)
		os.Exit(1)
	}
	if params.failReload && action == string(Reload) {
		fmt.Println("reload failed")
		os.Exit(1)
	}
	if params.failValidate && action == string(Test) {
		fmt.Println("test failed")
		os.Exit(1)
	}

	os.Exit(0)
}
