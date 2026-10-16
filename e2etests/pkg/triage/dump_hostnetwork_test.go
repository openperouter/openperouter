// SPDX-License-Identifier:Apache-2.0

package triage

import (
	"testing"
)

// singleRowStats is the "ip -s link show" layout printed by the iproute2 in the
// router namespace: one counter row per section.
const singleRowStats = `1: lo: <LOOPBACK,UP,LOWER_UP> mtu 65536 qdisc noqueue state UNKNOWN mode DEFAULT group default qlen 1000
    link/loopback 00:00:00:00:00:00 brd 00:00:00:00:00:00
    RX:  bytes packets errors dropped  missed   mcast
             0       0      0       0       0       0
    TX:  bytes packets errors dropped carrier collsns
             0       0      0       0       0       0
4: toswitch1: <BROADCAST,MULTICAST,UP,LOWER_UP> mtu 1500 qdisc fq_codel state UP mode DEFAULT group 4242 qlen 1000
    link/ether 52:54:00:ee:00:00 brd ff:ff:ff:ff:ff:ff
    RX:  bytes packets errors dropped  missed   mcast
        732706    2746      0     370       0       0
    TX:  bytes packets errors dropped carrier collsns
        136373    1019      0       0       0       0
    altname enx525400ee0000
    altname enp6s0
67: pe-e-110@if68: <BROADCAST,MULTICAST,UP,LOWER_UP> mtu 1450 qdisc noqueue master br-pe-110 state UP mode DEFAULT group default
    link/ether 3e:ee:28:d7:63:48 brd ff:ff:ff:ff:ff:ff link-netns 6e1b1a46
    RX:  bytes packets errors dropped  missed   mcast
          4926      67      0       0       0       0
    TX:  bytes packets errors dropped carrier collsns
          1986      23      0       9       0       0
`

// extendedRowStats is the layout that also prints the extended counters on a
// second row, which must not be mistaken for the section counters.
const extendedRowStats = `39: vni110: <BROADCAST,MULTICAST,UP,LOWER_UP> mtu 65486 qdisc noqueue master br-pe-110 state UNKNOWN mode DEFAULT group default
    link/ether e6:0d:a0:27:f2:97 brd ff:ff:ff:ff:ff:ff
    RX: bytes  packets  errors  dropped overrun mcast
    0          0        0       0       0       0
    RX errors: length   crc     frame   fifo    missed
               1        2       3       4       5
    TX: bytes  packets  errors  dropped carrier collsns
    3912       66       0       20      0       0
    TX errors: aborted  fifo   window heartbeat transns
               6        7       8       9       10
`

func TestParseLinkStats(t *testing.T) {
	tests := []struct {
		name     string
		output   string
		expected map[string]linkStats
	}{
		{
			name:   "one counter row per section",
			output: singleRowStats,
			expected: map[string]linkStats{
				"lo":        {},
				"toswitch1": {rxPackets: 2746, rxDropped: 370, txPackets: 1019},
				"pe-e-110":  {rxPackets: 67, txPackets: 23, txDropped: 9},
			},
		},
		{
			name:   "extended counter rows are ignored",
			output: extendedRowStats,
			expected: map[string]linkStats{
				"vni110": {txPackets: 66, txDropped: 20},
			},
		},
		{
			name:     "command output that is not an interface listing",
			output:   "Cannot open network namespace \"perouter\": No such file or directory\n",
			expected: map[string]linkStats{},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			parsed := parseLinkStats(test.output)
			if len(parsed) != len(test.expected) {
				t.Fatalf("parsed %d interfaces, expected %d: %v", len(parsed), len(test.expected), parsed)
			}
			for name, expected := range test.expected {
				if parsed[name] != expected {
					t.Errorf("interface %s: got %+v, expected %+v", name, parsed[name], expected)
				}
			}
		})
	}
}

func TestRemoteFDBEntries(t *testing.T) {
	fdb := `00:f3:00:00:00:6f dev br-pe-110 vlan 1 master br-pe-110 permanent
1e:27:96:8f:4a:55 dev vni110 vlan 1 extern_learn master br-pe-110
00:00:00:00:00:00 dev vni110 dst 100.65.0.1 self permanent
1e:27:96:8f:4a:55 dev vni110 dst 100.65.0.1 self extern_learn
4a:67:b9:7b:da:9d dev pe-e-110 master br-pe-110
`
	expected := `00:00:00:00:00:00 dev vni110 dst 100.65.0.1 self permanent
1e:27:96:8f:4a:55 dev vni110 dst 100.65.0.1 self extern_learn
`
	if entries := remoteFDBEntries(fdb); entries != expected {
		t.Errorf("got:\n%s\nexpected:\n%s", entries, expected)
	}

	if entries := remoteFDBEntries(""); entries != "" {
		t.Errorf("an empty forwarding database should yield no entries, got %q", entries)
	}
}
