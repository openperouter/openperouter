// SPDX-License-Identifier:Apache-2.0

package routerconfiguration

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"

	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/internal/conversion"
	openpeerrors "github.com/openperouter/openperouter/internal/errors"
	"github.com/openperouter/openperouter/internal/hostnetwork"
	"github.com/openperouter/openperouter/internal/hostnetwork/bridgerefresh"
	"github.com/openperouter/openperouter/internal/sysctl"
	"github.com/vishvananda/netns"
	"k8s.io/apimachinery/pkg/util/sets"
)

type interfacesConfiguration struct {
	targetNamespace string
	nodeIndex       int
	conversion.APIConfigData
}

// vnis holds the VNIs whose devices must stay on the node after a
// datapath reconcile cycle, either because they were configured or because
// their setup failed transiently.
type vnis struct {
	l3VNIs   []hostnetwork.L3VNIParams
	l3VPNs   []hostnetwork.L3VPNParams
	l2VNIs   []hostnetwork.L2VNIParams
	l2Trunks []hostnetwork.L2TrunkParams
}

// KernelDatapathConfigurator configures the host via kernel netlink calls.
// It supports the full API surface.
type KernelDatapathConfigurator struct {
	conversion.KernelDatapathConfigValidator
	RouterMACPrefix string
}

// InterfaceNameForFRR returns the name unchanged: in the kernel datapath FRR
// peers over the underlay network devices themselves.
func (k *KernelDatapathConfigurator) InterfaceNameForFRR(name string) string {
	return name
}

