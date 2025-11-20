// SPDX-License-Identifier:Apache-2.0

package webhooks

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	v1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/internal/conversion"
)

const (
	underlayValidationWebhookPath = "/validate-openperouter-io-v1alpha1-underlay"
)

type UnderlayValidator struct {
	client  client.Client
	decoder admission.Decoder
}

func SetupUnderlay(mgr ctrl.Manager) error {
	validator := &UnderlayValidator{
		client:  mgr.GetClient(),
		decoder: admission.NewDecoder(mgr.GetScheme()),
	}

	mgr.GetWebhookServer().Register(
		underlayValidationWebhookPath,
		&webhook.Admission{Handler: validator})

	if _, err := mgr.GetCache().GetInformer(context.Background(), &v1alpha1.Underlay{}); err != nil {
		return fmt.Errorf("failed to get informer for Underlay: %w", err)
	}
	return nil
}

func (v *UnderlayValidator) Handle(ctx context.Context, req admission.Request) (resp admission.Response) {
	var underlay v1alpha1.Underlay
	var oldUnderlay v1alpha1.Underlay
	if req.Operation == v1.Delete {
		if err := v.decoder.DecodeRaw(req.OldObject, &underlay); err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}
	} else {
		if err := v.decoder.Decode(req, &underlay); err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}
		if req.OldObject.Size() > 0 {
			if err := v.decoder.DecodeRaw(req.OldObject, &oldUnderlay); err != nil {
				return admission.Errored(http.StatusBadRequest, err)
			}
		}
	}

	switch req.Operation {
	case v1.Create:
		err := validateUnderlayCreate(&underlay)
		if err != nil {
			return admission.Denied(err.Error())
		}
	case v1.Update:
		err := validateUnderlayUpdate(&underlay)
		if err != nil {
			return admission.Denied(err.Error())
		}
	case v1.Delete:
		err := validateUnderlayDelete(&underlay)
		if err != nil {
			return admission.Denied(err.Error())
		}
	}
	return admission.Allowed("")
}

func validateUnderlayCreate(underlay *v1alpha1.Underlay) error {
	Logger.Debug("webhook underlay", "action", "create", "name", underlay.Name, "namespace", underlay.Namespace)
	defer Logger.Debug("webhook underlay", "action", "end create", "name", underlay.Name, "namespace", underlay.Namespace)

	return validateUnderlay(underlay)
}

func validateUnderlayUpdate(underlay *v1alpha1.Underlay) error {
	Logger.Debug("webhook underlay", "action", "update", "name", underlay.Name, "namespace", underlay.Namespace)
	defer Logger.Debug("webhook underlay", "action", "end update", "name", underlay.Name, "namespace", underlay.Namespace)

	return validateUnderlay(underlay)
}

func validateUnderlayDelete(_ *v1alpha1.Underlay) error {
	return nil
}

func validateUnderlay(underlay *v1alpha1.Underlay) error {
	existingUnderlays, err := getUnderlays()
	if err != nil {
		return err
	}
	toValidate := make([]v1alpha1.Underlay, 0, len(existingUnderlays.Items))
	found := false
	for _, existingUnderlay := range existingUnderlays.Items {
		if existingUnderlay.Name == underlay.Name && existingUnderlay.Namespace == underlay.Namespace {
			toValidate = append(toValidate, *underlay.DeepCopy())
			found = true
			continue
		}
		toValidate = append(toValidate, existingUnderlay)
	}
	if !found {
		toValidate = append(toValidate, *underlay.DeepCopy())
	}

	nodeList := &corev1.NodeList{}
	if err := WebhookClient.List(context.Background(), nodeList, &client.ListOptions{}); err != nil {
		return fmt.Errorf("failed to get existing Node objects when validating Underlay: %w", err)
	}

	if err := conversion.ValidateUnderlaysForNodes(nodeList.Items, toValidate); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}
	return nil
}

var getUnderlays = func() (*v1alpha1.UnderlayList, error) {
	underlayList := &v1alpha1.UnderlayList{}
	err := WebhookClient.List(context.Background(), underlayList, &client.ListOptions{})
	if err != nil {
		return nil, errors.Join(err, errors.New("failed to get existing FRRConfiguration objects"))
	}
	return underlayList, nil
}
