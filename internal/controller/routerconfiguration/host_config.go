// SPDX-License-Identifier:Apache-2.0

package routerconfiguration

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"

	"k8s.io/apimachinery/pkg/util/sets"

	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/internal/conversion"
	openpeerrors "github.com/openperouter/openperouter/internal/errors"
	"github.com/openperouter/openperouter/internal/hostnetwork"
	"github.com/openperouter/openperouter/internal/hostnetwork/bridgerefresh"
	"github.com/openperouter/openperouter/internal/sysctl"
)

var setupSingleL2VNI = hostnetwork.SetupL2VNI
var newVNISharedState = hostnetwork.NewVNISharedState
var findUnderlayMTU = hostnetwork.FindUnderlayMTUInNamespace

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

func (k *KernelDatapathConfigurator) Configure(ctx context.Context, config interfacesConfiguration) error {
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

	// This must run before underlay interfaces are added to the namespace.
	if err := ensureSysctlsForConfig(ctx, config); err != nil {
		return err
	}

	removeVNIsBeforeUnderlaySwap(ctx, ovsClient, config.targetNamespace, hostConfig, currentUnderlayIfaces)

	slog.InfoContext(ctx, "setting up underlay")

	hostConfig.Underlay.ExistingInterfaces = currentUnderlayIfaces
	if err := hostnetwork.SetupUnderlay(ctx, hostConfig.Underlay); err != nil {
		return fmt.Errorf("failed to setup underlay: %w", err)
	}

	configured := setupOverlays(ctx, ovsClient, config, hostConfig)
	return removeStaleResources(ctx, ovsClient, config, configured)
}

// configuredResources is what a datapath pass configured successfully. The
// cleanup phase keeps these resources on the node and removes the rest.
type configuredResources struct {
	l3VNIs []hostnetwork.L3VNIParams
	l3VPNs []hostnetwork.L3VPNParams
	l2VNIs []hostnetwork.L2VNIParams
	errors []error
}

// vniState is the state resolved once per datapath pass and shared by all its
// overlays. The lookups fail independently, so their errors are carried along
// for each resource to report its own failure.
type vniState struct {
	shared    *hostnetwork.VNISharedState
	mtuErr    error
	sharedErr error
}

// setupOverlays configures the L3 and L2 overlays and the passthrough of the
// pass, and returns the resources that must stay on the node together with the
// errors of the ones that could not be configured.
func setupOverlays(
	ctx context.Context,
	ovsClient *hostnetwork.OVSClient,
	config interfacesConfiguration,
	hostConfig conversion.HostConfigData,
) configuredResources {
	// The underlay MTU and the router namespace are node-wide: resolve them
	// once and share them with every VNI of the pass instead of listing the
	// underlay links and opening the namespace once per resource.
	underlayMTU, underlayMTUErr := resolveUnderlayMTU(hostConfig, config.targetNamespace)
	shared, sharedErr := resolveVNISharedState(hostConfig, config.targetNamespace, underlayMTU)
	if sharedErr != nil {
		// Let each VNI report the failure when it tries to resolve its own
		// state, so the error stays attributed to the resource it affects.
		slog.Warn("failed to resolve the state shared by the VNIs", "error", sharedErr)
		shared = nil
	}
	if shared != nil {
		defer func() {
			if err := shared.Close(); err != nil {
				slog.Error("failed to close the VNI shared namespace", "namespace", config.targetNamespace, "error", err)
			}
		}()
	}

	state := vniState{shared: shared, mtuErr: underlayMTUErr, sharedErr: sharedErr}
	failedL3Domains := sets.New[string]()

	configuredL3VNIs, l3VNIErrors := setupL3VNIs(ctx, hostConfig.L3VNIs, state, failedL3Domains)
	configuredL3VPNs, l3VPNErrors := setupL3VPNs(ctx, hostConfig.L3VPNs, state, failedL3Domains)
	configuredL2VNIs, l2Errors := setupL2Overlays(ctx, ovsClient, hostConfig.L2VNIs, failedL3Domains, state)

	return configuredResources{
		l3VNIs: configuredL3VNIs,
		l3VPNs: configuredL3VPNs,
		l2VNIs: configuredL2VNIs,
		errors: slices.Concat(l3VNIErrors, l3VPNErrors, l2Errors, setupPassthrough(ctx, hostConfig, config)),
	}
}

