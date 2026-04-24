// SPDX-License-Identifier:Apache-2.0

package routerconfiguration

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/openperouter/openperouter/api/static"
	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/internal/conversion"
	"github.com/openperouter/openperouter/internal/crdschema"
	"github.com/openperouter/openperouter/internal/staticconfiguration"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8sjson "k8s.io/apimachinery/pkg/util/json"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

var (
	underlayGVK      = schema.GroupVersionKind{Group: "openpe.openperouter.github.io", Version: "v1alpha1", Kind: "Underlay"}
	l3vniGVK         = schema.GroupVersionKind{Group: "openpe.openperouter.github.io", Version: "v1alpha1", Kind: "L3VNI"}
	l2vniGVK         = schema.GroupVersionKind{Group: "openpe.openperouter.github.io", Version: "v1alpha1", Kind: "L2VNI"}
	l3passthroughGVK = schema.GroupVersionKind{Group: "openpe.openperouter.github.io", Version: "v1alpha1", Kind: "L3Passthrough"}
	rawFRRConfigGVK  = schema.GroupVersionKind{Group: "openpe.openperouter.github.io", Version: "v1alpha1", Kind: "RawFRRConfig"}
)

func readStaticConfigs(configDir string) (conversion.APIConfigData, error) {
	routerConfigs, err := staticconfiguration.ReadRouterConfigs(configDir)
	if err != nil {
		return conversion.APIConfigData{}, fmt.Errorf("failed to read router configs: %w", err)
	}

	apiConfigs := make([]conversion.APIConfigData, len(routerConfigs))
	for i, rc := range routerConfigs {
		cfg, err := staticConfigToAPIConfig(rc)
		if err != nil {
			return conversion.APIConfigData{}, fmt.Errorf("failed to convert static config to API config: %w", err)
		}
		apiConfigs[i] = cfg
	}

	merged, err := conversion.MergeAPIConfigs(apiConfigs...)
	if err != nil {
		return conversion.APIConfigData{}, fmt.Errorf("failed to merge static configs: %w", err)
	}

	return merged, nil
}

func staticConfigToAPIConfig(staticConfig *static.PERouterConfig) (conversion.APIConfigData, error) {
	var allErrors field.ErrorList

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
		result, errs := applyDefaultsAndValidate(&underlays[i], underlayGVK)
		if len(errs) > 0 {
			allErrors = append(allErrors, errs...)
			continue
		}
		underlays[i] = *result
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
		result, errs := applyDefaultsAndValidate(&l3vnis[i], l3vniGVK)
		if len(errs) > 0 {
			allErrors = append(allErrors, errs...)
			continue
		}
		l3vnis[i] = *result
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
		result, errs := applyDefaultsAndValidate(&l2vnis[i], l2vniGVK)
		if len(errs) > 0 {
			allErrors = append(allErrors, errs...)
			continue
		}
		l2vnis[i] = *result
	}

	var l3passthrough []v1alpha1.L3Passthrough
	if staticConfig.BGPPassthrough.HostSession.ASN > 0 {
		pt := v1alpha1.L3Passthrough{
			TypeMeta: metav1.TypeMeta{
				Kind:       "L3Passthrough",
				APIVersion: "openpe.openperouter.github.io/v1alpha1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: "static-l3passthrough",
			},
			Spec: staticConfig.BGPPassthrough,
		}
		result, errs := applyDefaultsAndValidate(&pt, l3passthroughGVK)
		if len(errs) > 0 {
			allErrors = append(allErrors, errs...)
		}
		if result != nil {
			l3passthrough = []v1alpha1.L3Passthrough{*result}
		}
	}

	rawFRRConfigs := make([]v1alpha1.RawFRRConfig, len(staticConfig.RawFRRConfigs))
	for i, spec := range staticConfig.RawFRRConfigs {
		rawFRRConfigs[i] = v1alpha1.RawFRRConfig{
			TypeMeta: metav1.TypeMeta{
				Kind:       "RawFRRConfig",
				APIVersion: "openpe.openperouter.github.io/v1alpha1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: fmt.Sprintf("static-rawfrrconfig-%d", i),
			},
			Spec: spec,
		}
		result, errs := applyDefaultsAndValidate(&rawFRRConfigs[i], rawFRRConfigGVK)
		if len(errs) > 0 {
			allErrors = append(allErrors, errs...)
			continue
		}
		rawFRRConfigs[i] = *result
	}

	if len(allErrors) > 0 {
		return conversion.APIConfigData{}, fmt.Errorf("validation errors in static config: %v", allErrors.ToAggregate())
	}

	return conversion.APIConfigData{
		Underlays:     underlays,
		L3VNIs:        l3vnis,
		L2VNIs:        l2vnis,
		L3Passthrough: l3passthrough,
		RawFRRConfigs: rawFRRConfigs,
	}, nil
}

// applyDefaultsAndValidate converts a typed CRD object to unstructured, applies
// CRD-schema defaults, validates using CEL rules, and converts back.
func applyDefaultsAndValidate[T any](obj *T, gvk schema.GroupVersionKind) (*T, field.ErrorList) {
	rawMap, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, field.ErrorList{field.InternalError(field.NewPath(""), fmt.Errorf("converting to unstructured: %w", err))}
	}

	normalizedMap, err := normalizeGoTypes(rawMap)
	if err != nil {
		return nil, field.ErrorList{field.InternalError(field.NewPath(""), fmt.Errorf("normalizing Go types: %w", err))}
	}

	unstrObj := &unstructured.Unstructured{Object: normalizedMap}

	if err := crdschema.ApplyDefaults(unstrObj, gvk); err != nil {
		return nil, field.ErrorList{field.InternalError(field.NewPath(""), fmt.Errorf("applying defaults: %w", err))}
	}

	if valErrs := crdschema.Validate(context.Background(), unstrObj, gvk); len(valErrs) > 0 {
		return nil, valErrs
	}

	var result T
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstrObj.Object, &result); err != nil {
		return nil, field.ErrorList{field.InternalError(field.NewPath(""), fmt.Errorf("converting from unstructured: %w", err))}
	}

	return &result, nil
}

// normalizeGoTypes performs a JSON round-trip to normalize Go types (uint32 -> int64)
// so CEL validation works correctly. Without this, uint32 fields become uint64
// in the map, which CEL rejects. We use k8s.io/apimachinery/pkg/util/json
// which preserves integer types (unlike encoding/json which uses float64).
func normalizeGoTypes(m map[string]interface{}) (map[string]interface{}, error) {
	jsonData, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("marshalling to JSON: %w", err)
	}
	var normalized map[string]interface{}
	if err := k8sjson.Unmarshal(jsonData, &normalized); err != nil {
		return nil, fmt.Errorf("unmarshalling from JSON: %w", err)
	}
	return normalized, nil
}
