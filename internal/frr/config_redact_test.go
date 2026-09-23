// SPDX-License-Identifier: Apache-2.0

package frr

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestNeighborConfigPasswordRedactedInLogs(t *testing.T) {
	nc := NeighborConfig{
		ASN:      mustNewPeerASNFromNumber(64513),
		Addr:     "192.168.1.2",
		ID:       "192.168.1.2",
		Password: "SuperSecretBGPPassword123",
	}

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	logger.Info("test neighbor config", "neighbor", nc)

	want := `neighbor="{Name: ASN:64513 Addr:192.168.1.2 Interface: ID:192.168.1.2 Port:<nil> HoldTime:<nil> KeepaliveTime:<nil> ConnectTime:<nil> Password:<REDACTED> BFDEnabled:false BFDProfile: EBGPMultiHop:false EBGPMultiHopTTL:<nil> NetworkLayerProtocols:[] ListenRange: ExtendedNexthop:false UpdateSource:}"`
	if !strings.Contains(buf.String(), want) {
		t.Fatalf("log output does not contain expected redacted neighbor:\nwant substring: %s\ngot: %s", want, buf.String())
	}
}

func TestRenderedConfigPasswordRedacted(t *testing.T) {
	config := Config{
		Loglevel: "informational",
		Hostname: "testhost",
		Underlay: UnderlayConfig{
			MyASN:    64512,
			RouterID: "10.0.0.1",
			Neighbors: []NeighborConfig{
				{
					ASN:      mustNewPeerASNFromNumber(64513),
					Addr:     "192.168.1.2",
					ID:       "192.168.1.2",
					Password: "MyBGPSecret456",
				},
			},
		},
	}

	configString, err := templateConfig(&config)
	if err != nil {
		t.Fatalf("failed to render config: %v", err)
	}

	want := "log stdout informational\n" +
		"log timestamp precision 3\n" +
		"hostname testhost\n" +
		"ip nht resolve-via-default\n" +
		"ipv6 nht resolve-via-default\n" +
		"\n" +
		"route-map allowall permit 1\n" +
		"router bgp 64512\n" +
		"  no bgp ebgp-requires-policy\n" +
		"  no bgp network import-check\n" +
		"  no bgp default ipv4-unicast\n" +
		"  bgp router-id 10.0.0.1\n" +
		"  neighbor 192.168.1.2 remote-as 64513\n" +
		"  \n" +
		"  \n" +
		"  neighbor 192.168.1.2 password <REDACTED>\n" +
		"\n" +
		"exit\n" +
		"!\n"

	got := RedactPasswords(configString)
	if got != want {
		t.Fatalf("redacted config mismatch:\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestFRRReloadOutputPasswordRedacted(t *testing.T) {
	output := "Reloading frr.conf\n" +
		"+neighbor 192.168.1.2 password MyBGPSecret789\n" +
		"-neighbor 192.168.1.2 password OldPassword123\n" +
		" neighbor 192.168.1.2 remote-as 64513"

	want := "Reloading frr.conf\n" +
		"+neighbor 192.168.1.2 password <REDACTED>\n" +
		"-neighbor 192.168.1.2 password <REDACTED>\n" +
		" neighbor 192.168.1.2 remote-as 64513"

	got := RedactPasswords(output)
	if got != want {
		t.Fatalf("redacted reload output mismatch:\nwant:\n%s\ngot:\n%s", want, got)
	}
}
