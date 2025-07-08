// SPDX-License-Identifier:Apache-2.0

package hostnetwork

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/vishvananda/netns"
)

func TestEnsureIPv6Forwarding(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("Test requires root privileges")
	}

	testNS := "test-ipv6-forwarding"
	ns, err := netns.NewNamed(testNS)
	if err != nil {
		t.Fatalf("failed to create test namespace: %v", err)
	}
	defer func() {
		ns.Close()
		netns.DeleteNamed(testNS)
	}()

	err = EnsureIPv6Forwarding(testNS)
	if err != nil {
		t.Fatalf("failed to ensure IPv6 forwarding: %v", err)
	}

	var output string
	var checkErr error
	err = inNamespace(ns, func() error {
		out, err := exec.Command("sysctl", "-n", "net.ipv6.conf.all.forwarding").CombinedOutput()
		output = string(out)
		checkErr = err
		return err
	})
	if err != nil {
		t.Fatalf("failed to check IPv6 forwarding status: %v", err)
	}
	if checkErr != nil {
		t.Fatalf("failed to check IPv6 forwarding status: %v", checkErr)
	}

	if strings.TrimSpace(output) != "1" {
		t.Errorf("expected IPv6 forwarding to be enabled (1), got: %s", strings.TrimSpace(output))
	}
}

func TestEnsureIPv6ForwardingAlreadyEnabled(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("Test requires root privileges")
	}

	testNS := "test-ipv6-forwarding-already"
	ns, err := netns.NewNamed(testNS)
	if err != nil {
		t.Fatalf("failed to create test namespace: %v", err)
	}
	defer func() {
		ns.Close()
		netns.DeleteNamed(testNS)
	}()

	var setErr error
	err = inNamespace(ns, func() error {
		_, setErr = exec.Command("sysctl", "-w", "net.ipv6.conf.all.forwarding=1").CombinedOutput()
		return setErr
	})
	if err != nil {
		t.Fatalf("failed to enable IPv6 forwarding: %v", err)
	}
	if setErr != nil {
		t.Fatalf("failed to enable IPv6 forwarding: %v", setErr)
	}

	err = EnsureIPv6Forwarding(testNS)
	if err != nil {
		t.Fatalf("failed to ensure IPv6 forwarding when already enabled: %v", err)
	}

	var output string
	var checkErr error
	err = inNamespace(ns, func() error {
		out, err := exec.Command("sysctl", "-n", "net.ipv6.conf.all.forwarding").CombinedOutput()
		output = string(out)
		checkErr = err
		return err
	})
	if err != nil {
		t.Fatalf("failed to check IPv6 forwarding status: %v", err)
	}
	if checkErr != nil {
		t.Fatalf("failed to check IPv6 forwarding status: %v", checkErr)
	}

	if strings.TrimSpace(output) != "1" {
		t.Errorf("expected IPv6 forwarding to be enabled (1), got: %s", strings.TrimSpace(output))
	}
}
