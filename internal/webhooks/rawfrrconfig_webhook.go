// SPDX-License-Identifier:Apache-2.0

package webhooks

import (
	"context"
	"fmt"
	"net/http"

	v1 "k8s.io/api/admission/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/openperouter/openperouter/api/v1alpha1"
)

const (
	rawFRRConfigValidationWebhookPath = "/validate-openperouter-io-v1alpha1-rawfrrconfig"
)

type RawFRRConfigValidator struct {
	decoder           admission.Decoder
	operatorNamespace string
}

func SetupRawFRRConfig(mgr ctrl.Manager, operatorNamespace string) error {
	validator := &RawFRRConfigValidator{
		decoder:           admission.NewDecoder(mgr.GetScheme()),
		operatorNamespace: operatorNamespace,
	}

	mgr.GetWebhookServer().Register(
		rawFRRConfigValidationWebhookPath,
		&webhook.Admission{Handler: validator})

	if _, err := mgr.GetCache().GetInformer(context.Background(), &v1alpha1.RawFRRConfig{}); err != nil {
		return fmt.Errorf("failed to get informer for RawFRRConfig: %w", err)
	}
	return nil
}

func (v *RawFRRConfigValidator) Handle(_ context.Context, req admission.Request) admission.Response {
	var rawFRRConfig v1alpha1.RawFRRConfig
	if req.Operation == v1.Delete {
		return admission.Allowed("")
	}

	if err := v.decoder.Decode(req, &rawFRRConfig); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	switch req.Operation {
	case v1.Create, v1.Update:
		if err := validateRawFRRConfig(&rawFRRConfig, v.operatorNamespace); err != nil {
			return admission.Denied(err.Error())
		}
	}
	return admission.Allowed("").WithWarnings("please note RawFRRConfig is for experimentation only and not supported")
}

func validateRawFRRConfig(rawFRRConfig *v1alpha1.RawFRRConfig, operatorNamespace string) error {
	Logger.Debug("webhook rawfrrconfig", "action", "validate", "name", rawFRRConfig.Name, "namespace", rawFRRConfig.Namespace)

	if operatorNamespace == "" {
		return fmt.Errorf("operator namespace is not configured; cannot validate RawFRRConfig")
	}
	if rawFRRConfig.Namespace != operatorNamespace {
		return fmt.Errorf("RawFRRConfig must be created in the operator namespace %q, got %q",
			operatorNamespace, rawFRRConfig.Namespace)
	}

	if rawFRRConfig.Spec.RawConfig == "" {
		return fmt.Errorf("rawConfig must not be empty")
	}
	return nil
}
