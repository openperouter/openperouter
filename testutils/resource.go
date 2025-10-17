/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package testutils

import (
	"context"
	"fmt"
	"os"
	"strings"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// containsOpenPERouterCR checks if YAML contains OpenPERouter custom resources
func containsOpenPERouterCR(content string) bool {
	return strings.Contains(content, "openpe.openperouter.github.io")
}

// ValidateResourceFromFile reads a YAML file and validates its resources by
// attempting to create them in a Kubernetes cluster. This delegates to
// ValidateResourceFromContent for the actual validation logic.
// Returns an error if the file cannot be read or if validation fails.
func ValidateResourceFromFile(k8sClient client.Client, filePath string, namespace string) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}
	return ValidateResourceFromContent(k8sClient, string(data), namespace)

}

// ValidateResourceFromContent validates YAML content by attempting to create
// the resources in a Kubernetes cluster. It processes multi-document YAML
// (separated by ---), filters for OpenPERouter custom resources, and creates
// each resource using the Kubernetes API server for validation.
// Returns an error if YAML decoding fails, namespace is empty, or resource creation fails.
func ValidateResourceFromContent(k8sClient client.Client, content string, namespace string) error {

	// Split YAML documents (separated by ---)
	documents := strings.SplitSeq(content, "---")

	for doc := range documents {
		doc = strings.TrimSpace(doc)
		if doc == "" {
			continue
		}

		// Decode YAML into Unstructured object for generic Kubernetes resource handling
		obj := &unstructured.Unstructured{}
		decoder := yaml.NewYAMLOrJSONDecoder(strings.NewReader(doc), 4096)
		err := decoder.Decode(obj)
		if err != nil {
			return fmt.Errorf("failed to decode YAML: %w", err)
		}

		// Skip non-OpenPERouter resources
		if !strings.Contains(obj.GetAPIVersion(), "openpe.openperouter.github.io") {
			continue
		}
		// Ensure the resource has a namespace specified
		if obj.GetNamespace() == "" {
			return fmt.Errorf("empty namespace")
		}

		// Create the resource - K8s API server will validate it against the CRD schema
		err = k8sClient.Create(context.Background(), obj)
		if err != nil {
			return fmt.Errorf("failed to create %s %s/%s: %w",
				obj.GetKind(), obj.GetNamespace(), obj.GetName(), err)
		}
	}
	return nil
}

// CleanupResources deletes all OpenPERouter custom resources in the specified namespace.
// It discovers all CRDs in the openpe.openperouter.github.io API group, then iterates
// through each CRD to list and delete all instances of that resource type in the namespace.
// This is useful for cleaning up test resources after test execution.
// Returns an error if CRD listing fails or if any resource deletion fails.
func CleanupResources(k8sClient client.Client, namespace string) error {
	ctx := context.Background()

	// Get all CRDs to discover OpenPERouter resource types
	crdList := &apiextensionsv1.CustomResourceDefinitionList{}
	err := k8sClient.List(ctx, crdList)
	if err != nil {
		return err
	}

	// Process each CRD in the openpe.openperouter.github.io group
	for _, crd := range crdList.Items {
		if !containsOpenPERouterCR(crd.Spec.Group) {
			continue
		}

		// Extract the resource kind from the CRD
		kind := crd.Spec.Names.Kind

		// Use the first stored version available for this CRD
		if len(crd.Spec.Versions) == 0 {
			continue
		}
		version := crd.Spec.Versions[0].Name

		// List all resources of this kind in the namespace
		list := &unstructured.UnstructuredList{}
		list.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   crd.Spec.Group,
			Version: version,
			Kind:    kind + "List",
		})

		err = k8sClient.List(ctx, list, client.InNamespace(namespace))
		if err != nil {
			return err
		}

		// Delete each resource instance
		for _, item := range list.Items {
			err = k8sClient.Delete(ctx, &item)
			if err != nil {
				return err
			}
		}
	}
	return nil
}
