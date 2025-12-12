// SPDX-License-Identifier:Apache-2.0

package conversion

import "github.com/openperouter/openperouter/internal/status"

// NoOpStatusReporter is a no-op implementation of StatusReporter for use in webhooks
type NoOpStatusReporter struct{}

func (n *NoOpStatusReporter) ReportResourceSuccess(kind status.ResourceKind, resourceName string) {}
func (n *NoOpStatusReporter) ReportResourceFailure(kind status.ResourceKind, resourceName string, err error) {
}
func (n *NoOpStatusReporter) ReportResourceRemoved(kind status.ResourceKind, resourceName string) {}
