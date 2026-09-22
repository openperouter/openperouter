// SPDX-License-Identifier:Apache-2.0

package groutdra

import (
	"context"
	"os"
	"time"
)

const socketPollInterval = 200 * time.Millisecond

// waitForUnixSocket returns when path exists as a Unix socket, or ctx is done.
// QEMU (libvirt mode=server) binds this file; grout client must not attach first.
func waitForUnixSocket(ctx context.Context, path string) error {
	ticker := time.NewTicker(socketPollInterval)
	defer ticker.Stop()
	for {
		if isUnixSocket(path) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func isUnixSocket(path string) bool {
	st, err := os.Stat(path)
	if err != nil {
		return false
	}
	return st.Mode()&os.ModeSocket != 0
}
