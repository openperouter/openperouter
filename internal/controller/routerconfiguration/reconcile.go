// SPDX-License-Identifier:Apache-2.0

package routerconfiguration

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net"
	"slices"

	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/internal/conversion"
	openpeerrors "github.com/openperouter/openperouter/internal/errors"
	"github.com/openperouter/openperouter/internal/frr"
	"github.com/openperouter/openperouter/internal/hostnetwork"
)

// DatapathConfigurator abstracts host-level network configuration so the
// reconciler can work with different datapaths (kernel netlink, grout/DPDK).
type DatapathConfigurator interface {
	conversion.DatapathConfigValidator
	conversion.InterfaceNameTranslator

	Configure(ctx context.Context, config interfacesConfiguration) error
	FilterL2Trunks([]v1alpha1.L2Trunk) ([]v1alpha1.L2Trunk, error)
}

func Reconcile(ctx context.Context, apiConfig conversion.APIConfigData, nodeIndex int, logLevel string,
	frrConfigPath, targetNamespace string, updater frr.ConfigUpdater,
	datapathConfigurator DatapathConfigurator, frrConfigurator frrConfiguratorType) error {
	normalizeConfig(&apiConfig)
	if err := conversion.ValidateUnderlays(apiConfig.Underlays); err != nil {
		return fmt.Errorf("failed to validate underlays: %w", err)
	}

	var resourceErrors []error
	var err error

	validationConfig := apiConfig
	validationConfig.L2Trunks = nil
	err = datapathConfigurator.Validate(validationConfig)
	resourceErrors = append(resourceErrors, err)

	if conversion.HasMissingSRv6ForL3VPNs(apiConfig.Underlays, apiConfig.L3VPNs) {
		validL3VPNs, validationErr := conversion.FilterValidL3VPNs(apiConfig.L3VPNs)
		resourceErrors = append(resourceErrors, validationErr,
			conversion.MissingSRv6ForL3VPNErrors(validL3VPNs, nil))
		apiConfig.L3VPNs = nil
	}

	validOverlays, err := conversion.FilterOverlayResources(conversion.OverlayResources{
		L3VNIs:        apiConfig.L3VNIs,
		L3VPNs:        apiConfig.L3VPNs,
		L2VNIs:        apiConfig.L2VNIs,
		L3Passthrough: apiConfig.L3Passthrough,
	})
	if openpeerrors.IsNonResourceError(err) {
		return err
	}
	resourceErrors = append(resourceErrors, err)

	validOverlays.L2VNIs, err = filterL2VNIsWithInvalidRoutingDomain(
		validOverlays.L2VNIs, validOverlays.L3VNIs, validOverlays.L3VPNs)
	resourceErrors = append(resourceErrors, err)

	validL2Trunks, err := conversion.FilterValidL2Trunks(apiConfig.L2Trunks)
	resourceErrors = append(resourceErrors, err)

	validL2Trunks, err = datapathConfigurator.FilterL2Trunks(validL2Trunks)
	resourceErrors = append(resourceErrors, err)

	validOverlays.L2VNIs, err = filterL2VNIsWithMissingTrunk(validOverlays.L2VNIs, validL2Trunks)
	resourceErrors = append(resourceErrors, err)

	config := conversion.APIConfigData{
		Underlays:     apiConfig.Underlays,
		L3VNIs:        validOverlays.L3VNIs,
		L3VPNs:        validOverlays.L3VPNs,
		L2VNIs:        validOverlays.L2VNIs,
		L2Trunks:      validL2Trunks,
		L3Passthrough: validOverlays.L3Passthrough,
		RawFRRConfigs: apiConfig.RawFRRConfigs,
		Passwords:     apiConfig.Passwords,
	}

	config = withoutUnusedEncapsulationSource(config)
	if err := conversion.ValidateRoutingIsolation(config); err != nil {
		return err
	}
	if err := conversion.ValidateEncapsulationSourceUsage(config); err != nil {
		return err
	}
	frrData := frrConfigData{
		configFile: frrConfigPath, updater: updater, APIConfigData: config,
		nodeIndex: nodeIndex, logLevel: logLevel, interfaceNameTranslator: datapathConfigurator,
	}
	if _, kernel := datapathConfigurator.(*KernelDatapathConfigurator); kernel && len(config.L3VPNs) > 0 {
		if err := stageHostIsolationBeforeFRR(ctx, targetNamespace, frrData, frrConfigurator); err != nil {
			return err
		}
	}

	// The FRR configuration must be applied before the datapath creates the kernel
	// objects it references. bgpd handles ZEBRA_VNI_ADD in bgp_evpn_local_vni_add(),
	// which dereferences the instance returned by bgp_get_evpn() without checking it
	// for NULL, so a VXLAN interface showing up while FRR has no EVPN instance
	// configured crashes bgpd. See https://github.com/FRRouting/frr/issues/22851.
	// Removals keep working in this order because ZEBRA_VNI_DEL does not touch the
	// EVPN instance, and FRR stops referencing the objects before they are deleted.
	if err = frrConfigurator(ctx, frrData); err != nil {
		return err
	}

	err = datapathConfigurator.Configure(ctx, interfacesConfiguration{
		targetNamespace: targetNamespace,
		APIConfigData:   config,
		nodeIndex:       nodeIndex,
	})
	if openpeerrors.IsNonResourceError(err) {
		return err
	}
	resourceErrors = append(resourceErrors, err)

	return errors.Join(resourceErrors...)
}

