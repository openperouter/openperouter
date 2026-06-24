// SPDX-License-Identifier:Apache-2.0

package conversion

import (
	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/internal/hostnetwork"
)

type APIConfigData struct {
	Underlays     []v1alpha1.Underlay
	L3VNIs        []v1alpha1.L3VNI
	L2VNIs        []v1alpha1.L2VNI
	L3Passthrough []v1alpha1.L3Passthrough
	RawFRRConfigs []v1alpha1.RawFRRConfig
	// UnderlayNAD carries the resolved NetworkAttachmentDefinition config for an
	// underlay that references one. It is populated by the reconciler (which has
	// the API client) since conversion is pure.
	UnderlayNAD *UnderlayNAD
	// CNIBinDirs and CNICacheDir are the node-level CNI settings used to invoke
	// (and, on removal, tear down) a NAD-backed underlay interface. They are
	// always populated by the reconciler so the teardown path has them even when
	// the underlay (and thus UnderlayNAD) is gone.
	CNIBinDirs  []string
	CNICacheDir string
}

// UnderlayNAD holds the resolved NetworkAttachmentDefinition CNI config for an
// underlay that references one.
type UnderlayNAD struct {
	// Config is the NetworkAttachmentDefinition's .spec.config (CNI JSON).
	Config string
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
		if config.UnderlayNAD != nil {
			merged.UnderlayNAD = config.UnderlayNAD
		}
		if len(config.CNIBinDirs) > 0 {
			merged.CNIBinDirs = config.CNIBinDirs
		}
		if config.CNICacheDir != "" {
			merged.CNICacheDir = config.CNICacheDir
		}
	}

	return merged, nil
}
