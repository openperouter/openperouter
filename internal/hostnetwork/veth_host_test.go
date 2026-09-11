// SPDX-License-Identifier:Apache-2.0

//go:build runasroot

package hostnetwork

import (
	"net"
	"runtime"
	"testing"

	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
)

func TestVethInHostNamespace(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	original, err := netns.Get()
	if err != nil {
		t.Fatal(err)
	}
	defer original.Close()
	ns, err := netns.NewNamed("vnf-veth-test")
	if err != nil {
		t.Fatal(err)
	}
	defer ns.Close()
	defer netns.DeleteNamed("vnf-veth-test")
	defer func() {
		if err := netns.Set(original); err != nil {
			t.Fatalf("restore runner namespace: %v", err)
		}
	}()
	names := VethNames{HostSide: "hostleg", NamespaceSide: "routerleg"}
	for range 2 {
		if err := setupNamespacedVeth(t.Context(), names, "/run/netns/vnf-veth-test"); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{names.HostSide, names.NamespaceSide} {
			link, err := netlink.LinkByName(name)
			if err != nil {
				t.Fatal(err)
			}
			if link.Attrs().Flags&net.FlagUp == 0 {
				t.Fatalf("veth %s is down", name)
			}
		}
	}
}
