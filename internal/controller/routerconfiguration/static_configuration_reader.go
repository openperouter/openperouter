// SPDX-License-Identifier:Apache-2.0

package routerconfiguration

import (
	"fmt"

	"github.com/openperouter/openperouter/api/static"
	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/internal/conversion"
	"github.com/openperouter/openperouter/internal/staticconfiguration"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func readStaticConfigs(configDir string) (conversion.ApiConfigData, error) {
	routerConfigs, err := staticconfiguration.ReadRouterConfigs(configDir)
	if err != nil {
		return conversion.ApiConfigData{}, fmt.Errorf("failed to read router configs: %w", err)
	}

	apiConfigs := make([]conversion.ApiConfigData, len(routerConfigs))
	for i, rc := range routerConfigs {
		apiConfigs[i] = staticConfigToAPIConfig(rc)
	}

	merged, err := conversion.MergeAPIConfigs(apiConfigs...)
	if err != nil {
		return conversion.ApiConfigData{}, fmt.Errorf("failed to merge static configs: %w", err)
	}

	return merged, nil
}

func staticConfigToAPIConfig(staticConfig *static.PERouterConfig) conversion.ApiConfigData {
	underlays := make([]v1alpha1.Underlay, len(staticConfig.Underlays))
	for i, spec := range staticConfig.Underlays {
		underlays[i] = v1alpha1.Underlay{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Underlay",
				APIVersion: "openpe.openperouter.github.io/v1alpha1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: fmt.Sprintf("static-underlay-%d", i),
			},
			Spec: spec,
		}
	}

	l3vnis := make([]v1alpha1.L3VNI, len(staticConfig.L3VNIs))
	for i, spec := range staticConfig.L3VNIs {
		l3vnis[i] = v1alpha1.L3VNI{
			TypeMeta: metav1.TypeMeta{
				Kind:       "L3VNI",
				APIVersion: "openpe.openperouter.github.io/v1alpha1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: fmt.Sprintf("static-l3vni-%d", i),
			},
			Spec: spec,
		}
	}

	l2vnis := make([]v1alpha1.L2VNI, len(staticConfig.L2VNIs))
	for i, spec := range staticConfig.L2VNIs {
		l2vnis[i] = v1alpha1.L2VNI{
			TypeMeta: metav1.TypeMeta{
				Kind:       "L2VNI",
				APIVersion: "openpe.openperouter.github.io/v1alpha1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: fmt.Sprintf("static-l2vni-%d", i),
			},
			Spec: spec,
		}
	}

	var l3passthrough []v1alpha1.L3Passthrough
	if staticConfig.BGPPassthrough.HostSession.ASN > 0 {
		l3passthrough = []v1alpha1.L3Passthrough{
			{
				TypeMeta: metav1.TypeMeta{
					Kind:       "L3Passthrough",
					APIVersion: "openpe.openperouter.github.io/v1alpha1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "static-l3passthrough",
				},
				Spec: staticConfig.BGPPassthrough,
			},
		}
	}

	return conversion.ApiConfigData{
		Underlays:     underlays,
		L3VNIs:        l3vnis,
		L2VNIs:        l2vnis,
		L3Passthrough: l3passthrough,
	}
}
