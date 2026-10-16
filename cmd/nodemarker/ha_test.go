/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeKeyPair(t *testing.T, dir string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "openpe-webhook-service.test.svc"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	if err := os.WriteFile(filepath.Join(dir, servingCertName), certPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, servingKeyName), keyPEM, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestWaitForServingCertWaitsForAUsablePair(t *testing.T) {
	dir := t.TempDir()
	// What the chart's empty secret mounts as before the leader writes it: empty files.
	for _, f := range []string{servingCertName, servingKeyName} {
		if err := os.WriteFile(filepath.Join(dir, f), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	done := make(chan error, 1)
	go func() {
		done <- waitForServingCert(context.Background(), dir, 10*time.Millisecond)
	}()

	select {
	case err := <-done:
		t.Fatalf("returned before a key pair was there: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	writeKeyPair(t, dir)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("did not return after the key pair was written")
	}
}

func TestWaitForServingCertStopsWithTheContext(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err := waitForServingCert(ctx, t.TempDir(), 10*time.Millisecond)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected the context's error, got %v", err)
	}
}

func TestStartedCheckFailsUntilSet(t *testing.T) {
	var s startedCheck
	req, _ := http.NewRequest(http.MethodGet, "/readyz", nil)
	if err := s.check(req); err == nil {
		t.Fatal("ready before the webhooks were set up")
	}
	notStarted := errors.New("not started")
	s.set(func(*http.Request) error { return notStarted })
	if err := s.check(req); !errors.Is(err, notStarted) {
		t.Fatalf("expected the server's own answer, got %v", err)
	}
	s.set(func(*http.Request) error { return nil })
	if err := s.check(req); err != nil {
		t.Fatalf("expected ready, got %v", err)
	}
}
