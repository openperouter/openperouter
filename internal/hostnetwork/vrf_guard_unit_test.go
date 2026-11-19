// SPDX-License-Identifier:Apache-2.0

package hostnetwork

import (
	"errors"
	"net"
	"reflect"
	"testing"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

func TestVRFGuardReconciliation(t *testing.T) {
	for _, tc := range []struct {
		name     string
		disabled string
		families []int
	}{
		{name: "dual stack", disabled: "0", families: []int{netlink.FAMILY_V4, netlink.FAMILY_V6}},
		{name: "IPv6 disabled", disabled: "1", families: []int{netlink.FAMILY_V4}},
		{name: "IPv6 unsupported", families: []int{netlink.FAMILY_V4}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			vrf := &netlink.Vrf{LinkAttrs: netlink.LinkAttrs{Name: "red"}, Table: 901}
			working := netlink.Route{
				Family: netlink.FAMILY_V4, Table: 901, Type: unix.RTN_UNICAST,
				Protocol: unix.RTPROT_BGP, Priority: 20, Gw: net.ParseIP("192.0.2.1"),
			}
			state := mockVRFGuards(t, tc.disabled)
			state.routes = []netlink.Route{working}
			for range 3 {
				if err := ensureVRFDefaultGuards(vrf); err != nil {
					t.Fatal(err)
				}
			}
			want := []netlink.Route{working}
			for _, family := range tc.families {
				want = append(want, vrfDefaultGuard(vrf.Table, family))
			}
			if !reflect.DeepEqual(want, state.routes) || state.adds != len(tc.families) {
				t.Fatalf("routes = %+v, adds = %d; want %+v", state.routes, state.adds, want)
			}
			// Deletion must be repaired even while a working FRR default exists.
			state.routes = []netlink.Route{working}
			if err := ensureVRFDefaultGuards(vrf); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(want, state.routes) {
				t.Fatalf("restored routes = %+v; want %+v", state.routes, want)
			}
		})
	}
}

func TestVRFGuardErrors(t *testing.T) {
	for _, tc := range []struct {
		name       string
		listErr    error
		addErr     error
		readErr    error
		disabled   string
		concurrent bool
		wantErr    error
	}{
		{name: "dump failure", listErr: unix.EINTR, wantErr: unix.EINTR},
		{name: "permission denied", addErr: unix.EPERM, wantErr: unix.EPERM},
		{name: "foreign route at reserved metric", addErr: unix.EEXIST, wantErr: unix.EEXIST},
		{name: "concurrent insertion", addErr: unix.EEXIST, concurrent: true},
		{name: "sysctl read failure", readErr: unix.EACCES, wantErr: unix.EACCES},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := mockVRFGuards(t, tc.disabled)
			state.listErr, state.addErr, state.concurrent = tc.listErr, tc.addErr, tc.concurrent
			guardIPv6Read = func(string) (string, error) { return tc.disabled, tc.readErr }
			err := ensureVRFDefaultGuards(&netlink.Vrf{LinkAttrs: netlink.LinkAttrs{Name: "red"}, Table: 901})
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("error = %v; want %v", err, tc.wantErr)
			}
		})
	}
}

func TestVRFGuardReservedTables(t *testing.T) {
	for _, table := range []uint32{unix.RT_TABLE_UNSPEC, unix.RT_TABLE_DEFAULT, unix.RT_TABLE_MAIN, unix.RT_TABLE_LOCAL} {
		state := mockVRFGuards(t, "0")
		if err := ensureVRFDefaultGuards(&netlink.Vrf{Table: table}); err == nil {
			t.Fatalf("accepted reserved table %d", table)
		}
		if state.lists != 0 || state.adds != 0 {
			t.Fatalf("accessed reserved table %d", table)
		}
	}
}