// setupL3VNIs sets up each L3VNI and returns the ones that were configured and
// an error for each L3VNI that failed. The L3 domains of the failed L3VNIs are
// recorded so the L2VNIs depending on them are skipped.
func setupL3VNIs(
	ctx context.Context,
	l3VNIs []hostnetwork.L3VNIParams,
	state vniState,
	failedL3Domains sets.Set[string],
) ([]hostnetwork.L3VNIParams, []error) {
	var configuredL3VNIs []hostnetwork.L3VNIParams
	var resourceErrors []error
	for _, vni := range l3VNIs {
		slog.InfoContext(ctx, "setting up VNI", "vni", vni.VRF)
		if vni.LinkIPs != nil && state.mtuErr != nil {
			resourceErrors = append(resourceErrors, underlayMTUResourceError(openpeerrors.KindL3VNI, vni.Name, state.mtuErr))
			failedL3Domains.Insert(vni.VRF)
			continue
		}
		if err := hostnetwork.SetupL3VNI(ctx, vni, state.shared); err != nil {
			resourceErrors = append(resourceErrors, overlayResourceError(openpeerrors.KindL3VNI, vni.Name, err))
			failedL3Domains.Insert(vni.VRF)
			continue
		}
		configuredL3VNIs = append(configuredL3VNIs, vni)
	}
	return configuredL3VNIs, resourceErrors
}

// setupL3VPNs sets up each L3VPN and returns the ones that were configured and
// an error for each L3VPN that failed.
func setupL3VPNs(
	ctx context.Context,
	l3VPNs []hostnetwork.L3VPNParams,
	state vniState,
	failedL3Domains sets.Set[string],
) ([]hostnetwork.L3VPNParams, []error) {
	var configuredL3VPNs []hostnetwork.L3VPNParams
	var resourceErrors []error
	for _, l3vpn := range l3VPNs {
		slog.InfoContext(ctx, "setting up L3 VPN", "VRF", l3vpn.VRF)
		if l3vpn.LinkIPs != nil && state.mtuErr != nil {
			resourceErrors = append(resourceErrors, underlayMTUResourceError(openpeerrors.KindL3VPN, l3vpn.Name, state.mtuErr))
			failedL3Domains.Insert(l3vpn.VRF)
			continue
		}
		if err := hostnetwork.SetupL3VPN(ctx, l3vpn, state.shared); err != nil {
			resourceErrors = append(resourceErrors, overlayResourceError(openpeerrors.KindL3VPN, l3vpn.Name, err))
			failedL3Domains.Insert(l3vpn.VRF)
			continue
		}
		configuredL3VPNs = append(configuredL3VPNs, l3vpn)
	}
	return configuredL3VPNs, resourceErrors
}

// setupL2Overlays drops the L2VNIs whose L3 domain failed, reports the ones
// whose shared state could not be resolved, and sets up the rest.
func setupL2Overlays(
	ctx context.Context,
	ovsClient *hostnetwork.OVSClient,
	l2VNIs []hostnetwork.L2VNIParams,
	failedL3Domains sets.Set[string],
	state vniState,
) ([]hostnetwork.L2VNIParams, []error) {
	l2VNIs, l3DomainErrors := filterL2VNIsWithNoRoutingDomain(l2VNIs, failedL3Domains)
	stateErrors := l2VNIStateErrors(l2VNIs, state.mtuErr, state.sharedErr)
	resourceErrors := append(l3DomainErrors, stateErrors...)
	if len(stateErrors) > 0 || state.shared == nil {
		return nil, resourceErrors
	}
	configuredL2VNIs, setupErrors := setupL2VNIs(ctx, ovsClient, l2VNIs, state.shared)
	return configuredL2VNIs, append(resourceErrors, setupErrors...)
}

func setupPassthrough(ctx context.Context, hostConfig conversion.HostConfigData, config interfacesConfiguration) []error {
	slog.InfoContext(ctx, "setting up passthrough")
	if hostConfig.L3Passthrough == nil {
		return nil
	}
	if err := hostnetwork.SetupPassthrough(ctx, *hostConfig.L3Passthrough); err != nil {
		return []error{overlayResourceError(openpeerrors.KindL3Passthrough, config.L3Passthrough[0].Name, err)}
	}
	return nil
}

// removeStaleResources removes the resources that the pass did not configure,
// so devices left over from an earlier pass do not survive it.
func removeStaleResources(
	ctx context.Context,
	ovsClient *hostnetwork.OVSClient,
	config interfacesConfiguration,
	configured configuredResources,
) error {
	configuredVNIs, configuredVRFs := configuredVNIsAndVRFs(configured)

	// Both cleanup steps below need the same host and router namespace links:
	// list them once per namespace instead of once per resource type.
	cleanupLinks, err := hostnetwork.ListCleanupLinks(config.targetNamespace)
	if err != nil {
		return fmt.Errorf("failed to list links to clean up: %w", err)
	}
	defer func() {
		if err := cleanupLinks.Close(); err != nil {
			slog.Error("failed to close the cleanup namespace", "namespace", config.targetNamespace, "error", err)
		}
	}()

	slog.InfoContext(ctx, "removing deleted vnis")
	if err := cleanupLinks.RemoveNonConfiguredVNIs(ovsClient, configuredVNIs); err != nil {
		return fmt.Errorf("failed to remove deleted vnis: %w", err)
	}
	bridgerefresh.StopForRemovedVNIs(configured.l2VNIs)

	slog.InfoContext(ctx, "removing deleted l3vpns")
	if err := cleanupLinks.RemoveNonConfiguredL3VPNs(configured.l3VPNs); err != nil {
		return fmt.Errorf("failed to remove deleted l3vpns: %w", err)
	}

	slog.InfoContext(ctx, "removing deleted vrfs")
	if err := cleanupLinks.RemoveNonConfiguredVRFs(configuredVRFs); err != nil {
		return fmt.Errorf("failed to remove deleted vrfs: %w", err)
	}

	if len(config.L3Passthrough) == 0 {
		if err := hostnetwork.RemovePassthrough(config.targetNamespace); err != nil {
			return fmt.Errorf("failed to remove passthrough: %w", err)
		}
	}
	return errors.Join(configured.errors...)
}

