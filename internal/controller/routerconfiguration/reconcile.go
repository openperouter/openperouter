// SPDX-License-Identifier:Apache-2.0

package routerconfiguration

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/internal/conversion"
	openpeerrors "github.com/openperouter/openperouter/internal/errors"
	"github.com/openperouter/openperouter/internal/frr"
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
		L2Trunks:      apiConfig.L2Trunks,
		L3Passthrough: apiConfig.L3Passthrough,
	})
	if openpeerrors.IsNonResourceError(err) {
		return err
	}
	resourceErrors = append(resourceErrors, err)

	validOverlays.L2VNIs, err = filterL2VNIsWithInvalidRoutingDomain(
		validOverlays.L2VNIs, validOverlays.L3VNIs, validOverlays.L3VPNs)
	resourceErrors = append(resourceErrors, err)

	validL2Trunks := validOverlays.L2Trunks
	validL2Trunks, err = datapathConfigurator.FilterL2Trunks(validL2Trunks)
	resourceErrors = append(resourceErrors, err)

	validOverlays.L2VNIs, err = filterL2VNIsWithMissingTrunk(
		validOverlays.L2VNIs, validL2Trunks)
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

	// The FRR configuration must be applied before the datapath creates the kernel
	// objects it references. bgpd handles ZEBRA_VNI_ADD in bgp_evpn_local_vni_add(),
	// which dereferences the instance returned by bgp_get_evpn() without checking it
	// for NULL, so a VXLAN interface showing up while FRR has no EVPN instance
	// configured crashes bgpd. See https://github.com/FRRouting/frr/issues/22851.
	// Removals keep working in this order because ZEBRA_VNI_DEL does not touch the
	// EVPN instance, and FRR stops referencing the objects before they are deleted.
	if err = frrConfigurator(ctx, frrConfigData{
		configFile:              frrConfigPath,
		updater:                 updater,
		APIConfigData:           config,
		nodeIndex:               nodeIndex,
		logLevel:                logLevel,
		interfaceNameTranslator: datapathConfigurator,
	}); err != nil {
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

// missingTrunkMessagePrefix starts the message of an L2VNI dependency failure
// caused by an L2Trunk that is absent for this node.
const missingTrunkMessagePrefix = "referenced L2Trunk "

// filterL2VNIsWithMissingTrunk reports and removes L2VNIs whose referenced
// L2Trunk was filtered out or does not exist for this node. Resources may be
// created in any order, so this is a reconciliation concern, not a filter of
// the resource set.
func filterL2VNIsWithMissingTrunk(
	l2Vnis []v1alpha1.L2VNI,
	l2Trunks []v1alpha1.L2Trunk,
) ([]v1alpha1.L2VNI, error) {
	validTrunks := sets.New[types.NamespacedName]()
	for _, trunk := range l2Trunks {
		validTrunks.Insert(types.NamespacedName{Namespace: trunk.Namespace, Name: trunk.Name})
	}

	return filterL2VNIsByDependency(l2Vnis, func(l2 v1alpha1.L2VNI) error {
		if l2.Spec.Trunk == nil {
			return nil
		}
		key := types.NamespacedName{Namespace: l2.Namespace, Name: l2.Spec.Trunk.Name}
		if validTrunks.Has(key) {
			return nil
		}
		return missingDependencyError(l2.Name, openpeerrors.KindL2Trunk, l2.Spec.Trunk.Name)
	})
}

// filterL2VNIsWithInvalidRoutingDomain must be called after all L3VNI/L3VPN filtering is complete.
func filterL2VNIsWithInvalidRoutingDomain(
	l2Vnis []v1alpha1.L2VNI,
	validL3VNIs []v1alpha1.L3VNI,
	validL3VPNs []v1alpha1.L3VPN,
) ([]v1alpha1.L2VNI, error) {
	validL3VNINames := sets.New[string]()
	for _, l3 := range validL3VNIs {
		validL3VNINames.Insert(l3.Name)
	}
	validL3VPNNames := sets.New[string]()
	for _, vpn := range validL3VPNs {
		validL3VPNNames.Insert(vpn.Name)
	}

	return filterL2VNIsByDependency(l2Vnis, func(l2 v1alpha1.L2VNI) error {
		if l2.Spec.RoutingDomain == nil {
			return nil
		}
		switch l2.Spec.RoutingDomain.Type {
		case v1alpha1.RoutingDomainTypeL3VNI:
			if l2.Spec.RoutingDomain.L3VNI != nil && !validL3VNINames.Has(l2.Spec.RoutingDomain.L3VNI.Name) {
				return missingDependencyError(l2.Name, openpeerrors.KindL3VNI, l2.Spec.RoutingDomain.L3VNI.Name)
			}
		case v1alpha1.RoutingDomainTypeL3VPN:
			if l2.Spec.RoutingDomain.L3VPN != nil && !validL3VPNNames.Has(l2.Spec.RoutingDomain.L3VPN.Name) {
				return missingDependencyError(l2.Name, openpeerrors.KindL3VPN, l2.Spec.RoutingDomain.L3VPN.Name)
			}
		}
		return nil
	})
}

// filterL2VNIsByDependency keeps the L2VNIs whose dependency check passes and
// reports one dependency failure per rejected L2VNI.
func filterL2VNIsByDependency(
	l2Vnis []v1alpha1.L2VNI,
	dependencyError func(v1alpha1.L2VNI) error,
) ([]v1alpha1.L2VNI, error) {
	if len(l2Vnis) == 0 {
		return l2Vnis, nil
	}
	valid := make([]v1alpha1.L2VNI, 0, len(l2Vnis))
	var resourceErrors []error
	for _, l2 := range l2Vnis {
		if err := dependencyError(l2); err != nil {
			resourceErrors = append(resourceErrors, err)
			continue
		}
		valid = append(valid, l2)
	}
	return valid, errors.Join(resourceErrors...)
}

func missingDependencyError(l2vniName string, kind v1alpha1.FailedResourceKind, dependencyName string) error {
	return &openpeerrors.ResourceError{
		Obj: v1alpha1.FailedResource{
			Kind:    openpeerrors.KindL2VNI,
			Name:    l2vniName,
			Reason:  v1alpha1.FailedResourceReasonDependencyFailed,
			Message: fmt.Sprintf("referenced %s %q not found", kind, dependencyName),
		},
	}
}
