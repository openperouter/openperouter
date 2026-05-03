// SPDX-License-Identifier:Apache-2.0

package routerconfiguration

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"k8s.io/apimachinery/pkg/util/sets"

	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/internal/conversion"
	"github.com/openperouter/openperouter/internal/hostnetwork"
	"github.com/openperouter/openperouter/internal/hostnetwork/bridgerefresh"
	"github.com/openperouter/openperouter/internal/sysctl"
)

type interfacesConfiguration struct {
	targetNamespace    string
	underlayFromMultus bool
	nodeIndex          int
	conversion.APIConfigData
}

type UnderlayRemovedError struct{}

func (n UnderlayRemovedError) Error() string {
	return "no underlays configured"
}

func setupHostPrerequisites(ctx context.Context, config interfacesConfiguration) (conversion.HostConfigData, error) {
	hasAlreadyUnderlay, err := hostnetwork.HasUnderlayInterface(config.targetNamespace)
	if err != nil {
		return conversion.HostConfigData{}, fmt.Errorf("failed to check if target namespace %s has underlay: %w", config.targetNamespace, err)
	}
	if hasAlreadyUnderlay && len(config.Underlays) == 0 {
		slog.InfoContext(ctx, "underlay removed, cleaning up VNIs")
		if err := hostnetwork.RemoveAllVNIs(config.targetNamespace); err != nil {
			slog.Warn("failed to remove vnis after underlay removal", "err", err)
		}
		bridgerefresh.StopAllVNIs()
		return conversion.HostConfigData{}, UnderlayRemovedError{}
	}

	if len(config.Underlays) == 0 {
		return conversion.HostConfigData{}, nil
	}

	apiConfig := conversion.APIConfigData{
		Underlays:     config.Underlays,
		L3VNIs:        config.L3VNIs,
		L2VNIs:        config.L2VNIs,
		L3Passthrough: config.L3Passthrough,
	}
	hostConfig, err := conversion.APItoHostConfig(config.nodeIndex, config.targetNamespace, config.underlayFromMultus, apiConfig)
	if err != nil {
		return conversion.HostConfigData{}, fmt.Errorf("failed to convert config to host configuration: %w", err)
	}

	slog.InfoContext(ctx, "ensuring sysctls")
	if err := sysctl.Ensure(
		config.targetNamespace,
		sysctl.IPv4Forwarding(),
		sysctl.IPv6Forwarding(),
		sysctl.ArpAcceptAll(),
		sysctl.ArpAcceptDefault(),
		sysctl.AcceptUntrackedNADefault(),
		sysctl.AcceptUntrackedNAAll(),
	); err != nil {
		return conversion.HostConfigData{}, fmt.Errorf("failed to ensure sysctls: %w", err)
	}

	slog.InfoContext(ctx, "setting up underlay")
	if err := hostnetwork.SetupUnderlay(ctx, hostConfig.Underlay); err != nil {
		return conversion.HostConfigData{}, fmt.Errorf("failed to setup underlay: %w", err)
	}

	return hostConfig, nil
}

func provisionOverlays(ctx context.Context, config interfacesConfiguration, hostConfig conversion.HostConfigData) (ReconcileResult, error) {
	var result ReconcileResult

	slog.InfoContext(ctx, "configure interface start", "namespace", config.targetNamespace)
	defer slog.InfoContext(ctx, "configure interface end", "namespace", config.targetNamespace)

	failedVRFs := sets.New[string]()
	l3NameByVRF := map[string]string{}
	for _, l3 := range config.L3VNIs {
		l3NameByVRF[l3.Spec.VRF] = l3.Name
	}

	var configuredL3VNIs []hostnetwork.L3VNIParams
	for _, vni := range hostConfig.L3VNIs {
		slog.InfoContext(ctx, "setting up VNI", "vni", vni.VRF)
		if err := hostnetwork.SetupL3VNI(ctx, vni); err != nil {
			result.AddFailure("L3VNI", l3NameByVRF[vni.VRF], v1alpha1.OverlayAttachmentFailed, err.Error())
			failedVRFs.Insert(vni.VRF)
			continue
		}
		configuredL3VNIs = append(configuredL3VNIs, vni)
	}

	var configuredL2VNIs []hostnetwork.L2VNIParams
	for i, vni := range hostConfig.L2VNIs {
		l2Name := config.L2VNIs[i].Name
		if failedVRFs.Has(vni.VRF) {
			result.AddFailure("L2VNI", l2Name, v1alpha1.OverlayAttachmentFailed,
				fmt.Sprintf("VRF %s failed netlink provisioning", vni.VRF))
			continue
		}
		slog.InfoContext(ctx, "setting up L2VNI", "vni", vni.VNI)
		if err := hostnetwork.SetupL2VNI(ctx, vni); err != nil {
			result.AddFailure("L2VNI", l2Name, v1alpha1.OverlayAttachmentFailed, err.Error())
			continue
		}
		if err := bridgerefresh.StartForVNI(ctx, vni); err != nil {
			result.AddFailure("L2VNI", l2Name, v1alpha1.OverlayAttachmentFailed, err.Error())
			continue
		}
		configuredL2VNIs = append(configuredL2VNIs, vni)
	}

	slog.InfoContext(ctx, "setting up passthrough")
	if hostConfig.L3Passthrough != nil {
		if err := hostnetwork.SetupPassthrough(ctx, *hostConfig.L3Passthrough); err != nil {
			result.AddFailure("L3Passthrough", config.L3Passthrough[0].Name, v1alpha1.OverlayAttachmentFailed, err.Error())
		}
	}

	slog.InfoContext(ctx, "removing deleted vnis")
	toCheck := make([]hostnetwork.VNIParams, 0, len(configuredL3VNIs)+len(configuredL2VNIs))
	for _, vni := range configuredL3VNIs {
		toCheck = append(toCheck, vni.VNIParams)
	}
	for _, l2vni := range configuredL2VNIs {
		toCheck = append(toCheck, l2vni.VNIParams)
	}
	if err := hostnetwork.RemoveNonConfiguredVNIs(config.targetNamespace, toCheck); err != nil {
		return result, fmt.Errorf("failed to remove deleted vnis: %w", err)
	}
	bridgerefresh.StopForRemovedVNIs(configuredL2VNIs)

	if len(config.L3Passthrough) == 0 {
		if err := hostnetwork.RemovePassthrough(config.targetNamespace); err != nil {
			return result, fmt.Errorf("failed to remove passthrough: %w", err)
		}
	}
	return result, nil
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
