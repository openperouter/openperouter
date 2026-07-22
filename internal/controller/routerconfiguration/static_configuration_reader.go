// SPDX-License-Identifier:Apache-2.0

package routerconfiguration

import (
	"context"
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
	underlayGVK      = schema.GroupVersionKind{Group: "network.openperouter.io", Version: "v1alpha1", Kind: "Underlay"}
	l3vniGVK         = schema.GroupVersionKind{Group: "network.openperouter.io", Version: "v1alpha1", Kind: "L3VNI"}
	l2vniGVK         = schema.GroupVersionKind{Group: "network.openperouter.io", Version: "v1alpha1", Kind: "L2VNI"}
	l3vpnGVK         = schema.GroupVersionKind{Group: "network.openperouter.io", Version: "v1alpha1", Kind: "L3VPN"}
	l3passthroughGVK = schema.GroupVersionKind{Group: "network.openperouter.io", Version: "v1alpha1", Kind: "L3Passthrough"}
	rawFRRConfigGVK  = schema.GroupVersionKind{Group: "network.openperouter.io", Version: "v1alpha1", Kind: "RawFRRConfig"}
)

const (
	// StaticSourceLabel is the label key used to identify resources mirrored from static config.
	StaticSourceLabel = "openperouter.github.io/source"
	// StaticSourceValue is the label value for resources mirrored from static config.
	StaticSourceValue = "static"
	// StaticNodeLabel is the label key for the node name that owns static resources.
	StaticNodeLabel = "openperouter.github.io/static-node"
)