func TestVRFGuardIPv6FailurePreservesIPv4(t *testing.T) {
	state := mockVRFGuards(t, "0")
	add := guardRouteAdd
	guardRouteAdd = func(route *netlink.Route) error {
		if route.Family == netlink.FAMILY_V6 {
			return unix.EPERM
		}
		return add(route)
	}
	vrf := &netlink.Vrf{Table: 901}
	if err := ensureVRFDefaultGuards(vrf); !errors.Is(err, unix.EPERM) {
		t.Fatalf("error = %v; want EPERM", err)
	}
	want := []netlink.Route{vrfDefaultGuard(vrf.Table, netlink.FAMILY_V4)}
	if !reflect.DeepEqual(want, state.routes) {
		t.Fatalf("IPv4 guard removed on IPv6 failure: %+v", state.routes)
	}
}

func TestVRFGuardUnexpectedIPv6Setting(t *testing.T) {
	state := mockVRFGuards(t, "invalid")
	err := ensureVRFDefaultGuards(&netlink.Vrf{LinkAttrs: netlink.LinkAttrs{Name: "red"}, Table: 901})
	if err == nil || err.Error() != `read IPv6 availability for VRF red: unexpected IPv6 disable_ipv6 value "invalid" on all` {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(state.routes) != 1 || state.routes[0].Family != netlink.FAMILY_V4 {
		t.Fatalf("IPv4 guard missing on invalid IPv6 setting: %+v", state.routes)
	}
}

func TestVRFRejectsMixedModes(t *testing.T) {
	err := setupVRF("red", evpnVRF, srv6VRF)
	if err == nil || err.Error() != "VRF red cannot use both EVPN isolation and SRv6" {
		t.Fatalf("unexpected error for mixed modes: %v", err)
	}
}

func TestVRFGuardIPv6InterfaceOverrides(t *testing.T) {
	for _, tc := range []struct {
		name          string
		allState      string
		defaultState  string
		deviceState   string
		wantIPv6Guard bool
	}{
		{name: "future interfaces enabled", defaultState: "0", deviceState: "1", wantIPv6Guard: true},
		{name: "individual interface enabled", defaultState: "1", deviceState: "0", wantIPv6Guard: true},
		{name: "all zero but devices disabled", allState: "0", defaultState: "1", deviceState: "1"},
		{name: "all interfaces disabled", defaultState: "1", deviceState: "1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := mockVRFGuards(t, "1")
			guardIPv6Read = func(device string) (string, error) {
				switch device {
				case "all":
					if tc.allState != "" {
						return tc.allState, nil
					}
					return "1", nil
				case "default":
					return tc.defaultState, nil
				case "underlay":
					return tc.deviceState, nil
				default:
					return "1", nil
				}
			}
			guardLinkList = func() ([]netlink.Link, error) {
				return []netlink.Link{&netlink.Dummy{LinkAttrs: netlink.LinkAttrs{Name: "underlay"}}}, nil
			}
			vrf := &netlink.Vrf{LinkAttrs: netlink.LinkAttrs{Name: "red"}, Table: 901}
			if err := ensureVRFDefaultGuards(vrf); err != nil {
				t.Fatal(err)
			}
			want := []netlink.Route{vrfDefaultGuard(vrf.Table, netlink.FAMILY_V4)}
			if tc.wantIPv6Guard {
				want = append(want, vrfDefaultGuard(vrf.Table, netlink.FAMILY_V6))
			}
			if !reflect.DeepEqual(want, state.routes) {
				t.Fatalf("routes = %+v; want %+v", state.routes, want)
			}
		})
	}
}

