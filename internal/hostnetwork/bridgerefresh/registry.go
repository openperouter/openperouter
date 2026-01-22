// SPDX-License-Identifier:Apache-2.0

package bridgerefresh

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/openperouter/openperouter/internal/hostnetwork"
)

var (
	activeRefreshers = make(map[int]*BridgeRefresher) // VNI -> Refresher
	mu               sync.Mutex
)

// StartForVNI starts a bridge refresher for the given L2VNI.
// If a refresher already exists for this VNI, it is stopped and replaced.
func StartForVNI(ctx context.Context, params hostnetwork.L2VNIParams) error {
	mu.Lock()
	defer mu.Unlock()

	// Stop existing refresher if present
	if existing, ok := activeRefreshers[params.VNI]; ok {
		existing.Stop()
		delete(activeRefreshers, params.VNI)
	}

	refresher, err := New(params)
	if err != nil {
		return fmt.Errorf("failed to create refresher for VNI %d: %w", params.VNI, err)
	}

	refresher.Start(ctx)
	activeRefreshers[params.VNI] = refresher
	return nil
}

// StopForVNI stops and removes the bridge refresher for the given VNI.
func StopForVNI(vni int) error {
	mu.Lock()
	defer mu.Unlock()

	refresher, ok := activeRefreshers[vni]
	if !ok {
		return nil // Nothing to stop
	}

	refresher.Stop()
	delete(activeRefreshers, vni)
	return nil
}

// StopAll stops all active bridge refreshers.
// This should be called during controller shutdown.
func StopAll() {
	mu.Lock()
	defer mu.Unlock()

	for vni, refresher := range activeRefreshers {
		refresher.Stop()
		delete(activeRefreshers, vni)
	}

	slog.Info("stopped all bridge refreshers")
}

// StopForRemovedVNIs stops refreshers for VNIs that are no longer configured.
func StopForRemovedVNIs(configuredVNIs []hostnetwork.L2VNIParams) {
	mu.Lock()
	defer mu.Unlock()

	configured := make(map[int]bool)
	for _, params := range configuredVNIs {
		configured[params.VNI] = true
	}

	for vni, refresher := range activeRefreshers {
		if !configured[vni] {
			refresher.Stop()
			delete(activeRefreshers, vni)
			slog.Info("stopped bridge refresher for removed VNI", "vni", vni)
		}
	}
}

// ActiveCount returns the number of active refreshers.
func ActiveCount() int {
	mu.Lock()
	defer mu.Unlock()
	return len(activeRefreshers)
}