func (k *KernelDatapathConfigurator) Configure(ctx context.Context, config interfacesConfiguration) error { // nolint:gocognit
	routerMAC, err := hostnetwork.RouterMACForNode(k.RouterMACPrefix, config.nodeIndex)
	if err != nil {
		return fmt.Errorf("failed to generate router MAC: %w", err)
	}
	ovsClient := &hostnetwork.OVSClient{}
	defer ovsClient.Close()

	currentUnderlayIfaces, err := hostnetwork.UnderlayInterfaces(config.targetNamespace)
	if err != nil {
		return fmt.Errorf("failed to check if target namespace %s has underlay: %w", config.targetNamespace, err)
	}
	if len(currentUnderlayIfaces) > 0 && len(config.Underlays) == 0 {
		restoreUnderlay(ctx, ovsClient, config.targetNamespace, currentUnderlayIfaces)
		return nil
	}

	if len(config.Underlays) == 0 {
		return nil // nothing to do
	}

	slog.InfoContext(ctx, "configure interface start", "namespace", config.targetNamespace)
	defer slog.InfoContext(ctx, "configure interface end", "namespace", config.targetNamespace)
	hostConfig, err := conversion.APItoHostConfig(config.nodeIndex, config.targetNamespace, config.APIConfigData)
	if err != nil {
		return fmt.Errorf("failed to convert config to host configuration: %w", err)
	}

	// Fail closed before the legacy purge deletes the devices currently carrying
	// traffic: on a kernel without VNI filtering the migration cannot complete.
	configuredPorts := configuredVXLanPorts(hostConfig)
	if len(configuredPorts) > 0 {
		if err := hostnetwork.EnsureVniFilterSupport(config.targetNamespace); err != nil {
			return &openpeerrors.ResourceError{Obj: v1alpha1.FailedResource{
				Kind:    openpeerrors.KindUnderlay,
				Name:    config.Underlays[0].Name,
				Reason:  v1alpha1.FailedResourceReasonValidationFailed,
				Message: err.Error(),
			}}
		}
	}

	// This must run before underlay interfaces are added to the namespace.
	if err := ensureSysctlsForConfig(ctx, config); err != nil {
		return err
	}

	removeAllVNIs, err := areAllUnderlayInterfacesToBeRemoved(ctx, config, hostConfig)
	if err != nil {
		return err
	}
	if removeAllVNIs {
		// VXLAN tunnels are bound to the current underlay interfaces. If all underlay interfaces are being
		// replaced, tear down the VNIs first so they don't reference stale interfaces; they'll be recreated
		// on top of the new underlay.
		if err := hostnetwork.RemoveAllVNIs(ovsClient, config.targetNamespace); err != nil {
			slog.Warn("failed to remove vnis during underlay change", "err", err)
		}
		if err := hostnetwork.RemoveAllTrunks(config.targetNamespace); err != nil {
			slog.Warn("failed to remove l2trunks during underlay change", "err", err)
		}
		bridgerefresh.StopAllVNIs()
	}

	slog.InfoContext(ctx, "setting up underlay")

	if err := hostnetwork.SetupUnderlay(ctx, hostConfig.Underlay); err != nil {
		return fmt.Errorf("failed to setup underlay: %w", err)
	}

	for i := range hostConfig.L3VNIs {
		hostConfig.L3VNIs[i].RouterMAC = routerMAC
	}

	var resourceErrors []error

	// The purge and the new external devices cannot share a UDP port, so every
	// legacy device must go before setupVNIs creates the first external one. A
	// device that fails to purge does not block every other VNI's setup: a VNI
	// that actually needs the same port fails on its own in setupVNIs, with its
	// own per-resource error; one unrelated stuck device must not take down
	// every VNI on the node.
	if err := hostnetwork.PurgeLegacyVXLans(config.targetNamespace, configuredPorts); err != nil {
		resourceErrors = append(resourceErrors, fmt.Errorf("failed to purge legacy VXLAN devices: %w", err))
	}

	// spec.trunk is immutable, so converting a standalone L2VNI to a
	// trunk-attached one is a delete-and-recreate. Release the VNI from the old
	// device before setup adds it to the trunk's shared device. A reclaim
	// failure is scoped the same way: it must not block every other VNI's setup.
	if err := hostnetwork.ReclaimVNIs(config.targetNamespace, hostConfig.L2VNIs); err != nil {
		resourceErrors = append(resourceErrors, fmt.Errorf("failed to reclaim trunk VNIs: %w", err))
	}

	vnisToKeep, setupErrors := setupVNIs(ctx, ovsClient, config.targetNamespace, hostConfig)
	resourceErrors = append(resourceErrors, setupErrors...)
	slog.InfoContext(ctx, "setting up passthrough")
	if hostConfig.L3Passthrough != nil {
		if err := hostnetwork.SetupPassthrough(ctx, *hostConfig.L3Passthrough); err != nil {
			resourceErrors = append(resourceErrors, overlayAttachmentError(
				openpeerrors.KindL3Passthrough, config.L3Passthrough[0].Name, err.Error()))
		}
	}

	configuredVNIs := make([]hostnetwork.VNIParams, 0, len(vnisToKeep.l3VNIs)+len(vnisToKeep.l2VNIs))
	configuredVRFs := map[string]bool{}
	for _, vni := range vnisToKeep.l3VNIs {
		configuredVNIs = append(configuredVNIs, vni.VNIParams)
		configuredVRFs[vni.VRF] = true
	}
	for _, l2vni := range vnisToKeep.l2VNIs {
		configuredVRFs[l2vni.VRF] = true
		// A trunk-attached L2VNI owns no standalone devices; its endpoint is
		// owned by the trunk's own garbage collection.
		if l2vni.Trunk != nil {
			continue
		}
		configuredVNIs = append(configuredVNIs, l2vni.VNIParams)
	}
	for _, l3vpn := range vnisToKeep.l3VPNs {
		configuredVRFs[l3vpn.VRF] = true
	}

	slog.InfoContext(ctx, "removing deleted vnis")
	if err := hostnetwork.RemoveNonConfiguredVNIs(ovsClient, config.targetNamespace, configuredVNIs); err != nil {
		return errors.Join(append(resourceErrors, fmt.Errorf("failed to remove deleted vnis: %w", err))...)
	}
	if err := hostnetwork.RemoveNonConfiguredTrunks(config.targetNamespace, vnisToKeep.l2Trunks, vnisToKeep.l2VNIs); err != nil {
		return errors.Join(append(resourceErrors, fmt.Errorf("failed to remove deleted l2trunks: %w", err))...)
	}
	bridgerefresh.StopForRemovedVNIs(vnisToKeep.l2VNIs)

	slog.InfoContext(ctx, "removing deleted l3vpns")
	if err := hostnetwork.RemoveNonConfiguredL3VPNs(vnisToKeep.l3VPNs); err != nil {
		return errors.Join(append(resourceErrors, fmt.Errorf("failed to remove deleted l3vpns: %w", err))...)
	}

	slog.InfoContext(ctx, "removing deleted vrfs")
	if err := hostnetwork.RemoveNonConfiguredVRFs(config.targetNamespace,
		configuredVRFs); err != nil {
		return errors.Join(append(resourceErrors, fmt.Errorf("failed to remove deleted vrfs: %w", err))...)
	}

	if len(config.L3Passthrough) == 0 {
		if err := hostnetwork.RemovePassthrough(config.targetNamespace); err != nil {
			return errors.Join(append(resourceErrors, fmt.Errorf("failed to remove passthrough: %w", err))...)
		}
	}
	return errors.Join(resourceErrors...)
}

