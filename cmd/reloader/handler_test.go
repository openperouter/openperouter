// SPDX-License-Identifier:Apache-2.0

package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/openperouter/openperouter/internal/frrconfig"
)

func TestHandler(t *testing.T) {
	reloadSucceeds := func(_ string) error {
		return nil
	}

	reloadFails := func(_ string) error {
		return errors.New("failed")
	}

	tests := []struct {
		name       string
		reloadMock func(string) error
		method     string
		httpStatus int
	}{
		{
			"succeeds",
			reloadSucceeds,
			http.MethodPost,
			200,
		},
		{
			"wrong method",
			reloadSucceeds,
			http.MethodGet,
			http.StatusBadRequest,
		},
		{
			"reload fails",
			reloadFails,
			http.MethodPost,
			http.StatusInternalServerError,
		},
	}

	t.Cleanup(func() {
		updateConfig = frrconfig.Update
	})
	for _, tc := range tests {
		updateConfig = tc.reloadMock
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req, _ := http.NewRequest(tc.method, "/", nil)
			handler := http.HandlerFunc(reloadHandler("/etc/frr/frr.conf"))

			handler.ServeHTTP(w, req)
			res := w.Result()
			if err := res.Body.Close(); err != nil {
				t.Fatalf("Body.Close() failed: %s", err)
			}
			if res.StatusCode != tc.httpStatus {
				t.Fatalf("expecting %d, got %d", res.StatusCode, tc.httpStatus)
			}
		})
	}
}

func TestRemoveFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reload.sock")

	if err := removeFile(path); err != nil {
		t.Fatalf("expected a missing socket to be ignored: %v", err)
	}

	err := os.WriteFile(path, []byte{}, 0o600)
	if err != nil {
		t.Fatalf("failed to create temporary file: %v", err)
	}
	if err := removeFile(path); err != nil {
		t.Fatalf("failed to remove unix socket: %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected unix socket to be removed, got: %v", err)
	}
}

func TestHealth(t *testing.T) {
	tests := []struct {
		name               string
		check              func() error
		method             string
		expectedHTTPStatus int
	}{
		{
			name:               "healthy",
			check:              func() error { return nil },
			method:             http.MethodGet,
			expectedHTTPStatus: http.StatusOK,
		},
		{
			name:               "unhealthy",
			check:              func() error { return errors.New("watchfrr is not running") },
			method:             http.MethodGet,
			expectedHTTPStatus: http.StatusInternalServerError,
		},
		{
			name:               "wrong method",
			check:              func() error { return nil },
			method:             http.MethodPost,
			expectedHTTPStatus: http.StatusMethodNotAllowed,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req, err := http.NewRequest(test.method, "/healthz", nil)
			if err != nil {
				t.Fatalf("failed to create request: %v", err)
			}

			http.HandlerFunc(health(test.check)).ServeHTTP(w, req)
			res := w.Result()
			if err := res.Body.Close(); err != nil {
				t.Fatalf("Body.Close() failed: %s", err)
			}
			if res.StatusCode != test.expectedHTTPStatus {
				t.Fatalf("expecting %d, got %d", test.expectedHTTPStatus, res.StatusCode)
			}
		})
	}
}