// configuredVNIsAndVRFs returns the VNIs and VRFs that must stay on the node.
func configuredVNIsAndVRFs(configured configuredResources) ([]hostnetwork.VNIParams, map[string]bool) {
	configuredVNIs := make([]hostnetwork.VNIParams, 0, len(configured.l3VNIs)+len(configured.l2VNIs))
	configuredVRFs := map[string]bool{}
	for _, vni := range configured.l3VNIs {
		configuredVNIs = append(configuredVNIs, vni.VNIParams)
		configuredVRFs[vni.VRF] = true
	}
	for _, vni := range configured.l2VNIs {
		configuredVNIs = append(configuredVNIs, vni.VNIParams)
		configuredVRFs[vni.VRF] = true
	}
	for _, l3vpn := range configured.l3VPNs {
		configuredVRFs[l3vpn.VRF] = true
	}
	return configuredVNIs, configuredVRFs
}

// removeVNIsBeforeUnderlaySwap tears down the VNIs when every underlay
// interface currently in the namespace is being replaced.
func removeVNIsBeforeUnderlaySwap(
	ctx context.Context,
	ovsClient *hostnetwork.OVSClient,
	targetNamespace string,
	hostConfig conversion.HostConfigData,
	currentUnderlayIfaces []hostnetwork.UnderlayInterface,
) {
	if !areAllUnderlayInterfacesToBeRemoved(ctx, hostConfig, currentUnderlayIfaces) {
		return
	}
	// VXLAN tunnels are bound to the current underlay interfaces. If all underlay interfaces are being
	// replaced, tear down the VNIs first so they don't reference stale interfaces; they'll be recreated
	// on top of the new underlay.
	if err := hostnetwork.RemoveAllVNIs(ovsClient, targetNamespace); err != nil {
		slog.Warn("failed to remove vnis during underlay change", "err", err)
	}
	bridgerefresh.StopAllVNIs()
}

// filterL2VNIsWithNoRoutingDomain returns the L2VNIs whose L3 domain was set up,
// and an error for each of the others.
func filterL2VNIsWithNoRoutingDomain(
	l2VNIs []hostnetwork.L2VNIParams,
	failedL3Domains sets.Set[string],
) ([]hostnetwork.L2VNIParams, []error) {
	var healthy []hostnetwork.L2VNIParams
	var errs []error
	for _, vni := range l2VNIs {
		if failedL3Domains.Has(vni.VRF) {
			errs = append(errs, l2VNIError(vni, fmt.Sprintf("L3 domain %q failed netlink provisioning", vni.VRF)))
			continue
		}
		healthy = append(healthy, vni)
	}
	return healthy, errs
}

// setupL2VNIs sets up each L2VNI and returns the ones whose devices must stay
// on the node, and an error for each L2VNI that failed.
//
// An L2VNI that fails with openpeerrors.ErrTransient is kept even though its
// setup did not complete: what it already has on the node, possibly from an
// earlier pass, is left in place and the next reconcile retries it. Any other
// failure leaves the L2VNI out, so the cleanup that follows removes its
// devices. A check that does not depend on the state of the host, and so would
// fail the same way on every retry, belongs in validation before Configure and
// must not return ErrTransient.
func setupL2VNIs(
	ctx context.Context,
	ovsClient *hostnetwork.OVSClient,
	l2VNIs []hostnetwork.L2VNIParams,
	shared *hostnetwork.VNISharedState,
) ([]hostnetwork.L2VNIParams, []error) {
	var kept []hostnetwork.L2VNIParams
	var errs []error
	for _, vni := range l2VNIs {
		err := setupL2VNI(ctx, ovsClient, vni, shared)
		if errors.Is(err, openpeerrors.ErrTransient) {
			errs = append(errs, l2VNIError(vni, err.Error()))
			kept = append(kept, vni)
			continue
		}
		if err != nil {
			errs = append(errs, l2VNIError(vni, err.Error()))
			continue
		}
		kept = append(kept, vni)
	}
	return kept, errs
}

