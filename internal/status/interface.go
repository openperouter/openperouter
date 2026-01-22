/*
Copyright 2025.

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
	"time"

	"sigs.k8s.io/controller-runtime/pkg/event"
)

// ResourceKind represents the kind of OpenPERouter resource
type ResourceKind string

const (
	UnderlayKind      ResourceKind = "Underlay"
	L2VNIKind         ResourceKind = "L2VNI"
	L3VNIKind         ResourceKind = "L3VNI"
	L3PassthroughKind ResourceKind = "L3Passthrough"
)

// FailedResourceInfo contains information about a failed resource
type FailedResourceInfo struct {
	Kind         ResourceKind `json:"kind"`
	Name         string       `json:"name"`
	ErrorMessage string       `json:"errorMessage"`
}

// StatusSummary provides aggregated status information for controllers
type StatusSummary struct {
	FailedResources []FailedResourceInfo `json:"failedResources"`
	LastUpdateTime  time.Time            `json:"lastUpdateTime"`
}

// StatusReporter allows controllers to report their status via events
type StatusReporter interface {
	// ReportResourceSuccess reports successful resource configuration
	ReportResourceSuccess(kind ResourceKind, resourceName string)

	// ReportResourceFailure reports failed resource configuration with error details
	ReportResourceFailure(kind ResourceKind, resourceName string, err error)

	// ReportResourceRemoved reports that a resource has been removed and should be cleaned from status
	ReportResourceRemoved(kind ResourceKind, resourceName string)
}

// StatusReader allows controllers to read aggregated status information
type StatusReader interface {
	// GetStatusSummary returns aggregated status information
	GetStatusSummary() StatusSummary

	// GetConnection returns the update channel for controller-runtime integration
	GetConnection() chan event.GenericEvent
}
