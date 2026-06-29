// SPDX-License-Identifier:Apache-2.0

package routerconfiguration

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/internal/conversion"
	openpeerrors "github.com/openperouter/openperouter/internal/errors"
	"github.com/openperouter/openperouter/internal/grout"
	"github.com/openperouter/openperouter/internal/hostnetwork"
	"k8s.io/apimachinery/pkg/util/sets"
)

// GroutDatapathConfigurator configures the host via the grout DPDK daemon.
type GroutDatapathConfigurator struct {
	conversion.GroutDatapathConfigValidator

	groutSocketPath string
}

func NewGroutConfigurator(groutSocketPath string) *GroutDatapathConfigurator {
	return &GroutDatapathConfigurator{
		groutSocketPath: groutSocketPath,
	}
}

func (g *GroutDatapathConfigurator) Configure(ctx context.Context, config interfacesConfiguration) error {
	groutClient := grout.NewClient(g.groutSocketPath)

	currentUnderlayIfaces, err := hostnetwork.UnderlayInterfaces(config.targetNamespace)
	if err != nil {
		return fmt.Errorf("failed to check if target namespace %s has underlay: %w", config.targetNamespace, err)
	}
	if len(currentUnderlayIfaces) > 0 && len(config.Underlays) == 0 {
		cleanupGroutInterfaces(ctx, groutClient, config.targetNamespace, currentUnderlayIfaces)
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
		L3Passthrough: config.L3Passthrough,
	}
	hostConfig, err := conversion.APItoHostConfig(config.nodeIndex, config.targetNamespace, apiConfig)
	if err != nil {
		return fmt.Errorf("failed to convert config to host configuration: %w", err)
	}

	slog.InfoContext(ctx, "setting up underlay")
	if err := grout.SetupUnderlay(ctx, groutClient, hostConfig.Underlay); err != nil {
		return fmt.Errorf("failed to setup underlay: %w", err)
	}

	var resourceErrors []error
	failedL3Domains := sets.New[string]()
	reason := v1alpha1.FailedResourceReasonOverlayAttachmentFailed

	var configuredL3VNIs []hostnetwork.L3VNIParams
	for _, vni := range hostConfig.L3VNIs {
		slog.InfoContext(ctx, "setting up VNI", "vni", vni.VRF)
		if err := grout.SetupL3VNI(ctx, groutClient, vni); err != nil {
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

	slog.InfoContext(ctx, "setting up passthrough")
	if hostConfig.L3Passthrough != nil {
		if err := grout.SetupPassthrough(ctx, groutClient, *hostConfig.L3Passthrough); err != nil {
			resourceErrors = append(resourceErrors, &openpeerrors.ResourceError{
				Obj: v1alpha1.FailedResource{
					Kind: openpeerrors.KindL3Passthrough, Name: config.L3Passthrough[0].Name, Reason: reason, Message: err.Error(),
				},
			})
		}
	}

	if len(resourceErrors) > 0 {
		slog.Error("configuration failed", "errors", resourceErrors)
	}

	configuredVNIs := make([]hostnetwork.VNIParams, 0, len(configuredL3VNIs))
	configuredVRFs := []string{}
	for _, vni := range configuredL3VNIs {
		configuredVNIs = append(configuredVNIs, vni.VNIParams)
		configuredVRFs = append(configuredVRFs, vni.VRF)
	}

	slog.InfoContext(ctx, "removing deleted vnis")
	if err := grout.RemoveNonConfiguredVNIs(ctx, groutClient, config.targetNamespace, configuredVNIs); err != nil {
		return fmt.Errorf("failed to remove deleted vnis: %w", err)
	}

	slog.InfoContext(ctx, "removing deleted vrfs")
	if err := grout.RemoveNonConfiguredVRFs(ctx, groutClient, config.targetNamespace, configuredVRFs); err != nil {
		return fmt.Errorf("failed to remove deleted vrfs: %w", err)
	}

	if len(apiConfig.L3Passthrough) == 0 {
		if err := grout.RemovePassthrough(ctx, groutClient); err != nil {
			return fmt.Errorf("failed to remove passthrough: %w", err)
		}
	}

	return errors.Join(resourceErrors...)
}

func cleanupGroutInterfaces(ctx context.Context, groutClient *grout.Client, targetNamespace string, currentUnderlayIfaces []hostnetwork.UnderlayInterface) {
	slog.InfoContext(ctx, "underlay removed, cleaning up VNIs and underlay interfaces")
	if err := grout.RemoveAllVNIs(ctx, groutClient, targetNamespace); err != nil {
		slog.Warn("failed to remove vnis after underlay removal", "err", err)
	}
	if err := grout.RemoveAllVRFs(ctx, groutClient, targetNamespace); err != nil {
		slog.Warn("failed to remove vrfs after underlay removal", "err", err)
	}
	if err := grout.RestoreUnderlay(ctx, groutClient, targetNamespace, currentUnderlayIfaces); err != nil {
		slog.Warn("failed to remove underlay after underlay removal", "err", err)
	}
}