func stageHostIsolationBeforeFRR(ctx context.Context, targetNamespace string, data frrConfigData,
	configure frrConfiguratorType,
) error {
	config := data.APIConfigData
	nodeIndex := data.nodeIndex
	hostConfig, err := conversion.APItoHostConfig(nodeIndex, targetNamespace, config)
	if err != nil {
		return fmt.Errorf("convert SRv6 isolation plan: %w", err)
	}
	var source net.IP
	if len(config.Underlays) == 1 {
		source, err = remainingSRv6Source(config.Underlays[0].Spec.SRV6, hostConfig.Underlay.TunnelEndpoint)
		if err != nil {
			return err
		}
	}
	if err := stageIsolationBeforeFRR(targetNamespace, hostConfig, source, func(currentSource net.IP) error {
		return withdrawIsolationTableUsers(ctx, data, currentSource, configure)
	}); err != nil {
		return fmt.Errorf("prepare SRv6 isolation before FRR update: %w", err)
	}
	return nil
}

func stageIsolationBeforeFRR(targetNamespace string, hostConfig conversion.HostConfigData, source net.IP,
	withdraw func(net.IP) error,
) error {
	for {
		err := hostnetwork.StageSRv6Isolation(targetNamespace, hostConfig.L3VPNs, source,
			isolationTenantNames(hostConfig)...)
		if err == nil {
			return nil
		}
		var conflict *hostnetwork.VRFTableOwnershipError
		if !errors.As(err, &conflict) {
			return err
		}
		desired := map[string]bool{}
		for _, name := range isolationTenantNames(hostConfig) {
			desired[name] = true
		}
		for _, vpn := range hostConfig.L3VPNs {
			desired[vpn.VRF] = true
		}
		currentSource, err := hostnetwork.CheckObsoleteIsolationVRF(targetNamespace, desired, conflict)
		if err != nil {
			return err
		}
		if err := withdraw(currentSource); err != nil {
			return fmt.Errorf("withdraw obsolete table owner before cleanup: %w", err)
		}
		if err := hostnetwork.CleanupObsoleteIsolationVRF(targetNamespace, desired, conflict); err != nil {
			return err
		}
	}
}

// normalizeConfig sorts resources by namespace/name so validation order is deterministic.
func normalizeConfig(config *conversion.APIConfigData) {
	slices.SortFunc(config.L3VNIs, func(a, b v1alpha1.L3VNI) int {
		return cmp.Compare(objectKey(&a), objectKey(&b))
	})

	slices.SortFunc(config.L3VPNs, func(a, b v1alpha1.L3VPN) int {
		return cmp.Compare(objectKey(&a), objectKey(&b))
	})

	slices.SortFunc(config.L2VNIs, func(a, b v1alpha1.L2VNI) int {
		return cmp.Compare(objectKey(&a), objectKey(&b))
	})

	slices.SortFunc(config.L2Trunks, func(a, b v1alpha1.L2Trunk) int {
		return cmp.Compare(objectKey(&a), objectKey(&b))
	})

	slices.SortFunc(config.L3Passthrough, func(a, b v1alpha1.L3Passthrough) int {
		return cmp.Compare(objectKey(&a), objectKey(&b))
	})
}

func objectKey(o client.Object) string {
	return client.ObjectKeyFromObject(o).String()
}

