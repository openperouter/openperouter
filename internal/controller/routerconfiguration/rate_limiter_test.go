// SPDX-License-Identifier:Apache-2.0

package routerconfiguration

import (
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

func TestRateLimiterCapsTheBackoff(t *testing.T) {
	tests := []struct {
		name       string
		maxBackoff time.Duration
		want       time.Duration
	}{
		{name: "unset"},
		{name: "thirty second cap", maxBackoff: 30 * time.Second, want: 30 * time.Second},
		{name: "shorter cap", maxBackoff: time.Second, want: time.Second},
		{name: "longer cap", maxBackoff: time.Minute, want: time.Minute},
		{name: "below initial delay", maxBackoff: time.Millisecond, want: time.Millisecond},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			limiter := rateLimiter(tt.maxBackoff)
			if tt.maxBackoff == 0 {
				if limiter != nil {
					t.Fatal("unset backoff must leave the controller rate limiter unchanged")
				}
				return
			}
			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "node-1"}}
			delay := min(5*time.Millisecond, tt.want)
			for range 40 {
				if got := limiter.When(req); got != delay {
					t.Fatalf("backoff = %v, want %v", got, delay)
				}
				delay = min(delay*2, tt.want)
			}
			limiter.Forget(req)
			if got := limiter.When(req); got != min(5*time.Millisecond, tt.want) {
				t.Fatalf("backoff after Forget = %v, want %v", got, min(5*time.Millisecond, tt.want))
			}
		})
	}
}
