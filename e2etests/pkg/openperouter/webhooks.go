// SPDX-License-Identifier:Apache-2.0

package openperouter

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clientset "k8s.io/client-go/kubernetes"
)

const (
	skipWebhookLabel = "openperouter.io/skip-webhook"

	WebhookLabelSelector        = "app=webhook"
	NodeMarkerLabelSelector     = "app=nodemarker"
	CertControllerLabelSelector = "app=certcontroller"
)

// WebhookDeployment returns the deployment serving the admission webhooks.
// Its name depends on how OpenPERouter was installed, so it is looked up by
// the labels of its pods.
func WebhookDeployment(cs clientset.Interface) (*appsv1.Deployment, error) {
	deployments, err := cs.AppsV1().Deployments(Namespace).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing deployments in %s: %w", Namespace, err)
	}
	for i := range deployments.Items {
		if deployments.Items[i].Spec.Template.Labels["app"] == "webhook" {
			return &deployments.Items[i], nil
		}
	}
	return nil, fmt.Errorf("no webhook deployment found in %s", Namespace)
}

func DisableWebhooksForNamespace(cs clientset.Interface, namespace string) error {
	_, err := cs.CoreV1().Namespaces().Patch(
		context.Background(),
		namespace,
		types.MergePatchType,
		[]byte(`{"metadata":{"labels":{"`+skipWebhookLabel+`":""}}}`),
		metav1.PatchOptions{},
	)
	if err != nil {
		return err
	}

	names, err := findWebhookConfigurations(cs)
	if err != nil {
		return err
	}
	for _, name := range names {
		vwc, err := cs.AdmissionregistrationV1().ValidatingWebhookConfigurations().Get(
			context.Background(), name, metav1.GetOptions{})
		if err != nil {
			return err
		}

		for i := range vwc.Webhooks {
			vwc.Webhooks[i].NamespaceSelector = &metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{
						Key:      skipWebhookLabel,
						Operator: metav1.LabelSelectorOpDoesNotExist,
					},
				},
			}
		}

		_, err = cs.AdmissionregistrationV1().ValidatingWebhookConfigurations().Update(
			context.Background(), vwc, metav1.UpdateOptions{})
		if err != nil {
			return err
		}
	}
	return nil
}

func RestoreWebhooks(cs clientset.Interface, namespace string) error {
	labelPatch, _ := json.Marshal([]map[string]any{
		{
			"op":   "remove",
			"path": "/metadata/labels/" + jsonPatchEscape(skipWebhookLabel),
		},
	})
	_, err := cs.CoreV1().Namespaces().Patch(
		context.Background(),
		namespace,
		types.JSONPatchType,
		labelPatch,
		metav1.PatchOptions{},
	)
	if err != nil {
		return err
	}

	names, err := findWebhookConfigurations(cs)
	if err != nil {
		return err
	}
	for _, name := range names {
		vwc, err := cs.AdmissionregistrationV1().ValidatingWebhookConfigurations().Get(
			context.Background(), name, metav1.GetOptions{})
		if err != nil {
			return err
		}

		for i := range vwc.Webhooks {
			vwc.Webhooks[i].NamespaceSelector = nil
		}

		_, err = cs.AdmissionregistrationV1().ValidatingWebhookConfigurations().Update(
			context.Background(), vwc, metav1.UpdateOptions{})
		if err != nil {
			return err
		}
	}
	return nil
}

func findWebhookConfigurations(cs clientset.Interface) ([]string, error) {
	vwcList, err := cs.AdmissionregistrationV1().ValidatingWebhookConfigurations().List(
		context.Background(), metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	var names []string
	for _, vwc := range vwcList.Items {
		for _, wh := range vwc.Webhooks {
			if strings.Contains(wh.Name, "openperouter") {
				names = append(names, vwc.Name)
				break
			}
		}
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("no ValidatingWebhookConfiguration found with openperouter webhooks")
	}
	return names, nil
}

func jsonPatchEscape(s string) string {
	replacer := strings.NewReplacer("~", "~0", "/", "~1")
	return replacer.Replace(s)
}
