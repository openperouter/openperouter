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
	"crypto/tls"
	"errors"
	"net/http"
	"path/filepath"
	"sync/atomic"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/healthz"
)

const (
	servingCertPollInterval = time.Second
	// The names the cert rotator writes into the secret, and the kubelet mounts as files.
	servingCertName = "tls.crt"
	servingKeyName  = "tls.key"
)

// waitForServingCert returns once certDir holds a key pair that loads, or with the context's
// error. The kubelet swaps a secret volume's contents in one rename, so a pair that loads is
// complete.
func waitForServingCert(ctx context.Context, certDir string, interval time.Duration) error {
	certFile := filepath.Join(certDir, servingCertName)
	keyFile := filepath.Join(certDir, servingKeyName)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if _, err := tls.LoadX509KeyPair(certFile, keyFile); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// startedCheck is a readiness check that fails until set gives it the webhook server's own.
type startedCheck struct {
	checker atomic.Pointer[healthz.Checker]
}

func (s *startedCheck) set(c healthz.Checker) {
	s.checker.Store(&c)
}

func (s *startedCheck) check(req *http.Request) error {
	c := s.checker.Load()
	if c == nil {
		return errors.New("webhooks not set up yet")
	}
	return (*c)(req)
}
