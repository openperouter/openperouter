//go:build cniroot

// SPDX-License-Identifier:Apache-2.0

// Root-only integration test. Build and run it as root with the CNI plugin
// binaries available, e.g.:
//
//	GOBIN=/tmp/cnibin CGO_ENABLED=0 go install github.com/containernetworking/plugins/plugins/main/macvlan@v1.7.1
//	GOBIN=/tmp/cnibin CGO_ENABLED=0 go install github.com/containernetworking/plugins/plugins/ipam/static@v1.7.1
//	go test -c -tags cniroot -o /tmp/cni.itest ./internal/cni/
//	sudo CNI_BIN_DIR=/tmp/cnibin /tmp/cni.itest -test.v
//
// It proves the full mechanism: a subnet + node index yields a per-node IP via
// ipam.UnderlayIPs, which is passed to a NAD-defined macvlan (capabilities.ips +
// static IPAM) via the CNI "ips" capability and applied inside a named netns.
package cni_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/openperouter/openperouter/internal/cni"
	"github.com/openperouter/openperouter/internal/ipam"
)

func TestAddUnderlayIntoNetnsRoot(t *testing.T) {
	binDir := os.Getenv("CNI_BIN_DIR")
	if binDir == "" {
		t.Skip("set CNI_BIN_DIR to a dir containing the macvlan + static plugins")
	}
	if os.Geteuid() != 0 {
		t.Skip("must run as root")
	}

	const nsName = "cni-itest"
	const nsPath = "/var/run/netns/" + nsName
	const parent = "ulitest0"
	const ifName = "underlay0"
	const nodeIndex = 4 // -> host 5 of the /24
	cacheDir := t.TempDir()

	mustRun(t, "ip", "netns", "add", nsName)
	defer run("ip", "netns", "del", nsName)
	mustRun(t, "ip", "link", "add", parent, "type", "dummy")
	defer run("ip", "link", "del", parent)
	mustRun(t, "ip", "link", "set", parent, "up")

	addrs, err := ipam.UnderlayIPs([]string{"192.168.11.0/24"}, nodeIndex)
	if err != nil {
		t.Fatalf("UnderlayIPs: %v", err)
	}
	if addrs[0] != "192.168.11.5/24" {
		t.Fatalf("expected 192.168.11.5/24 for node %d, got %s", nodeIndex, addrs[0])
	}

	// The NAD declares the "ips" capability + static IPAM; the IP is supplied at
	// runtime via the "ips" capability (Params.Addresses).
	nadConfig := fmt.Sprintf(
		`{"cniVersion":"1.0.0","name":"underlay","plugins":[{"type":"macvlan","master":%q,"mode":"bridge","capabilities":{"ips":true},"ipam":{"type":"static"}}]}`,
		parent)

	p := cni.Params{
		Config:      []byte(nadConfig),
		NetNS:       nsPath,
		IfName:      ifName,
		ContainerID: "perouter",
		BinDirs:     []string{binDir},
		CacheDir:    cacheDir,
		Addresses:   addrs,
	}
	if _, err := cni.Add(context.Background(), p); err != nil {
		t.Fatalf("cni.Add: %v", err)
	}
	defer func() { _ = cni.Del(context.Background(), p) }()

	out := mustOut(t, "ip", "netns", "exec", nsName, "ip", "-br", "addr", "show", ifName)
	if !strings.Contains(out, "192.168.11.5/24") {
		t.Fatalf("interface %s did not get the assigned IP; got: %s", ifName, out)
	}
	t.Logf("underlay interface in %s: %s", nsPath, strings.TrimSpace(out))
}

// TestDelCachedRemovesInterfaceRoot proves the teardown path: after ADD, the
// interface is removed using ONLY the libcni cache (no live config supplied),
// which is what RemoveUnderlay does when the underlay/NAD is already gone.
func TestDelCachedRemovesInterfaceRoot(t *testing.T) {
	binDir := os.Getenv("CNI_BIN_DIR")
	if binDir == "" {
		t.Skip("set CNI_BIN_DIR to a dir containing the macvlan + static plugins")
	}
	if os.Geteuid() != 0 {
		t.Skip("must run as root")
	}

	const nsName = "cni-del-itest"
	const nsPath = "/var/run/netns/" + nsName
	const parent = "uldel0"
	const ifName = "underlay0"
	const containerID = "perouter-underlay"
	cacheDir := t.TempDir()

	mustRun(t, "ip", "netns", "add", nsName)
	defer run("ip", "netns", "del", nsName)
	mustRun(t, "ip", "link", "add", parent, "type", "dummy")
	defer run("ip", "link", "del", parent)
	mustRun(t, "ip", "link", "set", parent, "up")

	addrs, err := ipam.UnderlayIPs([]string{"192.168.11.10/24"}, 0)
	if err != nil {
		t.Fatalf("UnderlayIPs: %v", err)
	}
	nadConfig := fmt.Sprintf(
		`{"cniVersion":"1.0.0","name":"underlay","plugins":[{"type":"macvlan","master":%q,"mode":"bridge","capabilities":{"ips":true},"ipam":{"type":"static"}}]}`,
		parent)

	p := cni.Params{
		Config:      []byte(nadConfig),
		NetNS:       nsPath,
		IfName:      ifName,
		ContainerID: containerID,
		BinDirs:     []string{binDir},
		CacheDir:    cacheDir,
		Addresses:   addrs,
	}
	if _, err := cni.Add(context.Background(), p); err != nil {
		t.Fatalf("cni.Add: %v", err)
	}
	if out := mustOut(t, "ip", "netns", "exec", nsName, "ip", "-br", "addr", "show", ifName); !strings.Contains(out, "192.168.11.10/24") {
		t.Fatalf("setup: interface %s missing its IP: %s", ifName, out)
	}

	// Tear down using only the cache (no Config passed), mirroring RemoveUnderlay.
	if err := cni.DelCached(context.Background(), []string{binDir}, cacheDir, containerID); err != nil {
		t.Fatalf("cni.DelCached: %v", err)
	}

	if err := run("ip", "netns", "exec", nsName, "ip", "link", "show", ifName); err == nil {
		t.Fatalf("interface %s is still present after DelCached", ifName)
	}
	t.Logf("DelCached removed %s from %s", ifName, nsPath)
}

func mustRun(t *testing.T, name string, args ...string) {
	t.Helper()
	if err := run(name, args...); err != nil {
		t.Fatalf("%s %v: %v", name, args, err)
	}
}

func run(name string, args ...string) error {
	return exec.Command(name, args...).Run()
}

func mustOut(t *testing.T, name string, args ...string) string {
	t.Helper()
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
	return string(out)
}
