// SPDX-License-Identifier:Apache-2.0

package webhooks

import (
	"strings"
	"testing"

	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/internal/logging"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestValidateRawFRRConfig tests the validation logic of the RawFRRConfig webhook. The goal
// is not to test each called function (functions themselves should have unit tests for that),
// but to make sure that the webhook's logic overall is sound.
func TestValidateRawFRRConfig(t *testing.T) {
	tcs := []struct {
		name         string
		rawFRRConfig *v1alpha1.RawFRRConfig
		errorString  string
	}{
		{
			name: "webhook passes",
			rawFRRConfig: &v1alpha1.RawFRRConfig{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "rawfrrconfig",
				},
				Spec: v1alpha1.RawFRRConfigSpec{
					RawConfig: "some config",
				},
			},
		},
		{
			name: "empty rawConfig",
			rawFRRConfig: &v1alpha1.RawFRRConfig{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "rawfrrconfig",
				},
				Spec: v1alpha1.RawFRRConfigSpec{
					RawConfig: "",
				},
			},
			errorString: "rawConfig must not be empty",
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			origLogger := Logger
			defer func() {
				Logger = origLogger
			}()
			Logger, _ = logging.New("debug")

			err := validateRawFRRConfig(tc.rawFRRConfig)
			if tc.errorString == "" {
				if err != nil {
					t.Fatalf("expected no error, but got %q", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error to contain %q but got no error", tc.errorString)
			}
			if !strings.Contains(err.Error(), tc.errorString) {
				t.Fatalf("expected error message %q to contain substring %q", err.Error(), tc.errorString)
			}
		})
	}
}
