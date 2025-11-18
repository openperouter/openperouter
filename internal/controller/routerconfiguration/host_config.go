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
	RouterPodUUID  string `json:"routerPodUUID,omitempty"`
	PodRuntime     pods.Runtime
	StatusReporter status.StatusReporter
	conversion.ApiConfigData
}

type UnderlayRemovedError struct{}

func (n UnderlayRemovedError) Error() string {
	return "no underlays configured"
}

func configureInterfaces(ctx context.Context, config interfacesConfiguration) error {
	targetNS, err := config.PodRuntime.NetworkNamespace(ctx, config.RouterPodUUID)
	if err != nil {
		return fmt.Errorf("failed to retrieve namespace for pod %s: %w", config.RouterPodUUID, err)
	}

	hasAlreadyUnderlay, err := hostnetwork.HasUnderlayInterface(targetNS)
	if err != nil {
		return fmt.Errorf("failed to check if target namespace %s for pod %s has underlay: %w", targetNS, config.RouterPodUUID, err)
	}
	if hasAlreadyUnderlay && len(config.Underlays) == 0 {
		return UnderlayRemovedError{}
	}

	if len(config.Underlays) == 0 {
		return nil // nothing to do
	}

	slog.InfoContext(ctx, "configure interface start", "namespace", targetNS)
	defer slog.InfoContext(ctx, "configure interface end", "namespace", targetNS)
	apiConfig := conversion.ApiConfigData{
		NodeIndex:     config.NodeIndex,
		Underlays:     config.Underlays,
		L3VNIs:        config.L3VNIs,
		L2VNIs:        config.L2VNIs,
		L3Passthrough: config.L3Passthrough,
	}
	hostConfig, err := conversion.APItoHostConfig(config.NodeIndex, targetNS, apiConfig)
	if err != nil {
		return fmt.Errorf("failed to convert config to host configuration: %w", err)
	}

	slog.InfoContext(ctx, "ensuring IPv6 forwarding")
	if err := hostnetwork.EnsureIPv6Forwarding(targetNS); err != nil {
		return fmt.Errorf("failed to ensure IPv6 forwarding: %w", err)
	}

	slog.InfoContext(ctx, "setting up underlay")
	if err := hostnetwork.SetupUnderlay(ctx, hostConfig.Underlay); err != nil {
		// Report failure for the single underlay resource
		if len(config.Underlays) > 0 {
			config.StatusReporter.ReportResourceFailure(status.UnderlayKind, config.Underlays[0].Name, err)
		}
		return fmt.Errorf("failed to setup underlay: %w", err)
	}
	// Report success for the single underlay resource
	if len(config.Underlays) > 0 {
		config.StatusReporter.ReportResourceSuccess(status.UnderlayKind, config.Underlays[0].Name)
	}

	// Setup L3VNIs by iterating over API resources and finding corresponding host config
	for _, l3vni := range config.L3VNIs {
		slog.InfoContext(ctx, "setting up L3VNI", "name", l3vni.Name, "vni", l3vni.Spec.VNI)

		// Find corresponding host config for this L3VNI
		var hostL3VNI *hostnetwork.L3VNIParams
		for _, hvni := range hostConfig.L3VNIs {
			if hvni.VNI == int(l3vni.Spec.VNI) {
				hostL3VNI = &hvni
				break
			}
		}

		if hostL3VNI == nil {
			err := fmt.Errorf("no host config found for L3VNI %s with VNI %d", l3vni.Name, l3vni.Spec.VNI)
			config.StatusReporter.ReportResourceFailure(status.L3VNIKind, l3vni.Name, err)
			return err
		}

		if err := hostnetwork.SetupL3VNI(ctx, *hostL3VNI); err != nil {
			config.StatusReporter.ReportResourceFailure(status.L3VNIKind, l3vni.Name, err)
			return fmt.Errorf("failed to setup L3VNI %s: %w", l3vni.Name, err)
		}
		config.StatusReporter.ReportResourceSuccess(status.L3VNIKind, l3vni.Name)
	}

	// Setup L2VNIs by iterating over API resources and finding corresponding host config
	for _, l2vni := range config.L2VNIs {
		slog.InfoContext(ctx, "setting up L2VNI", "name", l2vni.Name, "vni", l2vni.Spec.VNI)

		// Find corresponding host config for this L2VNI
		var hostL2VNI *hostnetwork.L2VNIParams
		for _, hvni := range hostConfig.L2VNIs {
			if hvni.VNI == int(l2vni.Spec.VNI) {
				hostL2VNI = &hvni
				break
			}
		}

		if hostL2VNI == nil {
			err := fmt.Errorf("no host config found for L2VNI %s with VNI %d", l2vni.Name, l2vni.Spec.VNI)
			config.StatusReporter.ReportResourceFailure(status.L2VNIKind, l2vni.Name, err)
			return err
		}

		if err := hostnetwork.SetupL2VNI(ctx, *hostL2VNI); err != nil {
			config.StatusReporter.ReportResourceFailure(status.L2VNIKind, l2vni.Name, err)
			return fmt.Errorf("failed to setup L2VNI %s: %w", l2vni.Name, err)
		}
		config.StatusReporter.ReportResourceSuccess(status.L2VNIKind, l2vni.Name)
	}

	slog.InfoContext(ctx, "setting up passthrough")
	if hostConfig.L3Passthrough != nil {
		// Report for the single L3Passthrough resource
		if len(config.L3Passthrough) > 0 {
			if err := hostnetwork.SetupPassthrough(ctx, *hostConfig.L3Passthrough); err != nil {
				config.StatusReporter.ReportResourceFailure(status.L3PassthroughKind, config.L3Passthrough[0].Name, err)
				return fmt.Errorf("failed to setup passthrough: %w", err)
			}
			config.StatusReporter.ReportResourceSuccess(status.L3PassthroughKind, config.L3Passthrough[0].Name)
		}
	}

	slog.InfoContext(ctx, "removing deleted vnis")
	toCheck := make([]hostnetwork.VNIParams, 0, len(hostConfig.L3VNIs)+len(hostConfig.L2VNIs))
	for _, vni := range hostConfig.L3VNIs {
		toCheck = append(toCheck, vni.VNIParams)
	}
	for _, l2vni := range hostConfig.L2VNIs {
		toCheck = append(toCheck, l2vni.VNIParams)
	}
	if err := hostnetwork.RemoveNonConfiguredVNIs(targetNS, toCheck); err != nil {
		return fmt.Errorf("failed to remove deleted vnis: %w", err)
	}

	if len(apiConfig.L3Passthrough) == 0 {
		if err := hostnetwork.RemovePassthrough(targetNS); err != nil {
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
