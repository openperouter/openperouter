// SPDX-License-Identifier:Apache-2.0

package routerconfiguration

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/openperouter/openperouter/internal/conversion"
	openpeerrors "github.com/openperouter/openperouter/internal/errors"
	"github.com/openperouter/openperouter/internal/grout"
	"github.com/openperouter/openperouter/internal/grout/devicestate"
	"github.com/openperouter/openperouter/internal/hostnetwork"
	"github.com/openperouter/openperouter/internal/pci"
	"k8s.io/apimachinery/pkg/util/sets"
)

// GroutDatapathConfigurator configures the host via the grout DPDK daemon.
type GroutDatapathConfigurator struct {
	conversion.GroutDatapathConfigValidator

	groutSocketPath  string
	deviceStateStore devicestate.Store
	sysfs            pci.Sysfs
	underlayPortMode grout.UnderlayPortMode
}

func NewGroutConfigurator(groutSocketPath string, mode grout.UnderlayPortMode) *GroutDatapathConfigurator {
	return &GroutDatapathConfigurator{
		groutSocketPath:  groutSocketPath,
		deviceStateStore: devicestate.Store{Dir: "/var/run/grout/device-state"},
		sysfs:            pci.Sysfs{Root: "/sys"},
		underlayPortMode: mode,
	}
}

// InterfaceNameForFRR returns the grout port an underlay network device is
// attached to, as FRR sees the port and not the device. The tunnel endpoint
// port is not attached to a device and is named after the interface itself.
func (g *GroutDatapathConfigurator) InterfaceNameForFRR(name string) string {
	if name == hostnetwork.TunnelLoopback {
		return name
	}
	return grout.UnderlayPortName(name)
}

