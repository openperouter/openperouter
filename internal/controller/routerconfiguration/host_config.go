// SPDX-License-Identifier:Apache-2.0

package routerconfiguration

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/openperouter/openperouter/internal/conversion"
	"github.com/openperouter/openperouter/internal/hostnetwork"
	"github.com/openperouter/openperouter/internal/pods"
	"github.com/openperouter/openperouter/internal/status"
)

type interfacesConfiguration struct {
	RouterPodUUID   string `json:"routerPodUUID,omitempty"`
	PodRuntime      pods.Runtime
	StatusReporter  status.StatusReporter
	targetNamespace string
	conversion.ApiConfigData
}

type UnderlayRemovedError struct{}

func (n UnderlayRemovedError) Error() string {
	return "no underlays configured"
}

func configureInterfaces(ctx context.Context, config interfacesConfiguration) error {
	hasAlreadyUnderlay, err := hostnetwork.HasUnderlayInterface(config.targetNamespace)
	if err != nil {
		return fmt.Errorf("failed to check if target namespace %s has underlay: %w", config.targetNamespace, err)
	}
	if hasAlreadyUnderlay && len(config.Underlays) == 0 {
		return UnderlayRemovedError{}
	}

	if len(config.Underlays) == 0 {
		return nil // nothing to do
	}

	slog.InfoContext(ctx, "configure interface start", "namespace", config.targetNamespace)
	defer slog.InfoContext(ctx, "configure interface end", "namespace", config.targetNamespace)
	apiConfig := conversion.ApiConfigData{
		UnderlayFromMultus: config.UnderlayFromMultus,
		NodeIndex:          config.NodeIndex,
		Underlays:          config.Underlays,
		L3VNIs:             config.L3VNIs,
		L2VNIs:             config.L2VNIs,
		L3Passthrough:      config.L3Passthrough,
	}
	hostConfig, err := conversion.APItoHostConfig(config.NodeIndex, config.targetNamespace, apiConfig)
	if err != nil {
		return fmt.Errorf("failed to convert config to host configuration: %w", err)
	}

	slog.InfoContext(ctx, "ensuring IPv6 forwarding")
	if err := hostnetwork.EnsureIPv6Forwarding(config.targetNamespace); err != nil {
		return fmt.Errorf("failed to ensure IPv6 forwarding: %w", err)
	}

	// Despite the config has a list of Underlays, there is always either one or none
	if len(config.Underlays) > 0 {
		slog.InfoContext(ctx, "setting up Underlay", "name", config.Underlays[0].Name)

		if err := hostnetwork.SetupUnderlay(ctx, hostConfig.Underlay); err != nil {
			config.StatusReporter.ReportResourceFailure(status.UnderlayKind, config.Underlays[0].Name, err)
			return fmt.Errorf("failed to setup underlay: %w", err)
		}
		config.StatusReporter.ReportResourceSuccess(status.UnderlayKind, config.Underlays[0].Name)
	}

	for _, l3vni := range config.L3VNIs {
		slog.InfoContext(ctx, "setting up L3VNI", "name", l3vni.Name, "vni", l3vni.Spec.VNI)

		hostL3VNI := findHostL3VNI(hostConfig.L3VNIs, int(l3vni.Spec.VNI))
		if hostL3VNI == nil {
			return fmt.Errorf("unexpected error, no host config found for L3VNI %s with VNI %d", l3vni.Name, l3vni.Spec.VNI)
		}

		if err := hostnetwork.SetupL3VNI(ctx, *hostL3VNI); err != nil {
			config.StatusReporter.ReportResourceFailure(status.L3VNIKind, l3vni.Name, err)
			return fmt.Errorf("failed to setup L3VNI %s: %w", l3vni.Name, err)
		}
		config.StatusReporter.ReportResourceSuccess(status.L3VNIKind, l3vni.Name)
	}

	for _, l2vni := range config.L2VNIs {
		slog.InfoContext(ctx, "setting up L2VNI", "name", l2vni.Name, "vni", l2vni.Spec.VNI)

		hostL2VNI := findHostL2VNI(hostConfig.L2VNIs, int(l2vni.Spec.VNI))
		if hostL2VNI == nil {
			return fmt.Errorf("unexpected error, no host config found for L2VNI %s with VNI %d", l2vni.Name, l2vni.Spec.VNI)
		}

		if err := hostnetwork.SetupL2VNI(ctx, *hostL2VNI); err != nil {
			config.StatusReporter.ReportResourceFailure(status.L2VNIKind, l2vni.Name, err)
			return fmt.Errorf("failed to setup L2VNI %s: %w", l2vni.Name, err)
		}
		config.StatusReporter.ReportResourceSuccess(status.L2VNIKind, l2vni.Name)
	}

	// Despite the config has a list of L3Passthroughts, there is always either one or none
	if len(config.L3Passthrough) > 0 {
		slog.InfoContext(ctx, "setting up L3Passthrough", "name", config.L3Passthrough[0].Name)

		if hostConfig.L3Passthrough == nil {
			return fmt.Errorf("unexpected error, L3Passthrough not found in host config")
		}

		if err := hostnetwork.SetupPassthrough(ctx, *hostConfig.L3Passthrough); err != nil {
			config.StatusReporter.ReportResourceFailure(status.L3PassthroughKind, config.L3Passthrough[0].Name, err)
			return fmt.Errorf("failed to setup L3Passthrough %s: %w", config.L3Passthrough[0].Name, err)
		}
		config.StatusReporter.ReportResourceSuccess(status.L3PassthroughKind, config.L3Passthrough[0].Name)
	}

	slog.InfoContext(ctx, "removing deleted vnis")
	toCheck := make([]hostnetwork.VNIParams, 0, len(hostConfig.L3VNIs)+len(hostConfig.L2VNIs))
	for _, vni := range hostConfig.L3VNIs {
		toCheck = append(toCheck, vni.VNIParams)
	}
	for _, l2vni := range hostConfig.L2VNIs {
		toCheck = append(toCheck, l2vni.VNIParams)
	}
	if err := hostnetwork.RemoveNonConfiguredVNIs(config.targetNamespace, toCheck); err != nil {
		return fmt.Errorf("failed to remove deleted vnis: %w", err)
	}

	if len(apiConfig.L3Passthrough) == 0 {
		if err := hostnetwork.RemovePassthrough(config.targetNamespace); err != nil {
			return fmt.Errorf("failed to remove passthrough: %w", err)
		}
	}
	return nil
}

// nonRecoverableHostError tells whether the router pod
// should be restarted instead of being reconfigured.
func nonRecoverableHostError(e error) bool {
	if errors.As(e, &UnderlayRemovedError{}) {
		return true
	}
	underlayExistsError := hostnetwork.UnderlayExistsError("")
	return errors.As(e, &underlayExistsError)
}

// findHostL3VNI finds the corresponding host L3VNI configuration by VNI ID
func findHostL3VNI(hostL3VNIs []hostnetwork.L3VNIParams, vni int) *hostnetwork.L3VNIParams {
	for _, hvni := range hostL3VNIs {
		if hvni.VNI == vni {
			return &hvni
		}
	}
	return nil
}

// findHostL2VNI finds the corresponding host L2VNI configuration by VNI ID
func findHostL2VNI(hostL2VNIs []hostnetwork.L2VNIParams, vni int) *hostnetwork.L2VNIParams {
	for _, hvni := range hostL2VNIs {
		if hvni.VNI == vni {
			return &hvni
		}
	}
	return nil
}
