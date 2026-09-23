// SPDX-License-Identifier:Apache-2.0

package conversion

import (
	"errors"
	"maps"
	"slices"

	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/internal/frr"
	"github.com/openperouter/openperouter/internal/hostnetwork"
)

type APIConfigData struct {
	Underlays     []v1alpha1.Underlay
	L3VNIs        []v1alpha1.L3VNI
	L2VNIs        []v1alpha1.L2VNI
	L3VPNs        []v1alpha1.L3VPN
	L3Passthrough []v1alpha1.L3Passthrough
	RawFRRConfigs []v1alpha1.RawFRRConfig
	Passwords     map[string]string
}

type HostConfigData struct {
	Underlay      hostnetwork.UnderlayParams
	L3VNIs        []hostnetwork.L3VNIParams
	L2VNIs        []hostnetwork.L2VNIParams
	L3VPNs        []hostnetwork.L3VPNParams
	L3Passthrough *hostnetwork.PassthroughParams
}

func MergeAPIConfigs(configs ...APIConfigData) (APIConfigData, error) {
	if len(configs) == 0 {
		return APIConfigData{}, nil
	}

	merged := APIConfigData{
		L3VNIs:        []v1alpha1.L3VNI{},
		L2VNIs:        []v1alpha1.L2VNI{},
		L3VPNs:        []v1alpha1.L3VPN{},
		L3Passthrough: []v1alpha1.L3Passthrough{},
		Passwords:     map[string]string{},
	}

	for _, config := range configs {
		merged.Underlays = append(merged.Underlays, config.Underlays...)
		merged.L3VNIs = append(merged.L3VNIs, config.L3VNIs...)
		merged.L2VNIs = append(merged.L2VNIs, config.L2VNIs...)
		merged.L3VPNs = append(merged.L3VPNs, config.L3VPNs...)
		merged.L3Passthrough = append(merged.L3Passthrough, config.L3Passthrough...)
		merged.RawFRRConfigs = append(merged.RawFRRConfigs, config.RawFRRConfigs...)
		maps.Copy(merged.Passwords, config.Passwords)
	}

	return merged, nil
}

const passwordRedactionMarker = "<REDACTED>"

// RedactAPIConfigData returns a copy of data safe for logging: resolved BGP
// passwords and any passwords embedded in raw FRR snippets are replaced with a
// redaction marker. Underlays and VNIs carry only secret references, never
// plaintext credentials, so they are shared unchanged.
func RedactAPIConfigData(data APIConfigData) APIConfigData {
	if data.Passwords != nil {
		passwords := make(map[string]string, len(data.Passwords))
		for id := range data.Passwords {
			passwords[id] = passwordRedactionMarker
		}
		data.Passwords = passwords
	}
	data.RawFRRConfigs = RedactRawFRRConfigs(data.RawFRRConfigs)
	return data
}

// RedactRawFRRConfigs returns a copy of configs with the passwords in each raw
// FRR snippet replaced, safe for logging.
func RedactRawFRRConfigs(configs []v1alpha1.RawFRRConfig) []v1alpha1.RawFRRConfig {
	if configs == nil {
		return nil
	}

	redacted := slices.Clone(configs)
	for i := range redacted {
		redacted[i].Spec.RawConfig = frr.RedactPasswords(redacted[i].Spec.RawConfig)
	}
	return redacted
}

// validateAPIConfigData flags invalid config data.
func validateAPIConfigData(config APIConfigData) error {
	if len(config.L3Passthrough) > 1 {
		return errors.New("multiple passthroughs defined, can only have one")
	}

	if len(config.Underlays) > 1 {
		return errors.New("multiple underlays defined")
	}

	if len(config.Underlays) == 0 {
		return NoUnderlaysError("no underlays provided")
	}

	return nil
}
