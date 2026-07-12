// SPDX-License-Identifier: Apache-2.0

package frr

import (
	"bytes"
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

// TestNeighborConfigPasswordRedactedInLogs verifies that NeighborConfig
// does NOT expose the Password field when logged via slog.
func TestNeighborConfigPasswordRedactedInLogs(t *testing.T) {
	secret := "SuperSecretBGPPassword123"
	nc := NeighborConfig{
		ASN:      mustNewPeerASNFromNumber(64513),
		Addr:     "192.168.1.2",
		ID:       "192.168.1.2",
		Password: secret,
	}

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	logger.Info("test neighbor config", "neighbor", nc)

	if strings.Contains(buf.String(), secret) {
		t.Fatalf("cleartext password %q found in log output:\n%s", secret, buf.String())
	}
}

// TestRenderedConfigPasswordRedacted verifies that the rendered FRR
// config string does NOT contain cleartext passwords when redacted.
func TestRenderedConfigPasswordRedacted(t *testing.T) {
	secret := "MyBGPSecret456"
	config := Config{
		Underlay: UnderlayConfig{
			MyASN:    64512,
			RouterID: "10.0.0.1",
			Neighbors: []NeighborConfig{
				{
					ASN:      mustNewPeerASNFromNumber(64513),
					Addr:     "192.168.1.2",
					ID:       "192.168.1.2",
					Password: secret,
				},
			},
		},
	}

	configString, err := templateConfig(&config)
	if err != nil {
		t.Fatalf("failed to render config: %v", err)
	}

	redacted := RedactPasswords(configString)
	if strings.Contains(redacted, secret) {
		t.Fatalf("cleartext password %q found in redacted config:\n%s", secret, redacted)
	}

	if !strings.Contains(redacted, "password") {
		t.Fatal("redacted config lost the password line entirely — should keep the line with a redacted value")
	}
}

// TestFRRReloadOutputPasswordRedacted verifies that frr-reload.py
// output containing password lines gets redacted before logging,
// while preserving non-sensitive reload diagnostics.
func TestFRRReloadOutputPasswordRedacted(t *testing.T) {
	output := `Reloading frr.conf
+neighbor 192.168.1.2 password MyBGPSecret789
-neighbor 192.168.1.2 password OldPassword123
 neighbor 192.168.1.2 remote-as 64513`

	redacted := RedactPasswords(output)

	for _, secret := range []string{"MyBGPSecret789", "OldPassword123"} {
		if strings.Contains(redacted, secret) {
			t.Fatalf("cleartext password %q found in redacted reload output:\n%s",
				secret, redacted)
		}
	}

	for _, want := range []string{
		"+neighbor 192.168.1.2 password",
		"-neighbor 192.168.1.2 password",
		"neighbor 192.168.1.2 remote-as 64513",
	} {
		if !strings.Contains(redacted, want) {
			t.Fatalf("redaction unexpectedly removed reload output %q:\n%s", want, redacted)
		}
	}
}

// TestPasswordFromSecretRendersCorrectly verifies the FRR config renders
// correctly when the password is resolved from a Kubernetes Secret.
func TestPasswordFromSecretRendersCorrectly(t *testing.T) {
	resolvedPassword := "resolved-from-k8s-secret"
	nc := NeighborConfig{
		ASN:      mustNewPeerASNFromNumber(64513),
		Addr:     "192.168.1.2",
		ID:       "192.168.1.2",
		Password: resolvedPassword,
	}

	config := Config{
		Underlay: UnderlayConfig{
			MyASN:     64512,
			RouterID:  "10.0.0.1",
			Neighbors: []NeighborConfig{nc},
		},
	}

	configString, err := templateConfig(&config)
	if err != nil {
		t.Fatalf("failed to render config: %v", err)
	}

	expected := fmt.Sprintf("neighbor 192.168.1.2 password %s", resolvedPassword)
	if !strings.Contains(configString, expected) {
		t.Fatalf("rendered FRR config does not contain expected password line %q\n%s",
			expected, configString)
	}
}