func readStaticConfigs(configDir, nodeName, namespace string) (conversion.APIConfigData, error) {
	routerConfigs, err := staticconfiguration.ReadRouterConfigs(configDir)
	if err != nil {
		return conversion.APIConfigData{}, fmt.Errorf("failed to read router configs: %w", err)
	}

	apiConfigs := make([]conversion.APIConfigData, len(routerConfigs))
	for i, rc := range routerConfigs {
		cfg, err := staticConfigToAPIConfig(rc, nodeName, namespace)
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

func staticConfigToAPIConfig(staticConfig *static.PERouterConfig, nodeName, namespace string) (conversion.APIConfigData, error) {
	var allErrors field.ErrorList

	nodeSelector := &metav1.LabelSelector{
		MatchLabels: map[string]string{
			"kubernetes.io/hostname": nodeName,
		},
	}

	underlays, underlayErrs := staticUnderlaysToAPI(staticConfig.Underlays, nodeName, namespace, nodeSelector)
	allErrors = append(allErrors, underlayErrs...)

	staticName := func(name string) string {
		return fmt.Sprintf("static-%s-%s", nodeName, name)
	}

	l3vnis := make([]v1alpha1.L3VNI, len(staticConfig.L3VNIs))
	for i, staticL3 := range staticConfig.L3VNIs {
		spec := staticL3.L3VNISpec
		spec.NodeSelector = nodeSelector
		l3vnis[i] = v1alpha1.L3VNI{
			TypeMeta: metav1.TypeMeta{
				Kind:       "L3VNI",
				APIVersion: "network.openperouter.io/v1alpha1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      staticName(staticL3.Name),
				Namespace: namespace,
				Labels: map[string]string{
					StaticSourceLabel: StaticSourceValue,
					StaticNodeLabel:   nodeName,
				},
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
	for i, staticL2 := range staticConfig.L2VNIs {
		spec := staticL2.L2VNISpec
		spec.NodeSelector = nodeSelector
		if spec.RoutingDomain != nil {
			if spec.RoutingDomain.L3VNI != nil {
				spec.RoutingDomain.L3VNI.Name = staticName(spec.RoutingDomain.L3VNI.Name)
			}
			if spec.RoutingDomain.L3VPN != nil {
				spec.RoutingDomain.L3VPN.Name = staticName(spec.RoutingDomain.L3VPN.Name)
			}
		}
		l2vnis[i] = v1alpha1.L2VNI{
			TypeMeta: metav1.TypeMeta{
				Kind:       "L2VNI",
				APIVersion: "network.openperouter.io/v1alpha1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      staticName(staticL2.Name),
				Namespace: namespace,
				Labels: map[string]string{
					StaticSourceLabel: StaticSourceValue,
					StaticNodeLabel:   nodeName,
				},
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

	l3vpns := make([]v1alpha1.L3VPN, len(staticConfig.L3VPNs))
	for i, staticVPN := range staticConfig.L3VPNs {
		l3vpns[i] = v1alpha1.L3VPN{
			TypeMeta: metav1.TypeMeta{
				Kind:       "L3VPN",
				APIVersion: "network.openperouter.io/v1alpha1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      staticName(staticVPN.Name),
				Namespace: namespace,
				Labels: map[string]string{
					StaticSourceLabel: StaticSourceValue,
					StaticNodeLabel:   nodeName,
				},
			},
			Spec: staticVPN.L3VPNSpec,
		}
		result, errs := applyDefaultsAndValidate(&l3vpns[i], l3vpnGVK)
		if len(errs) > 0 {
			allErrors = append(allErrors, errs...)
			continue
		}
		l3vpns[i] = *result
	}

	var l3passthrough []v1alpha1.L3Passthrough
	if staticConfig.BGPPassthrough.HostSession.ASN > 0 {
		passthroughSpec := staticConfig.BGPPassthrough
		passthroughSpec.NodeSelector = nodeSelector
		pt := v1alpha1.L3Passthrough{
			TypeMeta: metav1.TypeMeta{
				Kind:       "L3Passthrough",
				APIVersion: "network.openperouter.io/v1alpha1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("static-%s-l3passthrough", nodeName),
				Namespace: namespace,
				Labels: map[string]string{
					StaticSourceLabel: StaticSourceValue,
					StaticNodeLabel:   nodeName,
				},
			},
			Spec: passthroughSpec,
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
		spec.NodeSelector = nodeSelector
		rawFRRConfigs[i] = v1alpha1.RawFRRConfig{
			TypeMeta: metav1.TypeMeta{
				Kind:       "RawFRRConfig",
				APIVersion: "network.openperouter.io/v1alpha1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("static-%s-rawfrrconfig-%d", nodeName, i),
				Namespace: namespace,
				Labels: map[string]string{
					StaticSourceLabel: StaticSourceValue,
					StaticNodeLabel:   nodeName,
				},
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
		L3VPNs:        l3vpns,
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
func normalizeGoTypes(m map[string]any) (map[string]any, error) {
	jsonData, err := k8sjson.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("marshalling to JSON: %w", err)
	}
	var normalized map[string]any
	if err := k8sjson.Unmarshal(jsonData, &normalized); err != nil {
		return nil, fmt.Errorf("unmarshalling from JSON: %w", err)
	}
	return normalized, nil
}

func staticUnderlaysToAPI(
	staticUnderlays []static.StaticUnderlaySpec,
	nodeName, namespace string,
	nodeSelector *metav1.LabelSelector,
) ([]v1alpha1.Underlay, field.ErrorList) {
	var allErrors field.ErrorList
	underlays := make([]v1alpha1.Underlay, len(staticUnderlays))
	for i, staticUnderlay := range staticUnderlays {
		neighborsPath := field.NewPath("underlays").Index(i).Child("neighbors")
		if errs := validateStaticNeighbors(staticUnderlay.Neighbors, neighborsPath); len(errs) > 0 {
			allErrors = append(allErrors, errs...)
			continue
		}
		spec := staticUnderlay.UnderlaySpec
		spec.NodeSelector = nodeSelector
		spec.Neighbors = make([]v1alpha1.Neighbor, len(staticUnderlay.Neighbors))
		for j, sn := range staticUnderlay.Neighbors {
			spec.Neighbors[j] = sn.Neighbor
		}
		underlays[i] = v1alpha1.Underlay{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Underlay",
				APIVersion: "network.openperouter.io/v1alpha1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("static-%s-underlay-%d", nodeName, i),
				Namespace: namespace,
				Labels: map[string]string{
					StaticSourceLabel: StaticSourceValue,
					StaticNodeLabel:   nodeName,
				},
			},
			Spec: spec,
		}
		result, errs := applyDefaultsAndValidate(&underlays[i], underlayGVK)
		if len(errs) > 0 {
			allErrors = append(allErrors, errs...)
			continue
		}
		restoreStaticPasswords(result.Spec.Neighbors, staticUnderlay.Neighbors)
		underlays[i] = *result
	}
	return underlays, allErrors
}

// restoreStaticPasswords re-injects passwords from the static config into the
// validated neighbors. The CRD round-trip strips Password (json:"-"), so we
// match by address/interface identity rather than relying on index stability.
func restoreStaticPasswords(neighbors []v1alpha1.Neighbor, staticNeighbors []static.StaticNeighbor) {
	byKey := make(map[string]*string, len(staticNeighbors))
	for _, sn := range staticNeighbors {
		byKey[neighborKey(&sn.Neighbor)] = sn.Password
	}
	for i := range neighbors {
		if pw, ok := byKey[neighborKey(&neighbors[i])]; ok {
			neighbors[i].Password = pw
		}
	}
}

func validateStaticNeighbors(staticNeighbors []static.StaticNeighbor, basePath *field.Path) field.ErrorList {
	seen := make(map[string]struct{}, len(staticNeighbors))
	for i := range staticNeighbors {
		sn := &staticNeighbors[i]
		p := basePath.Index(i)
		key := neighborKey(&sn.Neighbor)
		if key == "" {
			return field.ErrorList{field.Invalid(p, nil, "neighbor has neither address nor interface")}
		}
		if _, dup := seen[key]; dup {
			return field.ErrorList{field.Invalid(p, key, "duplicate neighbor")}
		}
		seen[key] = struct{}{}
		if sn.Password != nil {
			if err := validatePassword(*sn.Password); err != nil {
				return field.ErrorList{field.Invalid(
					p.Child("password"), nil,
					fmt.Sprintf("static config password for neighbor %s: %s", neighborAddr(&sn.Neighbor), err),
				)}
			}
			sn.PasswordSecret = nil
		}
	}
	return nil
}

const defaultBGPPort = 179

func neighborKey(n *v1alpha1.Neighbor) string {
	var key string
	switch {
	case n.Address != nil:
		key = "addr:" + *n.Address
	case n.Interface != nil:
		key = "iface:" + *n.Interface
	default:
		return ""
	}
	port := defaultBGPPort
	if n.Port != nil {
		port = int(*n.Port)
	}
	return fmt.Sprintf("%s:%d", key, port)
}
