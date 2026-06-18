// SPDX-License-Identifier:Apache-2.0

package routerconfiguration

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"

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

	Configure(ctx context.Context, config interfacesConfiguration) error
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

	err = datapathConfigurator.Validate(apiConfig)
	resourceErrors = append(resourceErrors, err)

	var validL3VNIs []v1alpha1.L3VNI
	validL3VNIs, err = conversion.FilterValidL3VNIs(apiConfig.L3VNIs)
	resourceErrors = append(resourceErrors, err)

	var validL3VPNs []v1alpha1.L3VPN
	validL3VPNs, err = conversion.FilterValidL3VPNs(apiConfig.L3VPNs)
	resourceErrors = append(resourceErrors, err)

	if err := conversion.DetectMutuallyExclusiveOverlays(validL3VNIs, validL3VPNs); err != nil {
		validL3VNIs = []v1alpha1.L3VNI{}
		validL3VPNs = []v1alpha1.L3VPN{}
		resourceErrors = append(resourceErrors, err)
	}

	if conversion.HasMissingSRv6ForL3VPNs(apiConfig.Underlays, validL3VPNs) {
		resourceErrors = append(
			resourceErrors,
			conversion.MissingSRv6ForL3VPNErrors(validL3VPNs, nil),
		)
		validL3VPNs = []v1alpha1.L3VPN{}
	}

	var validL2VNIs []v1alpha1.L2VNI
	validL2VNIs, err = conversion.FilterValidL2VNIs(apiConfig.L2VNIs)
	resourceErrors = append(resourceErrors, err)

	var vnis map[int32]string
	validL3VNIs, vnis, err = conversion.FilterUniqueL3VNIs(validL3VNIs)
	resourceErrors = append(resourceErrors, err)

	var rdAssignedNumbers map[int32]string
	validL3VPNs, rdAssignedNumbers, err = conversion.FilterUniqueL3VPNs(validL3VPNs)
	resourceErrors = append(resourceErrors, err)
	// TODO: This is safe today, but may cause issues when we change to per-VRF mutual exclusivity
	// for L3VNI and L3VPN.
	maps.Copy(vnis, rdAssignedNumbers)

	validL2VNIs, err = conversion.FilterUniqueL2VNIs(validL2VNIs, vnis)
	resourceErrors = append(resourceErrors, err)

	validL3VNIs, err = conversion.FilterUniqueVRFsForL3VNIs(validL3VNIs)
	resourceErrors = append(resourceErrors, err)

	validL3VPNs, err = conversion.FilterUniqueVRFsForL3VPNs(validL3VPNs)
	resourceErrors = append(resourceErrors, err)

	validL2VNIs, err = filterL2VNIsWithInvalidRoutingDomain(validL2VNIs, validL3VNIs, validL3VPNs)
	resourceErrors = append(resourceErrors, err)

	validL3VNIs, validL3VPNs, validL2VNIs, err = conversion.FilterValidVRFSubnets(validL3VNIs, validL3VPNs, validL2VNIs)
	resourceErrors = append(resourceErrors, err)

	var validPassthrough []v1alpha1.L3Passthrough
	validPassthrough, err = conversion.FilterValidPassthroughs(apiConfig.L3Passthrough)
	resourceErrors = append(resourceErrors, err)

	if err := conversion.ValidateHostSessions(validL3VNIs, validPassthrough); err != nil {
		return fmt.Errorf("failed to validate host sessions: %w", err)
	}

	config := conversion.APIConfigData{
		Underlays:     apiConfig.Underlays,
		L3VNIs:        validL3VNIs,
		L3VPNs:        validL3VPNs,
		L2VNIs:        validL2VNIs,
		L3Passthrough: validPassthrough,
		RawFRRConfigs: apiConfig.RawFRRConfigs,
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

	if err = frrConfigurator(ctx, frrConfigData{
		configFile:    frrConfigPath,
		updater:       updater,
		APIConfigData: config,
		nodeIndex:     nodeIndex,
		logLevel:      logLevel,
	}); err != nil {
		return err
	}

	return errors.Join(resourceErrors...)
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

	slices.SortFunc(config.L3Passthrough, func(a, b v1alpha1.L3Passthrough) int {
		return cmp.Compare(objectKey(&a), objectKey(&b))
	})
}

func objectKey(o client.Object) string {
	return client.ObjectKeyFromObject(o).String()
}
