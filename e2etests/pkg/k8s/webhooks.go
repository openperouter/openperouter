// SPDX-License-Identifier:Apache-2.0

package k8s

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clientset "k8s.io/client-go/kubernetes"
)

const skipWebhookLabel = "openperouter.io/skip-webhook"

func FindOpenPEWebhookConfigurations(cs clientset.Interface) ([]string, error) {
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

	names, err := FindOpenPEWebhookConfigurations(cs)
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
	labelPatch, _ := json.Marshal([]map[string]interface{}{
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

	names, err := FindOpenPEWebhookConfigurations(cs)
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

func jsonPatchEscape(s string) string {
	replacer := strings.NewReplacer("~", "~0", "/", "~1")
	return replacer.Replace(s)
}
