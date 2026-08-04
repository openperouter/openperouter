// SPDX-License-Identifier:Apache-2.0

package celvalidation

import (
	"context"
	"fmt"
	"strings"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// containsOpenPERouterCR checks if YAML contains OpenPERouter custom resources
func containsOpenPERouterCR(content string) bool {
	return strings.Contains(content, "openpe.openperouter.github.io")
}

// createObjects creates the provided objects.
func createObjects(k8sClient client.Client, objects ...unstructured.Unstructured) error {
	for _, obj := range objects {
		if obj.GetNamespace() == "" {
			return fmt.Errorf("empty namespace")
		}

		err := k8sClient.Create(context.Background(), &obj, &client.CreateOptions{FieldValidation: "Strict"})
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
