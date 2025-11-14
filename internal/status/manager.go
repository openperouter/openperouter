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

	// Cache of failed resources for status aggregation
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
		failedResourceCacheMutex: sync.RWMutex{},
		failedResourceCache:      make(map[string]*failedResourceCacheEntry),
	}

	// Send initial trigger event to create RouterNodeConfigurationStatus resource
	sm.sendTriggerEvent()

	return sm
}

// ReportResourceSuccess implements StatusReporter interface
func (er *StatusManager) ReportResourceSuccess(kind ResourceKind, resourceName string) {
	// Remove any previous failure from cache
	er.failedResourceCacheMutex.Lock()
	key := string(kind) + ":" + resourceName
	delete(er.failedResourceCache, key)
	er.failedResourceCacheMutex.Unlock()

	// Trigger reconciliation
	er.sendTriggerEvent()

	er.logger.Debug("reported success",
		"kind", kind,
		"resource", resourceName)
}

// ReportResourceFailure implements StatusReporter interface
func (er *StatusManager) ReportResourceFailure(kind ResourceKind, resourceName string, err error) {
	errorMessage := fmt.Sprintf("failed: %v", err)

	// Store failure in cache
	er.failedResourceCacheMutex.Lock()
	key := string(kind) + ":" + resourceName
	er.failedResourceCache[key] = &failedResourceCacheEntry{
		ResourceKind: kind,
		ResourceName: resourceName,
		ErrorMessage: errorMessage,
		Timestamp:    time.Now(),
	}
	er.failedResourceCacheMutex.Unlock()

	// Trigger reconciliation
	er.sendTriggerEvent()

	er.logger.Debug("reported failure",
		"kind", kind,
		"resource", resourceName,
		"error", err)
}

// ReportResourceRemoved implements StatusReporter interface
func (er *StatusManager) ReportResourceRemoved(kind ResourceKind, resourceName string) {
	// Remove any failure entry from cache
	er.failedResourceCacheMutex.Lock()
	key := string(kind) + ":" + resourceName
	_, existed := er.failedResourceCache[key]
	delete(er.failedResourceCache, key)
	er.failedResourceCacheMutex.Unlock()

	// Trigger reconciliation only if the resource was actually in the cache
	if existed {
		er.sendTriggerEvent()
		er.logger.Debug("reported resource removal",
			"kind", kind,
			"resource", resourceName)
	}
}

// sendTriggerEvent sends a minimal trigger event for reconciliation
func (er *StatusManager) sendTriggerEvent() {
	event := event.GenericEvent{
		Object: &triggerEvent{
			TypeMeta: metav1.TypeMeta{
				Kind:       "StatusTrigger",
				APIVersion: "internal.status.openperouter.io/v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      er.nodeName,
				Namespace: er.namespace,
			},
		},
	}

	select {
	case er.triggerChannel <- event:
	default:
		er.logger.Warn("status update channel full, dropping event", "node", er.nodeName)
	}
}

// GetStatusSummary returns aggregated status information for controllers
func (er *StatusManager) GetStatusSummary() StatusSummary {
	er.failedResourceCacheMutex.RLock()
	defer er.failedResourceCacheMutex.RUnlock()

	failedResources := make([]FailedResourceInfo, 0, len(er.failedResourceCache))
	var latestUpdate time.Time

	// Convert the cache to the expected status format and find the latest update timestamp
	for _, failedEntry := range er.failedResourceCache {
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

// GetChannel returns the update channel for controller-runtime integration
func (er *StatusManager) GetChannel() chan event.GenericEvent {
	return er.triggerChannel
}

// Compile-time interface checks
var _ StatusReporter = (*StatusManager)(nil)
var _ StatusReader = (*StatusManager)(nil)
