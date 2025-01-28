// SPDX-License-Identifier:Apache-2.0

package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
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
			handler := http.HandlerFunc(reloadHandler)

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
