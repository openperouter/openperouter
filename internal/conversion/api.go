// SPDX-License-Identifier:Apache-2.0

package conversion

import (
	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/internal/hostnetwork"
)

type ApiConfigData struct {
	NodeIndex          int
	UnderlayFromMultus bool
	Underlays          []v1alpha1.Underlay
	L3VNIs             []v1alpha1.L3VNI
	L2VNIs             []v1alpha1.L2VNI
	L3Passthrough      []v1alpha1.L3Passthrough
	LogLevel           string
}

type HostConfigData struct {
	Underlay      hostnetwork.UnderlayParams
	L3VNIs        []hostnetwork.L3VNIParams
	L2VNIs        []hostnetwork.L2VNIParams
	L3Passthrough *hostnetwork.PassthroughParams
}