// configuredVXLanPorts returns every UDP port the node's VNIs use. VXLanPort
// is per-resource, so a node can legitimately use several, and the legacy purge
// must cover all of them.
func configuredVXLanPorts(hostConfig conversion.HostConfigData) []int32 {
	ports := sets.New[int32]()
	for _, vni := range hostConfig.L3VNIs {
		if vni.VXLanPort != nil {
			ports.Insert(*vni.VXLanPort)
		}
	}
	for _, vni := range hostConfig.L2VNIs {
		if vni.VXLanPort != nil {
			ports.Insert(*vni.VXLanPort)
		}
	}
	for _, trunk := range hostConfig.L2Trunks {
		ports.Insert(trunk.VXLanPort)
	}
	result := ports.UnsortedList()
	slices.Sort(result)
	return result
}

// setupVNIs sets up every VNI of the datapath reconcile cyle. The underlay MTU
// and the VTEP loopback are node-wide, so it opens the router namespace once
// and resolves them once for the whole reconcile cycle.
func setupVNIs(
	ctx context.Context,
	ovsClient *hostnetwork.OVSClient,
	routerNamespace string,
	hostConfig conversion.HostConfigData,
) (vnis, []error) {
	if len(hostConfig.L3VNIs) == 0 && len(hostConfig.L3VPNs) == 0 &&
		len(hostConfig.L2VNIs) == 0 && len(hostConfig.L2Trunks) == 0 {
		return vnis{}, nil
	}

	ns, err := netns.GetFromPath(routerNamespace)
	if err != nil {
		// The namespace lives on the host, so failing to open it now does not
		// mean it will be missing on the next reconcile.
		openErr := fmt.Errorf("%w: failed to get network namespace %s: %w",
			openpeerrors.ErrTransient, routerNamespace, err)
		return vnisFromHostConfig(hostConfig), errorsForVNIs(hostConfig, openErr)
	}
	defer func() {
		if err := ns.Close(); err != nil {
			slog.Error("failed to close router namespace", "namespace", routerNamespace, "error", err)
		}
	}()

	configurator, err := hostnetwork.NewVNIConfigurator(ns, ovsClient)
	if err != nil {
		configuratorErr := fmt.Errorf("%w: %w", openpeerrors.ErrTransient, err)
		return vnisFromHostConfig(hostConfig), errorsForVNIs(hostConfig, configuratorErr)
	}

	failedL3Domains := map[string]struct{}{}
	failedTrunks := map[string]struct{}{}
	configuredTrunks, trunkErrors := setupL2Trunks(ctx, configurator, hostConfig.L2Trunks, failedTrunks)
	configuredL3VNIs, l3VNIErrors := setupL3VNIs(ctx, configurator, hostConfig.L3VNIs, failedL3Domains)
	configuredL3VPNs, l3VPNErrors := setupL3VPNs(ctx, configurator, hostConfig.L3VPNs, failedL3Domains)

	configuredL2VNIs, l2VNIErrors := setupL2VNIs(ctx, configurator, hostConfig.L2VNIs, failedL3Domains, failedTrunks)

	return vnis{
		l3VNIs:   configuredL3VNIs,
		l3VPNs:   configuredL3VPNs,
		l2VNIs:   configuredL2VNIs,
		l2Trunks: configuredTrunks,
	}, slices.Concat(trunkErrors, l3VNIErrors, l3VPNErrors, l2VNIErrors)
}