func TestVRFGuardIdentity(t *testing.T) {
	for _, family := range []int{netlink.FAMILY_V4, netlink.FAMILY_V6} {
		state := mockVRFGuards(t, "0")
		guard := vrfDefaultGuard(901, family)
		for _, change := range []func(*netlink.Route){
			func(r *netlink.Route) { r.Table++ },
			func(r *netlink.Route) { r.Protocol = unix.RTPROT_BGP },
			func(r *netlink.Route) { r.Type = unix.RTN_UNICAST },
			func(r *netlink.Route) { r.Priority = 20 },
			func(r *netlink.Route) { r.Tos = 16 },
			func(r *netlink.Route) { r.Dst.Mask[0] = 255 },
		} {
			foreign := vrfDefaultGuard(901, family)
			change(&foreign)
			state.routes = append(state.routes, foreign)
		}
		routes, err := listVRFDefaultGuards(guard)
		if err != nil || len(routes) != 0 {
			t.Fatalf("foreign routes matched: %+v, error %v", routes, err)
		}
		guard.Dst = nil // Kernel dumps may represent the default prefix as nil.
		state.routes = append(state.routes, guard)
		routes, err = listVRFDefaultGuards(guard)
		if err != nil || !reflect.DeepEqual(routes, []netlink.Route{guard}) {
			t.Fatalf("owned route not matched: %+v, error %v", routes, err)
		}
	}
	if uint32(vrfGuardPriority)>>24 != 255 || uint32(vrfGuardPriority)&0x00ffffff != 8192 {
		t.Fatal("VRF guard priority must decode to FRR [255/8192]")
	}
}

func TestVRFGuardRemovalSnapshots(t *testing.T) {
	for _, tc := range []struct {
		name        string
		interrupts  int
		v6Error     error
		deleteError error
		wantError   error
		wantDeleted int
		wantV4Dumps int
	}{
		{name: "both families", wantDeleted: 2, wantV4Dumps: 1},
		{name: "fresh interrupted snapshot", interrupts: 1, wantDeleted: 2, wantV4Dumps: 2},
		{name: "persistent interrupted snapshot", interrupts: 3, wantError: netlink.ErrDumpInterrupted, wantV4Dumps: 3},
		{name: "IPv6 list failure deletes nothing", v6Error: unix.EPERM, wantError: unix.EPERM, wantV4Dumps: 1},
		{name: "IPv6 unavailable", v6Error: unix.EAFNOSUPPORT, wantDeleted: 1, wantV4Dumps: 1},
		{name: "delete failure", deleteError: unix.EPERM, wantError: unix.EPERM, wantDeleted: 1, wantV4Dumps: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mockVRFGuards(t, "0")
			var deleted []netlink.Route
			v4Dumps := 0
			guardRouteList = func(family int, _ *netlink.Route, _ uint64) ([]netlink.Route, error) {
				if family == netlink.FAMILY_V4 {
					v4Dumps++
					if v4Dumps <= tc.interrupts {
						return []netlink.Route{vrfDefaultGuard(999, family)}, netlink.ErrDumpInterrupted
					}
				}
				if family == netlink.FAMILY_V6 && tc.v6Error != nil {
					return nil, tc.v6Error
				}
				return []netlink.Route{vrfDefaultGuard(901, family)}, nil
			}
			guardRouteDel = func(route *netlink.Route) error {
				deleted = append(deleted, *route)
				return tc.deleteError
			}
			err := removeVRFDefaultGuards(&netlink.Vrf{LinkAttrs: netlink.LinkAttrs{Name: "red"}, Table: 901})
			if !errors.Is(err, tc.wantError) {
				t.Fatalf("remove error = %v; want %v", err, tc.wantError)
			}
			if len(deleted) != tc.wantDeleted || v4Dumps != tc.wantV4Dumps {
				t.Fatalf("deleted %v after %d dumps; want %d deletions and %d dumps", deleted, v4Dumps,
					tc.wantDeleted, tc.wantV4Dumps)
			}
			for _, route := range deleted {
				if route.Table != 901 {
					t.Fatalf("used interrupted snapshot: %v", route)
				}
			}
		})
	}
}

