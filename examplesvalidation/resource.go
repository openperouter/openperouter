// SPDX-License-Identifier:Apache-2.0

package examplesvalidation

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

// validateResourceFromFile reads a YAML file and validates its resources by
// attempting to create them in a Kubernetes cluster. This delegates to
// ValidateResourceFromContent for the actual validation logic.
// Returns an error if the file cannot be read or if validation fails.
func validateResourceFromFile(k8sClient client.Client, filePath string) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}
	return validateResourceYAML(k8sClient, string(data))

}

// validateResourceYAML validates YAML content by attempting to create
// the resources in a Kubernetes cluster. It processes multi-document YAML
// (separated by ---), filters for OpenPERouter custom resources, and creates
// each resource using the Kubernetes API server for validation.
// Returns an error if YAML decoding fails, namespace is empty, or resource creation fails.
func validateResourceYAML(k8sClient client.Client, content string) error {

	documents := strings.SplitSeq(content, "---")

	for doc := range documents {
		doc = strings.TrimSpace(doc)
		if doc == "" {
			continue
		}

		obj := &unstructured.Unstructured{}
		decoder := yaml.NewYAMLOrJSONDecoder(strings.NewReader(doc), 4096)
		err := decoder.Decode(obj)
		if err != nil {
			return fmt.Errorf("failed to decode YAML: %w", err)
		}

		if !strings.Contains(obj.GetAPIVersion(), "openpe.openperouter.github.io") {
			continue
		}

		if obj.GetNamespace() == "" {
			return fmt.Errorf("empty namespace")
		}

		err = k8sClient.Create(context.Background(), obj)
		if err != nil {
			return fmt.Errorf("failed to create %s %s/%s: %w",
				obj.GetKind(), obj.GetNamespace(), obj.GetName(), err)
		}
	}
	return nil
}

// cleanupResources deletes all OpenPERouter custom resources in the specified namespace.
func cleanupResources(k8sClient client.Client, namespace string) error {
	ctx := context.Background()

	crdList := &apiextensionsv1.CustomResourceDefinitionList{}
	err := k8sClient.List(ctx, crdList)
	if err != nil {
		return err
	}

	for _, crd := range crdList.Items {
		if !containsOpenPERouterCR(crd.Spec.Group) {
			continue
		}

		kind := crd.Spec.Names.Kind

		if len(crd.Spec.Versions) == 0 {
			continue
		}
		version := crd.Spec.Versions[0].Name

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

		for _, item := range list.Items {
			err = k8sClient.Delete(ctx, &item)
			if err != nil {
				return err
			}
		}
	}
	return nil
}
