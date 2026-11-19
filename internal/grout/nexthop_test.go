// SPDX-License-Identifier:Apache-2.0

package grout

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEnsureNeighborAgeing(t *testing.T) {
	const configShow = "grcli --err-exit --json --socket sock nexthop config show"
	const configSet = "grcli --err-exit --json --socket sock nexthop config set lifetime 30"

	tests := []struct {
		name            string
		calls           []cmdCall
		wantErrContains string
	}{
		{
			name: "sets the lifetime when it differs",
			calls: []cmdCall{
				{cmd: configShow, output: `{"used":52,"max":4096,"lifetime":1200,"unreachable":60}`},
				{cmd: configSet},
			},
		},
		{
			name: "leaves the lifetime alone when already set",
			calls: []cmdCall{
				{cmd: configShow, output: `{"used":52,"max":4096,"lifetime":30,"unreachable":60}`},
			},
		},
		{
			name:            "fails on unparsable config",
			calls:           []cmdCall{{cmd: configShow, output: "not json"}},
			wantErrContains: `parsing nexthop config "not json"`,
		},
		{
			name: "fails when setting the lifetime fails",
			calls: []cmdCall{
				{cmd: configShow, output: `{"used":52,"max":4096,"lifetime":1200,"unreachable":60}`},
				{cmd: configSet, err: errors.New("grcli exploded")},
			},
			wantErrContains: "setting nexthop lifetime to 30",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			defer mockCmdExec(tc.calls...)()

			err := NewClient("sock").ensureNeighborAgeing(context.Background())
			if tc.wantErrContains == "" {
				assert.NoError(t, err)
				return
			}
			assert.ErrorContains(t, err, tc.wantErrContains)
		})
	}
}
