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

// KernelDatapathConfigurator configures the host via kernel netlink calls.
// It supports the full API surface.
type KernelDatapathConfigurator struct {
	conversion.KernelDatapathConfigValidator
}

func (k *KernelDatapathConfigurator) Configure(ctx context.Context, config interfacesConfiguration) error { // nolint:gocognit
	hasAlreadyUnderlay, err := hostnetwork.HasUnderlayInterface(config.targetNamespace)
	if err != nil {
		return fmt.Errorf("failed to check if target namespace %s has underlay: %w", config.targetNamespace, err)
	}
	if hasAlreadyUnderlay && len(config.Underlays) == 0 {
		restoreUnderlay(ctx, config.targetNamespace)
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

	existing, err := hostnetwork.UnderlayInterfaces(config.targetNamespace)
	if err != nil {
		return fmt.Errorf("failed to list existing underlay interfaces: %w", err)
	}

	removedInterfaces := hostnetwork.UnderlayInterfacesWereRemoved(existing, hostConfig.Underlay.UnderlayInterfaces)
	if hostnetwork.UnderlayInterfacesWereRemoved(existing, hostConfig.Underlay.UnderlayInterfaces) {
		slog.InfoContext(ctx, "all underlay interfaces removed, cleaning up VNIs before interface swap",
			"removed", removedInterfaces, "requested", hostConfig.Underlay.UnderlayInterfaces)
		if err := hostnetwork.RemoveAllVNIs(config.targetNamespace); err != nil {
			slog.Warn("failed to remove vnis during underlay change", "err", err)
		}
		bridgerefresh.StopAllVNIs()
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

	var configuredL3VPNs []hostnetwork.L3VPNParams
	for _, l3vpn := range hostConfig.L3VPNs {
		slog.InfoContext(ctx, "setting up L3 VPN", "VRF", l3vpn.VRF)
		if err := hostnetwork.SetupL3VPN(ctx, l3vpn); err != nil {
			resourceErrors = append(resourceErrors, &openpeerrors.ResourceError{
				Obj: v1alpha1.FailedResource{
					Kind: openpeerrors.KindL3VPN, Name: l3vpn.Name, Reason: reason, Message: err.Error(),
				},
			})
			failedL3Domains.Insert(l3vpn.VRF)
			continue
		}
		configuredL3VPNs = append(configuredL3VPNs, l3vpn)
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

	configuredVNIs := make([]hostnetwork.VNIParams, 0, len(configuredL3VNIs)+len(configuredL2VNIs))
	configuredVRFs := map[string]bool{}
	for _, vni := range configuredL3VNIs {
		configuredVNIs = append(configuredVNIs, vni.VNIParams)
		configuredVRFs[vni.VRF] = true
	}
	for _, l2vni := range configuredL2VNIs {
		configuredVNIs = append(configuredVNIs, l2vni.VNIParams)
		configuredVRFs[l2vni.VRF] = true
	}
	for _, l3vpn := range configuredL3VPNs {
		configuredVRFs[l3vpn.VRF] = true
	}

	slog.InfoContext(ctx, "removing deleted vnis")
	if err := hostnetwork.RemoveNonConfiguredVNIs(config.targetNamespace, configuredVNIs); err != nil {
		return fmt.Errorf("failed to remove deleted vnis: %w", err)
	}
	bridgerefresh.StopForRemovedVNIs(configuredL2VNIs)

	slog.InfoContext(ctx, "removing deleted l3vpns")
	if err := hostnetwork.RemoveNonConfiguredL3VPNs(config.targetNamespace,
		configuredL3VPNs); err != nil {
		return fmt.Errorf("failed to remove deleted l3vpns: %w", err)
	}

	slog.InfoContext(ctx, "removing deleted vrfs")
	if err := hostnetwork.RemoveNonConfiguredVRFs(config.targetNamespace,
		configuredVRFs); err != nil {
		return fmt.Errorf("failed to remove deleted vrfs: %w", err)
	}

	if len(config.L3Passthrough) == 0 {
		if err := hostnetwork.RemovePassthrough(config.targetNamespace); err != nil {
			return fmt.Errorf("failed to remove passthrough: %w", err)
		}
	}
	return errors.Join(resourceErrors...)
}

func restoreUnderlay(ctx context.Context, targetNamespace string) {
	slog.InfoContext(ctx, "underlay removed, cleaning up VNIs and underlay interfaces")
	if err := hostnetwork.RemoveAllVNIs(targetNamespace); err != nil {
		slog.Warn("failed to remove vnis after underlay removal", "err", err)
	}
	if err := hostnetwork.RemoveAllL3VPNs(targetNamespace); err != nil {
		slog.Warn("failed to remove l3vpns after underlay removal", "err", err)
	}
	if err := hostnetwork.RemoveAllVRFs(targetNamespace); err != nil {
		slog.Warn("failed to remove vrfs after underlay removal", "err", err)
	}
	bridgerefresh.StopAllVNIs()
	if err := hostnetwork.RestoreUnderlay(ctx, targetNamespace); err != nil {
		slog.Warn("failed to remove underlay interfaces after underlay removal", "err", err)
	}
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
