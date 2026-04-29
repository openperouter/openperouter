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
	openpeerrors "github.com/openperouter/openperouter/internal/errors"
	"github.com/openperouter/openperouter/internal/hostnetwork"
	"github.com/openperouter/openperouter/internal/hostnetwork/bridgerefresh"
	"github.com/openperouter/openperouter/internal/sysctl"
)

type interfacesConfiguration struct {
	targetNamespace string
	nodeIndex       int
	conversion.APIConfigData
}

// HostConfigurator applies host-level network configuration (interfaces, VNIs, sysctls).
// Injected into Reconcile so tests can substitute a no-op implementation.
type HostConfigurator func(ctx context.Context, config interfacesConfiguration) error

func configureInterfaces(ctx context.Context, config interfacesConfiguration) error {
	hasAlreadyUnderlay, err := hostnetwork.HasUnderlayInterface(config.targetNamespace)
	if err != nil {
		return fmt.Errorf("failed to check if target namespace %s has underlay: %w", config.targetNamespace, err)
	}
	if hasAlreadyUnderlay && len(config.Underlays) == 0 {
		slog.InfoContext(ctx, "underlay removed, cleaning up VNIs and underlay interfaces")
		if err := hostnetwork.RemoveAllVNIs(config.targetNamespace); err != nil {
			slog.Warn("failed to remove vnis after underlay removal", "err", err)
		}
		bridgerefresh.StopAllVNIs()
		if err := hostnetwork.RestoreUnderlay(ctx, config.targetNamespace); err != nil {
			slog.Warn("failed to remove underlay interfaces after underlay removal", "err", err)
		}
		return nil
	}

	if len(config.Underlays) == 0 {
		return nil // nothing to do
	}

	slog.InfoContext(ctx, "configure interface start", "namespace", config.targetNamespace)
	defer slog.InfoContext(ctx, "configure interface end", "namespace", config.targetNamespace)
	apiConfig := conversion.APIConfigData{
		Underlays:     config.Underlays,
		L3VNIs:        config.L3VNIs,
		L2VNIs:        config.L2VNIs,
		L3VPNs:        config.L3VPNs,
		L3Passthrough: config.L3Passthrough,
	}
	hostConfig, err := conversion.APItoHostConfig(config.nodeIndex, config.targetNamespace, apiConfig)
	if err != nil {
		return fmt.Errorf("failed to convert config to host configuration: %w", err)
	}

	if err := ensureSysctlsForConfig(ctx, config); err != nil {
		return err
	}

	slog.InfoContext(ctx, "setting up underlay")
	if err := hostnetwork.SetupUnderlay(ctx, hostConfig.Underlay); err != nil {
		return fmt.Errorf("failed to setup underlay: %w", err)
	}

	var resourceErrors []error
	failedL3Domains := sets.New[string]()
	reason := v1alpha1.FailedResourceReasonOverlayAttachmentFailed

	var configuredL3VNIs []hostnetwork.L3VNIParams
	for _, vni := range hostConfig.L3VNIs {
		slog.InfoContext(ctx, "setting up VNI", "vni", vni.VRF)
		if err := hostnetwork.SetupL3VNI(ctx, vni); err != nil {
			resourceErrors = append(resourceErrors, &openpeerrors.ResourceError{
				Obj: v1alpha1.FailedResource{
					Kind: openpeerrors.KindL3VNI, Name: vni.Name, Reason: reason, Message: err.Error(),
				},
			})
			failedL3Domains.Insert(vni.VRF)
			continue
		}
		configuredL3VNIs = append(configuredL3VNIs, vni)
	}

	var configuredL2VNIs []hostnetwork.L2VNIParams
	for _, vni := range hostConfig.L2VNIs {
		if failedL3Domains.Has(vni.VRF) {
			resourceErrors = append(resourceErrors, &openpeerrors.ResourceError{
				Obj: v1alpha1.FailedResource{
					Kind: openpeerrors.KindL2VNI, Name: vni.Name, Reason: reason,
					Message: fmt.Sprintf("L3 domain %q failed netlink provisioning", vni.VRF),
				},
			})
			continue
		}
		slog.InfoContext(ctx, "setting up L2VNI", "vni", vni.VNI)
		if err := hostnetwork.SetupL2VNI(ctx, vni); err != nil {
			resourceErrors = append(resourceErrors, &openpeerrors.ResourceError{
				Obj: v1alpha1.FailedResource{
					Kind: openpeerrors.KindL2VNI, Name: vni.Name, Reason: reason, Message: err.Error(),
				},
			})
			continue
		}
		if err := bridgerefresh.StartForVNI(ctx, vni); err != nil {
			resourceErrors = append(resourceErrors, &openpeerrors.ResourceError{
				Obj: v1alpha1.FailedResource{
					Kind: openpeerrors.KindL2VNI, Name: vni.Name, Reason: reason, Message: err.Error(),
				},
			})
			continue
		}
		configuredL2VNIs = append(configuredL2VNIs, vni)
	}

	slog.InfoContext(ctx, "setting up passthrough")
	if hostConfig.L3Passthrough != nil {
		if err := hostnetwork.SetupPassthrough(ctx, *hostConfig.L3Passthrough); err != nil {
			resourceErrors = append(resourceErrors, &openpeerrors.ResourceError{
				Obj: v1alpha1.FailedResource{
					Kind: openpeerrors.KindL3Passthrough, Name: config.L3Passthrough[0].Name, Reason: reason, Message: err.Error(),
				},
			})
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
		return fmt.Errorf("failed to remove deleted vnis: %w", err)
	}
	bridgerefresh.StopForRemovedVNIs(configuredL2VNIs)

	if len(config.L3Passthrough) == 0 {
		if err := hostnetwork.RemovePassthrough(config.targetNamespace); err != nil {
			return fmt.Errorf("failed to remove passthrough: %w", err)
		}
	}
	return errors.Join(resourceErrors...)
}

// nonRecoverableHostError tells whether the router pod
// should be restarted instead of being reconfigured.
func nonRecoverableHostError(e error) bool {
	underlayExistsError := hostnetwork.UnderlayExistsError("")
	return errors.As(e, &underlayExistsError)
}

func ensureSysctlsForConfig(ctx context.Context, config interfacesConfiguration) error {
	slog.InfoContext(ctx, "ensuring sysctls")
	sysctls := []sysctl.Sysctl{
		sysctl.IPv4Forwarding(),
		sysctl.IPv6Forwarding(),
		sysctl.ArpAcceptAll(),
		sysctl.ArpAcceptDefault(),
		sysctl.AcceptUntrackedNADefault(),
		sysctl.AcceptUntrackedNAAll(),
	}
	if isSRV6(config.Underlays[0]) {
		sysctls = append(sysctls,
			sysctl.Seg6MakeFlowLabel(),
			sysctl.EnableSeg6All(),
		)
	}
	if err := sysctl.EnsureInNamespace(
		config.targetNamespace,
		sysctls...,
	); err != nil {
		return fmt.Errorf("failed to ensure sysctls: %w", err)
	}
	return nil
}

func isSRV6(underlay v1alpha1.Underlay) bool {
	return underlay.Spec.SRV6 != nil
}