func setupL2VNI(ctx context.Context, ovsClient *hostnetwork.OVSClient, vni hostnetwork.L2VNIParams, shared *hostnetwork.VNISharedState) error {
	slog.InfoContext(ctx, "setting up L2VNI", "vni", vni.VNI)
	if err := setupSingleL2VNI(ctx, ovsClient, vni, shared); err != nil {
		return err
	}
	return bridgerefresh.StartForVNI(ctx, vni)
}

// resolveUnderlayMTU resolves the underlay MTU once for the host veths of the
// pass. The MTU is node-wide, so every L3VNI, L3VPN and L2VNI of the pass reuses
// it instead of resolving it per resource. Resources without a host veth do not
// need it, so no lookup happens when none of them has one.
func resolveUnderlayMTU(hostConfig conversion.HostConfigData, targetNamespace string) (int, error) {
	needsMTU := len(hostConfig.L2VNIs) > 0 ||
		slices.ContainsFunc(hostConfig.L3VNIs, func(vni hostnetwork.L3VNIParams) bool { return vni.LinkIPs != nil }) ||
		slices.ContainsFunc(hostConfig.L3VPNs, func(l3vpn hostnetwork.L3VPNParams) bool { return l3vpn.LinkIPs != nil })
	if !needsMTU {
		return 0, nil
	}
	return findUnderlayMTU(targetNamespace)
}

// resolveVNISharedState resolves the state shared by every VNI of a datapath
// pass. Resolving it opens the router namespace, so it must run once per pass
// and its result must be reused by every VNI.
func resolveVNISharedState(
	hostConfig conversion.HostConfigData,
	targetNamespace string,
	underlayMTU int,
) (*hostnetwork.VNISharedState, error) {
	if len(hostConfig.L2VNIs) == 0 && len(hostConfig.L3VNIs) == 0 && len(hostConfig.L3VPNs) == 0 {
		return nil, nil
	}
	shared, err := newVNISharedState(targetNamespace, underlayMTU)
	if err != nil {
		return nil, fmt.Errorf("could not resolve the VNI shared state: %w", err)
	}
	return shared, nil
}

func underlayMTUResourceError(kind v1alpha1.FailedResourceKind, name string, err error) error {
	return &openpeerrors.ResourceError{
		Obj: v1alpha1.FailedResource{
			Kind:    kind,
			Name:    name,
			Reason:  v1alpha1.FailedResourceReasonOverlayAttachmentFailed,
			Message: fmt.Sprintf("could not find underlay MTU: %v", err),
		},
	}
}

func overlayResourceError(kind v1alpha1.FailedResourceKind, name string, err error) error {
	return &openpeerrors.ResourceError{
		Obj: v1alpha1.FailedResource{
			Kind:    kind,
			Name:    name,
			Reason:  v1alpha1.FailedResourceReasonOverlayAttachmentFailed,
			Message: err.Error(),
		},
	}
}

func l2VNIError(vni hostnetwork.L2VNIParams, message string) error {
	return &openpeerrors.ResourceError{
		Obj: v1alpha1.FailedResource{
			Kind:    openpeerrors.KindL2VNI,
			Name:    vni.Name,
			Reason:  v1alpha1.FailedResourceReasonOverlayAttachmentFailed,
			Message: message,
		},
	}
}

// l2VNIStateErrors returns one error per L2VNI when the state needed to set
// them up could not be resolved.
func l2VNIStateErrors(l2VNIs []hostnetwork.L2VNIParams, mtuErr, stateErr error) []error {
	var err error
	switch {
	case len(l2VNIs) == 0:
		return nil
	case mtuErr != nil:
		err = fmt.Errorf("could not find underlay MTU: %w", mtuErr)
	case stateErr != nil:
		err = stateErr
	default:
		return nil
	}

	errs := make([]error, 0, len(l2VNIs))
	for _, vni := range l2VNIs {
		errs = append(errs, l2VNIError(vni, err.Error()))
	}
	return errs
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

	if err := hostnetwork.RemoveAllL3VPNs(targetNamespace); err != nil {
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
	hostConfig conversion.HostConfigData,
	existing []hostnetwork.UnderlayInterface,
) bool {
	toRemove := hostnetwork.UnderlayInterfacesToRemove(existing, hostConfig.Underlay.UnderlayInterfaces)
	allRemoved := len(toRemove) > 0 && len(toRemove) == len(existing)
	if allRemoved {
		slog.InfoContext(ctx, "all underlay interfaces removed, cleaning up VNIs before interface swap",
			"removed", toRemove,
			"requested", hostConfig.Underlay.UnderlayInterfaces,
		)
	}
	return allRemoved
}