// vnisFromHostConfig returns every VNI of the host configuration, so a failure
// to resolve the state they share keeps their devices for the next reconcile to
// retry.
func vnisFromHostConfig(hostConfig conversion.HostConfigData) vnis {
	return vnis{
		l3VNIs:   hostConfig.L3VNIs,
		l3VPNs:   hostConfig.L3VPNs,
		l2VNIs:   hostConfig.L2VNIs,
		l2Trunks: hostConfig.L2Trunks,
	}
}

// errorsForVNIs reports err against every configured VNI, so each one shows up
// in its status with the reason it was not set up.
func errorsForVNIs(hostConfig conversion.HostConfigData, err error) []error {
	allErrors := make([]error, 0,
		len(hostConfig.L3VNIs)+len(hostConfig.L3VPNs)+len(hostConfig.L2VNIs))
	for _, vni := range hostConfig.L3VNIs {
		allErrors = append(allErrors, overlayAttachmentError(openpeerrors.KindL3VNI, vni.Name, err.Error()))
	}
	for _, l3vpn := range hostConfig.L3VPNs {
		allErrors = append(allErrors, overlayAttachmentError(openpeerrors.KindL3VPN, l3vpn.Name, err.Error()))
	}
	for _, vni := range hostConfig.L2VNIs {
		allErrors = append(allErrors, overlayAttachmentError(openpeerrors.KindL2VNI, vni.Name, err.Error()))
	}
	for _, trunk := range hostConfig.L2Trunks {
		allErrors = append(allErrors, overlayAttachmentError(openpeerrors.KindL2Trunk, trunk.Name, err.Error()))
	}
	return allErrors
}

// A VNIConfigurator implements all four interfaces below. Each setup function
// accepts only the one it uses.
type l3VNIConfigurator interface {
	SetupL3VNI(context.Context, hostnetwork.L3VNIParams) error
}

type l3VPNConfigurator interface {
	SetupL3VPN(context.Context, hostnetwork.L3VPNParams) error
}

type l2VNIConfigurator interface {
	SetupL2VNI(context.Context, hostnetwork.L2VNIParams) error
	SetupL2TrunkVNI(context.Context, hostnetwork.L2VNIParams) error
}

type l2TrunkConfigurator interface {
	SetupL2Trunk(context.Context, hostnetwork.L2TrunkParams) error
}

// setupL3VNIs sets up every L3VNI through the configurator, and returns the
// ones it configured plus an error for each that failed. A failed L3VNI marks
// its routing domain as failed, so its L2VNIs are not set up on top of a
// missing routing domain.
func setupL3VNIs(
	ctx context.Context,
	configurator l3VNIConfigurator,
	l3VNIs []hostnetwork.L3VNIParams,
	failedL3Domains map[string]struct{},
) ([]hostnetwork.L3VNIParams, []error) {
	var configured []hostnetwork.L3VNIParams
	var errs []error
	for _, vni := range l3VNIs {
		slog.InfoContext(ctx, "setting up VNI", "vni", vni.VRF)
		if err := configurator.SetupL3VNI(ctx, vni); err != nil {
			errs = append(errs, overlayAttachmentError(openpeerrors.KindL3VNI, vni.Name, err.Error()))
			failedL3Domains[vni.VRF] = struct{}{}
			continue
		}
		configured = append(configured, vni)
	}
	return configured, errs
}

