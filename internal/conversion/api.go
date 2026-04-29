// SPDX-License-Identifier:Apache-2.0

package conversion

import (
	"errors"

	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/internal/hostnetwork"
)

type APIConfigData struct {
	Underlays     []v1alpha1.Underlay
	L3VNIs        []v1alpha1.L3VNI
	L2VNIs        []v1alpha1.L2VNI
	L3Passthrough []v1alpha1.L3Passthrough
	RawFRRConfigs []v1alpha1.RawFRRConfig
}

type HostConfigData struct {
	Underlay      hostnetwork.UnderlayParams
	L3VNIs        []hostnetwork.L3VNIParams
	L2VNIs        []hostnetwork.L2VNIParams
	L3Passthrough *hostnetwork.PassthroughParams
}

func MergeAPIConfigs(configs ...APIConfigData) (APIConfigData, error) {
	if len(configs) == 0 {
		return APIConfigData{}, nil
	}

	merged := APIConfigData{
		L3VNIs:        []v1alpha1.L3VNI{},
		L2VNIs:        []v1alpha1.L2VNI{},
		L3Passthrough: []v1alpha1.L3Passthrough{},
	}

	for _, config := range configs {
		merged.Underlays = append(merged.Underlays, config.Underlays...)
		merged.L3VNIs = append(merged.L3VNIs, config.L3VNIs...)
		merged.L2VNIs = append(merged.L2VNIs, config.L2VNIs...)
		merged.L3Passthrough = append(merged.L3Passthrough, config.L3Passthrough...)
		merged.RawFRRConfigs = append(merged.RawFRRConfigs, config.RawFRRConfigs...)
	}

	return merged, nil
}

// validateAPIConfigData flags invalid config data.
// The caller must ensure that length of config.Underlays > 0, otherwise this will panic.
func validateAPIConfigData(config APIConfigData) error {
	if len(config.Underlays) > 1 {
		return errors.New("multiple underlays defined")
	}
	if len(config.L3Passthrough) > 1 {
		return errors.New("multiple passthroughs defined, can have only one")
	}

	underlay := config.Underlays[0]
	if len(config.L3VNIs) > 0 && underlay.Spec.EVPN == nil {
		return errors.New("EVPN configuration is required when L3 VNIs are defined")
	}
	if len(config.L2VNIs) > 0 && underlay.Spec.EVPN == nil {
		return errors.New("EVPN configuration is required when L2 VNIs are defined")
	}
	return nil
}