func TestGuardSnapshotAcrossVRFs(t *testing.T) {
	for _, tc := range []struct {
		name       string
		concurrent bool
		wantDumps  int
	}{
		{name: "normal additions", wantDumps: 2},
		{name: "concurrent additions require fresh dumps", concurrent: true, wantDumps: 6},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := mockVRFGuards(t, "0")
			if tc.concurrent {
				state.addErr, state.concurrent = unix.EEXIST, true
			}
			special := vrfDefaultGuard(901, netlink.FAMILY_V4)
			special.Tos = 16
			state.routes = []netlink.Route{special}
			snapshot := &vrfGuardSnapshot{}
			for _, table := range []uint32{901, 902, 901} {
				if err := ensureVRFDefaultGuardsWithSnapshot(&netlink.Vrf{LinkAttrs: netlink.LinkAttrs{Name: "tenant"}, Table: table}, snapshot); err != nil {
					t.Fatal(err)
				}
			}
			if state.lists != tc.wantDumps || state.adds != 4 {
				t.Fatalf("dumps=%d, adds=%d; want %d, 4", state.lists, state.adds, tc.wantDumps)
			}
			want := []netlink.Route{special, vrfDefaultGuard(901, netlink.FAMILY_V4), vrfDefaultGuard(901, netlink.FAMILY_V6), vrfDefaultGuard(902, netlink.FAMILY_V4), vrfDefaultGuard(902, netlink.FAMILY_V6)}
			if !reflect.DeepEqual(state.routes, want) {
				t.Fatalf("routes=%v; want %v", state.routes, want)
			}

		})
	}
}

func TestGuardSnapshotFindsExistingNonMainTables(t *testing.T) {
	state := mockVRFGuards(t, "0")
	state.routes = []netlink.Route{
		vrfDefaultGuard(901, netlink.FAMILY_V4), vrfDefaultGuard(901, netlink.FAMILY_V6),
		vrfDefaultGuard(902, netlink.FAMILY_V4), vrfDefaultGuard(902, netlink.FAMILY_V6),
	}
	snapshot := &vrfGuardSnapshot{}
	for _, table := range []uint32{901, 902} {
		vrf := &netlink.Vrf{LinkAttrs: netlink.LinkAttrs{Name: "tenant"}, Table: table}
		if err := ensureVRFDefaultGuardsWithSnapshot(vrf, snapshot); err != nil {
			t.Fatal(err)
		}
	}
	if state.lists != 2 || state.adds != 0 {
		t.Fatalf("dumps=%d, adds=%d; want 2, 0", state.lists, state.adds)
	}
}

type guardRouteState struct {
	routes     []netlink.Route
	listErr    error
	addErr     error
	concurrent bool
	adds       int
	lists      int
}

func mockVRFGuards(t *testing.T, disabled string) *guardRouteState {
	t.Helper()
	originalList, originalAdd, originalRead, originalLinks := guardRouteList, guardRouteAdd, guardIPv6Read, guardLinkList
	originalDelete := guardRouteDel
	t.Cleanup(func() {
		guardRouteList, guardRouteAdd, guardIPv6Read, guardLinkList = originalList, originalAdd, originalRead, originalLinks
		guardRouteDel = originalDelete
	})
	state := &guardRouteState{}
	guardLinkList = func() ([]netlink.Link, error) { return nil, nil }
	guardIPv6Read = func(string) (string, error) { return disabled, nil }
	guardRouteAdd = func(route *netlink.Route) error {
		state.adds++
		if state.addErr == nil || state.concurrent {
			state.routes = append(state.routes, *route)
		}
		return state.addErr
	}
	guardRouteList = func(family int, filter *netlink.Route, mask uint64) ([]netlink.Route, error) {
		state.lists++
		wantMask := netlink.RT_FILTER_TABLE | netlink.RT_FILTER_DST | netlink.RT_FILTER_TYPE |
			netlink.RT_FILTER_PRIORITY | netlink.RT_FILTER_PROTOCOL
		if mask&^netlink.RT_FILTER_TABLE != wantMask&^netlink.RT_FILTER_TABLE || filter.Dst != nil {
			t.Fatalf("unexpected filter %+v, mask %d", filter, mask)
		}
		var found []netlink.Route
		for _, route := range state.routes {
			if route.Family != family || (mask&netlink.RT_FILTER_TABLE != 0 && filter.Table != unix.RT_TABLE_UNSPEC && route.Table != filter.Table) || route.Type != filter.Type ||
				route.Protocol != filter.Protocol {
				continue
			}
			if route.Dst != nil {
				ones, _ := route.Dst.Mask.Size()
				if ones != 0 {
					continue
				}
			}
			found = append(found, route)
		}
		return found, state.listErr
	}
	return state
}
