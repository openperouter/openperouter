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

package status

import (
	"fmt"
	"log/slog"
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

// triggerEvent is a minimal event used only to trigger reconciliation
type triggerEvent struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
}

// DeepCopyObject returns a deep copy of the object for controller-runtime
func (t *triggerEvent) DeepCopyObject() runtime.Object {
	return &triggerEvent{
		TypeMeta:   t.TypeMeta,
		ObjectMeta: *t.ObjectMeta.DeepCopy(), //nolint:staticcheck
	}
}

type failedResourceCacheEntry struct {
	// Resource information
	ResourceKind ResourceKind
	ResourceName string

	// Error message for the failure
	ErrorMessage string

	// Timestamp when the failure occurred
	Timestamp time.Time
}

// StatusManager sends resource status events and stores state for status aggregation
type StatusManager struct {
	logger *slog.Logger

	// Channel used to trigger controller-runtime reconciliation
	triggerChannel chan event.GenericEvent
	nodeName       string
	namespace      string

	// nowFunc returns the current time; can be overridden for testing
	nowFunc func() time.Time

	// Cache of failed resources for status aggregation.
	// Protected by mutex since PERouterReconciler may write status updates
	// while RouterNodeConfigurationStatusReconciler reads the summary.
	failedResourceCacheMutex sync.RWMutex
	failedResourceCache      map[string]*failedResourceCacheEntry // key: "kind:name"
}

// NewStatusManager creates a new StatusManager that sends rich status events
func NewStatusManager(updateChannel chan event.GenericEvent, nodeName, namespace string, logger *slog.Logger) *StatusManager {
	sm := &StatusManager{
		triggerChannel:           updateChannel,
		nodeName:                 nodeName,
		namespace:                namespace,
		logger:                   logger,
		nowFunc:                  time.Now,
		failedResourceCacheMutex: sync.RWMutex{},
		failedResourceCache:      make(map[string]*failedResourceCacheEntry),
	}

	// Send initial trigger event to create RouterNodeConfigurationStatus resource
	sm.sendTriggerEvent()

	return sm
}

// ReportResourceSuccess implements StatusReporter interface
func (sm *StatusManager) ReportResourceSuccess(kind ResourceKind, resourceName string) {
	sm.failedResourceCacheMutex.Lock()
	key := fmt.Sprintf("%s:%s", kind, resourceName)
	delete(sm.failedResourceCache, key)
	sm.failedResourceCacheMutex.Unlock()

	sm.sendTriggerEvent()

	sm.logger.Debug("reported success",
		"kind", kind,
		"resource", resourceName)
}

// ReportResourceFailure implements StatusReporter interface
func (sm *StatusManager) ReportResourceFailure(kind ResourceKind, resourceName string, err error) {
	errorMessage := fmt.Sprintf("failed: %v", err)

	sm.failedResourceCacheMutex.Lock()
	key := fmt.Sprintf("%s:%s", kind, resourceName)
	sm.failedResourceCache[key] = &failedResourceCacheEntry{
		ResourceKind: kind,
		ResourceName: resourceName,
		ErrorMessage: errorMessage,
		Timestamp:    sm.nowFunc(),
	}
	sm.failedResourceCacheMutex.Unlock()

	sm.sendTriggerEvent()

	sm.logger.Debug("reported failure",
		"kind", kind,
		"resource", resourceName,
		"error", err)
}

// ReportResourceRemoved implements StatusReporter interface
func (sm *StatusManager) ReportResourceRemoved(kind ResourceKind, resourceName string) {
	sm.failedResourceCacheMutex.Lock()
	key := fmt.Sprintf("%s:%s", kind, resourceName)
	_, existed := sm.failedResourceCache[key]
	delete(sm.failedResourceCache, key)
	sm.failedResourceCacheMutex.Unlock()

	if existed {
		sm.sendTriggerEvent()
		sm.logger.Debug("reported resource removal",
			"kind", kind,
			"resource", resourceName)
	}
}

// sendTriggerEvent sends a minimal trigger event for reconciliation
func (sm *StatusManager) sendTriggerEvent() {
	event := event.GenericEvent{
		Object: &triggerEvent{
			TypeMeta: metav1.TypeMeta{
				Kind: "StatusTrigger",
				// Internal-only API version used as a marker for trigger events.
				// This is not a real API - it's only used to satisfy the TypeMeta requirement.
				APIVersion: "internal.status.openperouter.io/v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      sm.nodeName,
				Namespace: sm.namespace,
			},
		},
	}

	select {
	case sm.triggerChannel <- event:
	default:
		sm.logger.Warn("status update channel full, dropping event", "node", sm.nodeName)
	}
}

// GetStatusSummary returns aggregated status information for controllers
func (sm *StatusManager) GetStatusSummary() StatusSummary {
	sm.failedResourceCacheMutex.RLock()
	defer sm.failedResourceCacheMutex.RUnlock()

	failedResources := make([]FailedResourceInfo, 0, len(sm.failedResourceCache))
	var latestUpdate time.Time

	for _, failedEntry := range sm.failedResourceCache {
		if failedEntry.Timestamp.After(latestUpdate) {
			latestUpdate = failedEntry.Timestamp
		}

		failedResources = append(failedResources, FailedResourceInfo{
			Kind:         failedEntry.ResourceKind,
			Name:         failedEntry.ResourceName,
			ErrorMessage: failedEntry.ErrorMessage,
		})
	}

	return StatusSummary{
		FailedResources: failedResources,
		LastUpdateTime:  latestUpdate,
	}
}

// GetConnection returns the update channel for controller-runtime integration
func (sm *StatusManager) GetConnection() chan event.GenericEvent {
	return sm.triggerChannel
}

// Compile-time interface checks
var _ StatusReporter = (*StatusManager)(nil)
var _ StatusReader = (*StatusManager)(nil)
