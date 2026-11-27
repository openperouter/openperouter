// SPDX-License-Identifier:Apache-2.0

package hostnetwork

import (
	"errors"
	"testing"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

func TestInterfaceHasIP(t *testing.T) {
	onLink := []netlink.Addr{
		*mustParseAddr(t, "192.168.1.1/24"),
		*mustParseAddr(t, "2001:db8::1/64"),
	}

	tests := []struct {
		name    string
		address string
		want    bool
	}{
		{name: "IPv4 address on the link", address: "192.168.1.1/24", want: true},
		{name: "IPv4 address with another prefix length", address: "192.168.1.1/25", want: false},
		{name: "IPv4 address not on the link", address: "192.168.1.2/24", want: false},
		{name: "IPv6 address on the link", address: "2001:db8::1/64", want: true},
		{name: "IPv6 address with its zeros written out", address: "2001:db8:0:0::1/64", want: true},
		{name: "IPv6 address in upper case", address: "2001:DB8::1/64", want: true},
		{name: "IPv6 address with another prefix length", address: "2001:db8::1/48", want: false},
		{name: "IPv6 address not on the link", address: "2001:db8::2/64", want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stubAddrList(t, onLink, nil)
			got, err := interfaceHasIP(testLink(), tc.address)
			if err != nil {
				t.Fatalf("interfaceHasIP(%s) failed: %v", tc.address, err)
			}
			if got != tc.want {
				t.Errorf("interfaceHasIP(%s) = %v, want %v", tc.address, got, tc.want)
			}
		})
	}
}

func TestInterfaceHasIPErrors(t *testing.T) {
	t.Run("invalid address", func(t *testing.T) {
		stubAddrList(t, nil, nil)
		if _, err := interfaceHasIP(testLink(), "not-an-address"); err == nil {
			t.Fatal("expected an error for an invalid address")
		}
	})
	t.Run("listing the addresses fails", func(t *testing.T) {
		listErr := errors.New("list failed")
		stubAddrList(t, nil, listErr)
		if _, err := interfaceHasIP(testLink(), "192.168.1.1/24"); !errors.Is(err, listErr) {
			t.Fatalf("expected the list error, got %v", err)
		}
	})
}

func TestOnlyLinkLocal(t *testing.T) {
	tests := []struct {
		name    string
		address string
		want    bool
	}{
		{name: "IPv6 link-local address", address: "fe80::1/64", want: true},
		{name: "IPv6 global address", address: "2001:db8::1/64", want: false},
		{name: "IPv4 address", address: "192.0.2.1/24", want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			addr := mustParseAddr(t, tc.address)
			if got := OnlyLinkLocal()(*addr); got != tc.want {
				t.Errorf("OnlyLinkLocal()(%s) = %v, want %v", tc.address, got, tc.want)
			}
		})
	}
}

func TestAutoconfigured(t *testing.T) {
	tests := []struct {
		name    string
		address string
		flags   int
		want    bool
	}{
		{name: "IPv6 address with a finite lifetime", address: "2001:db8::1/64", want: true},
		{name: "permanent IPv6 address", address: "2001:db8::1/64", flags: unix.IFA_F_PERMANENT, want: false},
		{name: "IPv6 link-local address", address: "fe80::1/64", flags: 0, want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			addr := mustParseAddr(t, tc.address)
			addr.Flags = tc.flags
			if got := Autoconfigured()(*addr); got != tc.want {
				t.Errorf("Autoconfigured()(%s, flags %d) = %v, want %v", tc.address, tc.flags, got, tc.want)
			}
		})
	}
}

func TestMatchesAddressFilters(t *testing.T) {
	permanent := mustParseAddr(t, "2001:db8::1/64")
	permanent.Flags = unix.IFA_F_PERMANENT
	tests := []struct {
		name    string
		addr    *netlink.Addr
		filters []AddressFilter
		want    bool
	}{
		{name: "no filters keeps everything", addr: permanent, want: true},
		{name: "one matching filter", addr: permanent, filters: []AddressFilter{ExcludeLinkLocal()}, want: true},
		{name: "one non-matching filter", addr: permanent, filters: []AddressFilter{OnlyLinkLocal()}, want: false},
		{
			name:    "every filter must match",
			addr:    permanent,
			filters: []AddressFilter{ExcludeLinkLocal(), Autoconfigured()},
			want:    false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := matchesAddressFilters(*tc.addr, tc.filters); got != tc.want {
				t.Errorf("matchesAddressFilters(%s) = %v, want %v", tc.addr, got, tc.want)
			}
		})
	}
}

func stubAddrList(t *testing.T, addrs []netlink.Addr, err error) {
	t.Helper()
	orig := addrList
	addrList = func(netlink.Link, int) ([]netlink.Addr, error) {
		return addrs, err
	}
	t.Cleanup(func() { addrList = orig })
}

func testLink() netlink.Link {
	return &netlink.Dummy{LinkAttrs: netlink.LinkAttrs{Name: "test0"}}
}

func mustParseAddr(t *testing.T, s string) *netlink.Addr {
	t.Helper()
	addr, err := netlink.ParseAddr(s)
	if err != nil {
		t.Fatalf("failed to parse %s: %v", s, err)
	}
	return addr
}