// setupL3VPNs sets up every L3VPN through the configurator, and returns the
// ones it configured plus an error for each that failed.
func setupL3VPNs(
	ctx context.Context,
	configurator l3VPNConfigurator,
	l3VPNs []hostnetwork.L3VPNParams,
	failedL3Domains map[string]struct{},
) ([]hostnetwork.L3VPNParams, []error) {
	var configured []hostnetwork.L3VPNParams
	var errs []error
	for _, l3vpn := range l3VPNs {
		slog.InfoContext(ctx, "setting up L3 VPN", "VRF", l3vpn.VRF)
		if err := configurator.SetupL3VPN(ctx, l3vpn); err != nil {
			errs = append(errs, overlayAttachmentError(openpeerrors.KindL3VPN, l3vpn.Name, err.Error()))
			failedL3Domains[l3vpn.VRF] = struct{}{}
			continue
		}
		configured = append(configured, l3vpn)
	}
	return configured, errs
}

// setupL2Trunks sets up every L2Trunk before the L2VNIs that reference them.
// A failed trunk is reported and its L2VNIs are not configured on top of it.
func setupL2Trunks(
	ctx context.Context,
	configurator l2TrunkConfigurator,
	l2Trunks []hostnetwork.L2TrunkParams,
	failedTrunks map[string]struct{},
) ([]hostnetwork.L2TrunkParams, []error) {
	var kept []hostnetwork.L2TrunkParams
	var errs []error
	for _, trunk := range l2Trunks {
		slog.InfoContext(ctx, "setting up L2Trunk", "l2trunk", trunk.Name, "namespace", trunk.Namespace)
		err := configurator.SetupL2Trunk(ctx, trunk)
		if err != nil {
			errs = append(errs, overlayAttachmentError(openpeerrors.KindL2Trunk, trunk.Name, err.Error()))
			failedTrunks[hostnetwork.TrunkID(trunk.Namespace, trunk.Name)] = struct{}{}
		}
		// Mirror the L2VNI policy: a transient failure keeps the devices the
		// trunk already has; any other failure lets the cleanup remove them.
		if err == nil || isErrorTransient(err) {
			kept = append(kept, trunk)
		}
	}
	return kept, errs
}

// setupL2VNIs sets up every L2VNI whose routing domain and trunk are healthy,
// and returns the ones whose devices must stay on the node, plus an error for
// each L2VNI that failed. An L2VNI of a failed routing domain or trunk is
// reported and is not set up on top of the missing dependency.
func setupL2VNIs(
	ctx context.Context,
	configurator l2VNIConfigurator,
	l2VNIs []hostnetwork.L2VNIParams,
	failedL3Domains map[string]struct{},
	failedTrunks map[string]struct{},
) ([]hostnetwork.L2VNIParams, []error) {
	var kept []hostnetwork.L2VNIParams
	var errs []error
	for _, vni := range l2VNIs {
		if _, domainFailed := failedL3Domains[vni.VRF]; domainFailed {
			errs = append(errs, overlayAttachmentError(openpeerrors.KindL2VNI, vni.Name,
				fmt.Sprintf("L3 domain %q failed netlink provisioning", vni.VRF)))
			continue
		}
		if vni.Trunk != nil && vni.Trunk.TrunkID != "" {
			if _, trunkFailed := failedTrunks[vni.Trunk.TrunkID]; trunkFailed {
				errs = append(errs, overlayAttachmentError(openpeerrors.KindL2VNI, vni.Name,
					fmt.Sprintf("L2Trunk %q failed netlink provisioning", vni.Trunk.Name)))
				continue
			}
		}
		err := setupL2VNI(ctx, configurator, vni)
		if err != nil {
			errs = append(errs, overlayAttachmentError(openpeerrors.KindL2VNI, vni.Name, err.Error()))
		}
		// A transient failure keeps the devices the VNI already has on the node,
		// possibly from an earlier reconcile, so the next one retries it; any
		// other failure leaves the VNI out, so the cleanup that follows removes
		// its devices.
		if err == nil || isErrorTransient(err) {
			kept = append(kept, vni)
		}
	}
	return kept, errs
}

