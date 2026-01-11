// SPDX-License-Identifier:Apache-2.0

package liveness

import (
	"fmt"
	"strings"
	"testing"
)

func TestLiveness(t *testing.T) {
	tests := []struct {
		desc          string
		vtyshRes      string
		vtyshError    error
		expectedError error
	}{
		{
			desc:     "regular",
			vtyshRes: " zebra bgpd watchfrr staticd bfdd\n",
		},
		{
			desc:          "returns error",
			vtyshError:    fmt.Errorf("failed to run"),
			expectedError: fmt.Errorf("fail to list FRR daemons"),
		},
		{
			desc:          "less daemons",
			vtyshRes:      " zebra bgpd bfdd\n",
			expectedError: fmt.Errorf("some FRR daemons are not running"),
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			vtysh := func(_ string) (string, error) {
				return test.vtyshRes, test.vtyshError
			}

			err := PingFrr(vtysh)
			if test.expectedError != nil {
				if err == nil {
					t.Fatalf("expected error %q but got none", test.expectedError)
				}
				if !strings.Contains(err.Error(), test.expectedError.Error()) {
					t.Fatalf("expected error to contain %q, got %q", test.expectedError.Error(), err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