// filterL2VNIsWithInvalidRoutingDomain must be called after all L3VNI/L3VPN filtering is complete.
func filterL2VNIsWithInvalidRoutingDomain(
	l2Vnis []v1alpha1.L2VNI,
	validL3VNIs []v1alpha1.L3VNI,
	validL3VPNs []v1alpha1.L3VPN,
) ([]v1alpha1.L2VNI, error) {
	if len(l2Vnis) == 0 {
		return l2Vnis, nil
	}

	validL3VNINames := sets.New[string]()
	for _, l3 := range validL3VNIs {
		validL3VNINames.Insert(l3.Name)
	}
	validL3VPNNames := sets.New[string]()
	for _, vpn := range validL3VPNs {
		validL3VPNNames.Insert(vpn.Name)
	}
	valid := make([]v1alpha1.L2VNI, 0, len(l2Vnis))
	var resourceErrors []error
	for _, l2 := range l2Vnis {
		if l2.Spec.RoutingDomain == nil {
			valid = append(valid, l2)
			continue
		}
		switch l2.Spec.RoutingDomain.Type {
		case v1alpha1.RoutingDomainTypeL3VNI:
			if l2.Spec.RoutingDomain.L3VNI != nil && !validL3VNINames.Has(l2.Spec.RoutingDomain.L3VNI.Name) {
				resourceErrors = append(resourceErrors, &openpeerrors.ResourceError{
					Obj: v1alpha1.FailedResource{
						Kind:    openpeerrors.KindL2VNI,
						Name:    l2.Name,
						Reason:  v1alpha1.FailedResourceReasonDependencyFailed,
						Message: fmt.Sprintf("referenced L3VNI %q not found", l2.Spec.RoutingDomain.L3VNI.Name),
					},
				})
				continue
			}
		case v1alpha1.RoutingDomainTypeL3VPN:
			if l2.Spec.RoutingDomain.L3VPN != nil && !validL3VPNNames.Has(l2.Spec.RoutingDomain.L3VPN.Name) {
				resourceErrors = append(resourceErrors, &openpeerrors.ResourceError{
					Obj: v1alpha1.FailedResource{
						Kind:    openpeerrors.KindL2VNI,
						Name:    l2.Name,
						Reason:  v1alpha1.FailedResourceReasonDependencyFailed,
						Message: fmt.Sprintf("referenced L3VPN %q not found", l2.Spec.RoutingDomain.L3VPN.Name),
					},
				})
				continue
			}
		}
		valid = append(valid, l2)
	}
	return valid, errors.Join(resourceErrors...)
}

// filterL2VNIsWithMissingTrunk drops the L2VNIs referencing an L2Trunk that
// does not apply to this node, or that failed validation. They stay in the
// API, but lose their data plane configuration until the trunk is available.
func filterL2VNIsWithMissingTrunk(l2Vnis []v1alpha1.L2VNI, validL2Trunks []v1alpha1.L2Trunk) ([]v1alpha1.L2VNI, error) {
	if len(l2Vnis) == 0 {
		return l2Vnis, nil
	}

	trunkNames := sets.New[string]()
	for _, trunk := range validL2Trunks {
		trunkNames.Insert(trunk.Name)
	}

	valid := make([]v1alpha1.L2VNI, 0, len(l2Vnis))
	var resourceErrors []error
	for _, l2 := range l2Vnis {
		if l2.Spec.Trunk == nil || trunkNames.Has(l2.Spec.Trunk.Name) {
			valid = append(valid, l2)
			continue
		}
		resourceErrors = append(resourceErrors, &openpeerrors.ResourceError{
			Obj: v1alpha1.FailedResource{
				Kind:    openpeerrors.KindL2VNI,
				Name:    l2.Name,
				Reason:  v1alpha1.FailedResourceReasonDependencyFailed,
				Message: fmt.Sprintf("referenced L2Trunk %q not found", l2.Spec.Trunk.Name),
			},
		})
	}
	return valid, errors.Join(resourceErrors...)
}

// All isolated VPNs share the conflicting underlay table. Withdraw that group
// before freeing the table; activate it only after staging succeeds. Legacy
// VPNs and independent EVPN domains remain in the intermediate configuration.
func withdrawIsolationTableUsers(ctx context.Context, data frrConfigData, source net.IP,
	configure frrConfiguratorType,
) error {
	withdrawn := map[string]bool{}
	var retained []v1alpha1.L3VPN
	for _, vpn := range data.L3VPNs {
		if vpn.Spec.RoutingIsolation != nil {
			withdrawn[vpn.Namespace+"/"+vpn.Name] = true
			continue
		}
		retained = append(retained, vpn)
	}
	data.L3VPNs = retained
	data.L2VNIs = slices.DeleteFunc(slices.Clone(data.L2VNIs), func(vni v1alpha1.L2VNI) bool {
		domain := vni.Spec.RoutingDomain
		if domain == nil || domain.Type != v1alpha1.RoutingDomainTypeL3VPN || domain.L3VPN == nil {
			return false
		}
		return withdrawn[vni.Namespace+"/"+domain.L3VPN.Name]
	})
	data.APIConfigData = withoutUnusedEncapsulationSource(data.APIConfigData)
	if source != nil && !source.IsUnspecified() {
		data.srv6EncapsulationSource = new(source.String())
	}
	return configure(ctx, data)
}

func withoutUnusedEncapsulationSource(config conversion.APIConfigData) conversion.APIConfigData {
	if slices.ContainsFunc(config.L3VPNs, func(vpn v1alpha1.L3VPN) bool { return vpn.Spec.RoutingIsolation != nil }) {
		return config
	}
	// Filtering or a preliminary withdrawal can remove the last isolated VPN.
	// Copy before clearing the effective source; the API objects stay intact.
	config.Underlays = slices.Clone(config.Underlays)
	for i := range config.Underlays {
		if config.Underlays[i].Spec.SRV6 == nil {
			continue
		}
		config.Underlays[i] = *config.Underlays[i].DeepCopy()
		config.Underlays[i].Spec.SRV6.EncapsulationSource = nil
	}
	return config
}