func (g *GroutDatapathConfigurator) Configure(ctx context.Context, config interfacesConfiguration) error {
	groutClient := grout.NewClient(g.groutSocketPath)

	ovsClient := &hostnetwork.OVSClient{}
	defer ovsClient.Close()

	currentUnderlayIfaces, err := grout.UnderlayInterfaces(g.deviceStateStore)
	if err != nil {
		return fmt.Errorf("failed to list current grout underlay interfaces: %w", err)
	}
	if len(currentUnderlayIfaces) > 0 && len(config.Underlays) == 0 {
		return g.cleanupGroutInterfaces(ctx, groutClient, ovsClient, config.targetNamespace, currentUnderlayIfaces)
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

	slog.InfoContext(ctx, "setting up underlay")
	if err = grout.SetupUnderlay(ctx, groutClient, hostConfig.Underlay, g.underlayPortMode, g.deviceStateStore, g.sysfs); err != nil {
		return fmt.Errorf("failed to setup underlay: %w", err)
	}

	var resourceErrors []error
	failedL3Domains := sets.New[string]()

	var configuredL3VNIs []hostnetwork.L3VNIParams
	for _, vni := range hostConfig.L3VNIs {
		slog.InfoContext(ctx, "setting up VNI", "vni", vni.VRF)
		if err := grout.SetupL3VNI(ctx, groutClient, vni); err != nil {
			resourceErrors = append(resourceErrors,
				overlayAttachmentError(openpeerrors.KindL3VNI, vni.Name, err.Error()))
			failedL3Domains.Insert(vni.VRF)
			continue
		}
		configuredL3VNIs = append(configuredL3VNIs, vni)
	}

	var configuredL3VPNs []hostnetwork.L3VPNParams
	for _, l3vpn := range hostConfig.L3VPNs {
		slog.InfoContext(ctx, "setting up L3 VPN", "VRF", l3vpn.VRF)
		if err := grout.SetupL3VPN(ctx, groutClient, l3vpn); err != nil {
			resourceErrors = append(resourceErrors,
				overlayAttachmentError(openpeerrors.KindL3VPN, l3vpn.Name, err.Error()))
			failedL3Domains.Insert(l3vpn.VRF)
			continue
		}
		configuredL3VPNs = append(configuredL3VPNs, l3vpn)
	}

	var configuredL2VNIs []hostnetwork.L2VNIParams
	for _, vni := range hostConfig.L2VNIs {
		if failedL3Domains.Has(vni.VRF) {
			message := fmt.Sprintf("L3 domain %q failed provisioning", vni.VRF)
			resourceErrors = append(resourceErrors, overlayAttachmentError(openpeerrors.KindL2VNI, vni.Name, message))
			continue
		}
		slog.InfoContext(ctx, "setting up L2VNI", "vni", vni.VNI)
		if err := grout.SetupL2VNI(ctx, groutClient, ovsClient, vni); err != nil {
			resourceErrors = append(resourceErrors,
				overlayAttachmentError(openpeerrors.KindL2VNI, vni.Name, err.Error()))
			continue
		}
		configuredL2VNIs = append(configuredL2VNIs, vni)
	}

	slog.InfoContext(ctx, "setting up passthrough")
	if hostConfig.L3Passthrough != nil {
		if err := grout.SetupPassthrough(ctx, groutClient, *hostConfig.L3Passthrough); err != nil {
			resourceErrors = append(resourceErrors,
				overlayAttachmentError(openpeerrors.KindL3Passthrough, config.L3Passthrough[0].Name, err.Error()))
		}
	}

	configuredVNIs := make([]hostnetwork.VNIParams, 0, len(configuredL3VNIs)+len(configuredL2VNIs))
	configuredVRFs := []string{}
	for _, vni := range configuredL3VNIs {
		configuredVNIs = append(configuredVNIs, vni.VNIParams)
		configuredVRFs = append(configuredVRFs, vni.VRF)
	}
	for _, vni := range configuredL2VNIs {
		configuredVNIs = append(configuredVNIs, vni.VNIParams)
		configuredVRFs = append(configuredVRFs, vni.VRF)
	}
	for _, l3vpn := range configuredL3VPNs {
		configuredVRFs = append(configuredVRFs, l3vpn.VRF)
	}

	slog.InfoContext(ctx, "removing deleted vnis")
	if err := grout.RemoveNonConfiguredVNIs(ctx, groutClient, ovsClient, config.targetNamespace, configuredVNIs); err != nil {
		return errors.Join(append(resourceErrors, fmt.Errorf("failed to remove deleted vnis: %w", err))...)
	}

	slog.InfoContext(ctx, "removing deleted l3vpns")
	if err := grout.RemoveNonConfiguredL3VPNs(ctx, groutClient, configuredL3VPNs); err != nil {
		return fmt.Errorf("failed to remove deleted l3vpns: %w", err)
	}

	slog.InfoContext(ctx, "removing deleted vrfs")
	if err := grout.RemoveNonConfiguredVRFs(ctx, groutClient, config.targetNamespace, configuredVRFs); err != nil {
		return errors.Join(append(resourceErrors, fmt.Errorf("failed to remove deleted vrfs: %w", err))...)
	}

	if len(apiConfig.L3Passthrough) == 0 {
		if err := grout.RemovePassthrough(ctx, groutClient); err != nil {
			return errors.Join(append(resourceErrors, fmt.Errorf("failed to remove passthrough: %w", err))...)
		}
	}

	return errors.Join(resourceErrors...)
}

func (g *GroutDatapathConfigurator) cleanupGroutInterfaces(
	ctx context.Context,
	groutClient *grout.Client,
	ovsClient *hostnetwork.OVSClient,
	targetNamespace string,
	currentUnderlayIfaces []hostnetwork.UnderlayInterface,
) error {
	slog.InfoContext(ctx, "underlay removed, cleaning up VNIs and underlay interfaces")
	if err := grout.RemoveAllVNIs(ctx, groutClient, ovsClient, targetNamespace); err != nil {
		slog.Warn("failed to remove vnis after underlay removal", "err", err)
	}
	if err := grout.RemoveAllL3VPNs(ctx, groutClient); err != nil {
		slog.Warn("failed to remove l3vpns after underlay removal", "err", err)
	}
	if err := grout.RemoveAllVRFs(ctx, groutClient, targetNamespace); err != nil {
		slog.Warn("failed to remove vrfs after underlay removal", "err", err)
	}
	if err := grout.RemoveTunnelEndpoint(ctx, groutClient); err != nil {
		slog.Warn("failed to remove tunnel endpoint after underlay removal", "err", err)
	}
	if err := grout.RestoreUnderlay(ctx, groutClient, targetNamespace,
		currentUnderlayIfaces, g.deviceStateStore, g.sysfs); err != nil {
		return fmt.Errorf("failed to remove underlay after underlay removal: %w", err)
	}
	return nil
}
