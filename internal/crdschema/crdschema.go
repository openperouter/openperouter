// SPDX-License-Identifier:Apache-2.0

// Package crdschema provides CRD-schema-based defaulting and CEL validation
// for static configuration objects. It embeds the generated CRD manifests and
// uses them as the single source of truth for defaults and validation rules,
// matching the behavior of the Kubernetes API server.
package crdschema

import (
	"context"
	"fmt"
	"io/fs"
	"strings"

	apiextensions "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	structuralschema "k8s.io/apiextensions-apiserver/pkg/apiserver/schema"
	celvalidation "k8s.io/apiextensions-apiserver/pkg/apiserver/schema/cel"
	structuraldefaulting "k8s.io/apiextensions-apiserver/pkg/apiserver/schema/defaulting"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	celconfig "k8s.io/apiserver/pkg/apis/cel"
	k8syaml "sigs.k8s.io/yaml"

	crd "github.com/openperouter/openperouter/config/crd"
)

var (
	schemas    map[schema.GroupVersionKind]*structuralschema.Structural
	validators map[schema.GroupVersionKind]*celvalidation.Validator
)

func init() {
	schemas = make(map[schema.GroupVersionKind]*structuralschema.Structural)
	validators = make(map[schema.GroupVersionKind]*celvalidation.Validator)
	if err := loadCRDs(); err != nil {
		panic(fmt.Sprintf("failed to load CRD schemas: %v", err))
	}
}

func loadCRDs() error {
	entries, err := fs.ReadDir(crd.Manifests, "bases")
	if err != nil {
		return fmt.Errorf("reading embedded CRD manifests: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}

		data, err := fs.ReadFile(crd.Manifests, "bases/"+entry.Name())
		if err != nil {
			return fmt.Errorf("reading CRD manifest %s: %w", entry.Name(), err)
		}

		if err := parseCRD(data); err != nil {
			return fmt.Errorf("parsing CRD manifest %s: %w", entry.Name(), err)
		}
	}

	return nil
}

func parseCRD(data []byte) error {
	// Parse as v1 CRD
	var v1CRD apiextensionsv1.CustomResourceDefinition
	if err := k8syaml.Unmarshal(data, &v1CRD); err != nil {
		return fmt.Errorf("unmarshalling CRD: %w", err)
	}

	// Iterate over v1 versions and convert each schema individually.
	// Converting the entire CRD from v1 to internal drops per-version schemas,
	// so we convert each version's JSONSchemaProps directly.
	for _, ver := range v1CRD.Spec.Versions {
		if ver.Schema == nil || ver.Schema.OpenAPIV3Schema == nil {
			continue
		}

		gvk := schema.GroupVersionKind{
			Group:   v1CRD.Spec.Group,
			Version: ver.Name,
			Kind:    v1CRD.Spec.Names.Kind,
		}

		var internalSchema apiextensions.JSONSchemaProps
		if err := apiextensionsv1.Convert_v1_JSONSchemaProps_To_apiextensions_JSONSchemaProps(
			ver.Schema.OpenAPIV3Schema, &internalSchema, nil); err != nil {
			return fmt.Errorf("converting schema for %s: %w", gvk, err)
		}

		structural, err := structuralschema.NewStructural(&internalSchema)
		if err != nil {
			return fmt.Errorf("building structural schema for %s: %w", gvk, err)
		}

		schemas[gvk] = structural

		// Build CEL validator (nil if no validation rules exist)
		validator := celvalidation.NewValidator(structural, true, celconfig.PerCallLimit)
		if validator != nil {
			validators[gvk] = validator
		}
	}

	return nil
}

// ApplyDefaults applies CRD-schema-defined default values to the given
// unstructured object. The defaults are derived from the kubebuilder:default
// annotations in api/v1alpha1/ via the generated CRD manifests.
func ApplyDefaults(obj *unstructured.Unstructured, gvk schema.GroupVersionKind) error {
	structural, ok := schemas[gvk]
	if !ok {
		return fmt.Errorf("no CRD schema found for %s", gvk)
	}

	structuraldefaulting.Default(obj.Object, structural)
	return nil
}

// Validate runs CEL validation rules from the CRD schema against the given
// unstructured object. Rules that use oldSelf (immutability constraints) are
// automatically skipped because oldObj is nil (static config is always a fresh
// load, not an update).
func Validate(ctx context.Context, obj *unstructured.Unstructured, gvk schema.GroupVersionKind) field.ErrorList {
	validator, ok := validators[gvk]
	if !ok {
		// No validator means no CEL rules for this type — that's fine
		return nil
	}

	// Pass nil for oldObj so transition rules (oldSelf) are skipped.
	errs, _ := validator.Validate(ctx, field.NewPath(""), schemas[gvk], obj.Object, nil, celconfig.RuntimeCELCostBudget)
	return errs
}

// KnownGVKs returns all GroupVersionKinds that have loaded CRD schemas.
// Useful for testing that all expected CRDs were embedded and parsed.
func KnownGVKs() []schema.GroupVersionKind {
	gvks := make([]schema.GroupVersionKind, 0, len(schemas))
	for gvk := range schemas {
		gvks = append(gvks, gvk)
	}
	return gvks
}