func setupL2VNI(
	ctx context.Context,
	configurator l2VNIConfigurator,
	vni hostnetwork.L2VNIParams,
) error {
	if vni.Trunk != nil && vni.Trunk.TrunkID != "" {
		slog.InfoContext(ctx, "setting up trunk-attached L2VNI",
			"vni", vni.VNI, "trunk", vni.Trunk.Name, "tag", vni.Trunk.VLAN)
		if err := configurator.SetupL2TrunkVNI(ctx, vni); err != nil {
			return err
		}
		return bridgerefresh.StartForVNI(ctx, vni)
	}
	slog.InfoContext(ctx, "setting up L2VNI", "vni", vni.VNI)
	if err := configurator.SetupL2VNI(ctx, vni); err != nil {
		return err
	}
	return bridgerefresh.StartForVNI(ctx, vni)
}

// isErrorTransient reports whether a setup failure may go away on the next
// reconcile. A check that does not depend on the state of the host, and would
// therefore fail the same way on every retry, belongs in validation before
// Configure and must not be marked transient.
func isErrorTransient(err error) bool {
	return errors.Is(err, openpeerrors.ErrTransient)
}

func overlayAttachmentError(kind v1alpha1.FailedResourceKind, name, message string) error {
	return &openpeerrors.ResourceError{
		Obj: v1alpha1.FailedResource{
			Kind:    kind,
			Name:    name,
			Reason:  v1alpha1.FailedResourceReasonOverlayAttachmentFailed,
			Message: message,
		},
	}
}

func restoreUnderlay(
	ctx context.Context,
	ovsClient *hostnetwork.OVSClient,
	targetNamespace string,
	currentUnderlayIfaces []hostnetwork.UnderlayInterface,
) {
	slog.InfoContext(ctx, "underlay removed, cleaning up VNIs and underlay interfaces")

	if err := hostnetwork.RemoveAllVNIs(ovsClient, targetNamespace); err != nil {
		slog.Warn("failed to remove vnis after underlay removal", "err", err)
	}

	if err := hostnetwork.RemoveAllL3VPNs(); err != nil {
		slog.Warn("failed to remove l3vpns after underlay removal", "err", err)
	}

	if err := hostnetwork.RemoveAllVRFs(targetNamespace); err != nil {
		slog.Warn("failed to remove vrfs after underlay removal", "err", err)
	}

	bridgerefresh.StopAllVNIs()
	if err := hostnetwork.RestoreUnderlay(ctx, targetNamespace,
		currentUnderlayIfaces); err != nil {
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
		sysctl.DisableRPFilter("default"),
		sysctl.DisableRPFilter("all"),
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

// areAllUnderlayInterfacesToBeRemoved tells whether every underlay interface
// currently in the namespace is being replaced, considering both the host
// network devices and the CNI-provisioned interfaces.
func areAllUnderlayInterfacesToBeRemoved(
	ctx context.Context,
	config interfacesConfiguration,
	hostConfig conversion.HostConfigData,
) (bool, error) {
	existing, err := hostnetwork.UnderlayInterfaces(config.targetNamespace)
	if err != nil {
		return false, fmt.Errorf("failed to list existing underlay interfaces: %w", err)
	}

	toRemove := hostnetwork.UnderlayInterfacesToRemove(existing, hostConfig.Underlay.UnderlayInterfaces)
	allRemoved := len(toRemove) > 0 && len(toRemove) == len(existing)
	if allRemoved {
		slog.InfoContext(ctx, "all underlay interfaces removed, cleaning up VNIs before interface swap",
			"removed", toRemove,
			"requested", hostConfig.Underlay.UnderlayInterfaces,
		)
	}
	return allRemoved, nil
}
