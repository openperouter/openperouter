// SPDX-License-Identifier:Apache-2.0

package hostnetwork

import (
	"errors"
	"reflect"
	"testing"

	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netlink/nl"
	"golang.org/x/sys/unix"
)

func TestGuardDumpFilteringFallback(t *testing.T) {
	for _, tc := range []struct {
		name    string
		table   int
		failure error
		calls   int
		wantErr error
	}{
		{name: "filtered", table: 701, calls: 1},
		{name: "all tables", calls: 1},
		{name: "old socket", table: 701, failure: unix.ENOPROTOOPT, calls: 2},
		{name: "unsupported dump", table: 701, failure: unix.EOPNOTSUPP, calls: 2},
		{name: "old request", table: 701, failure: unix.EINVAL, calls: 2},
		{name: "permission failure", table: 701, failure: unix.EPERM, calls: 1, wantErr: unix.EPERM},
		{name: "interrupted dump", table: 701, failure: netlink.ErrDumpInterrupted,
			calls: 1, wantErr: netlink.ErrDumpInterrupted},
	} {
		t.Run(tc.name, func(t *testing.T) {
			original := guardRouteDump
			t.Cleanup(func() { guardRouteDump = original })
			calls := 0
			guardRouteDump = func(family, table int, strict bool) ([][]byte, error) {
				calls++
				wantTable, wantStrict := tc.table, tc.table != 0
				if calls > 1 {
					wantTable, wantStrict = 0, false
				}
				if family != netlink.FAMILY_V6 || table != wantTable || strict != wantStrict {
					t.Fatalf("dump family=%d table=%d strict=%v", family, table, strict)
				}
				if calls == 1 {
					return nil, tc.failure
				}
				return nil, nil
			}
			filter := vrfDefaultGuard(uint32(tc.table), netlink.FAMILY_V6)
			_, err := listGuardRouteCandidates(netlink.FAMILY_V6, &filter, netlink.RT_FILTER_TABLE)
			if !errors.Is(err, tc.wantErr) || calls != tc.calls {
				t.Fatalf("calls=%d err=%v; want calls=%d err=%v", calls, err, tc.calls, tc.wantErr)
			}
		})
	}
}

func TestGuardRouteSourceSelector(t *testing.T) {
	for _, sourceBits := range []uint8{0, 64} {
		header := &nl.RtMsg{RtMsg: unix.RtMsg{
			Family: unix.AF_INET6, Src_len: sourceBits, Type: unix.RTN_UNREACHABLE,
			Protocol: uint8(vrfGuardProtocol),
		}}
		data := append(header.Serialize(), nl.NewRtAttr(unix.RTA_TABLE, nl.Uint32Attr(701)).Serialize()...)
		data = append(data, nl.NewRtAttr(unix.RTA_PRIORITY, nl.Uint32Attr(vrfGuardPriority)).Serialize()...)
		filter := vrfDefaultGuard(701, netlink.FAMILY_V6)
		got, err := decodeGuardRoute(data, netlink.FAMILY_V6, &filter)
		if err != nil {
			t.Fatal(err)
		}
		if sourceBits != 0 {
			if got != nil {
				t.Fatalf("source-specific route accepted as unconditional guard: %v", got)
			}
			continue
		}
		if !reflect.DeepEqual(got, &filter) {
			t.Fatalf("got %v; want unconditional guard %v", got, filter)
		}
	}
}

func TestRoutingTableRulesRetriesInterruptedDump(t *testing.T) {
	for _, tc := range []struct {
		name      string
		failures  int
		failure   error
		wantErr   error
		wantCalls int
	}{
		{name: "fresh snapshot", failures: 1, failure: netlink.ErrDumpInterrupted, wantCalls: 2},
		{name: "persistent interruption", failures: 3, failure: netlink.ErrDumpInterrupted,
			wantErr: netlink.ErrDumpInterrupted, wantCalls: 3},
		{name: "permission denied", failures: 1, failure: unix.EPERM, wantErr: unix.EPERM, wantCalls: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			want := []netlink.Rule{{Table: 701}}
			got, err := listRoutingTableRules(netlink.FAMILY_V6, func(int) ([]netlink.Rule, error) {
				calls++
				if calls <= tc.failures {
					return []netlink.Rule{{Table: 999}}, tc.failure
				}
				return want, nil
			})
			if !errors.Is(err, tc.wantErr) || calls != tc.wantCalls {
				t.Fatalf("err=%v, calls=%d", err, calls)
			}
			if tc.wantErr != nil {
				want = nil
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("got %v; want %v", got, want)
			}
		})
	}
}
