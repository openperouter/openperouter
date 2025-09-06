// SPDX-License-Identifier:Apache-2.0

package webhooks

import (
	"context"
	"fmt"
	"net/http"

	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/internal/conversion"
	v1 "k8s.io/api/admission/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

var ValidateL3Passthroughs func(l3passthroughs []v1alpha1.L3Passthrough) error

const (
	l3passthroughValidationWebhookPath = "/validate-openperouter-io-v1alpha1-l3passthrough"
)

type L3PassthroughValidator struct {
	client  client.Client
	decoder admission.Decoder
}

func SetupL3Passthrough(mgr ctrl.Manager) error {
	validator := &L3PassthroughValidator{
		client:  mgr.GetClient(),
		decoder: admission.NewDecoder(mgr.GetScheme()),
	}

	mgr.GetWebhookServer().Register(
		l3passthroughValidationWebhookPath,
		&webhook.Admission{Handler: validator})

	if _, err := mgr.GetCache().GetInformer(context.Background(), &v1alpha1.L3Passthrough{}); err != nil {
		return fmt.Errorf("failed to get informer for L3Passthrough: %w", err)
	}
	return nil
}

func (v *L3PassthroughValidator) Handle(ctx context.Context, req admission.Request) (resp admission.Response) {
	var l3passthrough v1alpha1.L3Passthrough
	var oldL3Passthrough v1alpha1.L3Passthrough
	if req.Operation == v1.Delete {
		if err := v.decoder.DecodeRaw(req.OldObject, &l3passthrough); err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}
	} else {
		if err := v.decoder.Decode(req, &l3passthrough); err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}
		if req.OldObject.Size() > 0 {
			if err := v.decoder.DecodeRaw(req.OldObject, &oldL3Passthrough); err != nil {
				return admission.Errored(http.StatusBadRequest, err)
			}
		}
	}

	switch req.Operation {
	case v1.Create:
		if err := validateL3PassthroughCreate(&l3passthrough); err != nil {
			return admission.Denied(err.Error())
		}
	case v1.Update:
		if err := validateL3PassthroughUpdate(&l3passthrough, &oldL3Passthrough); err != nil {
			return admission.Denied(err.Error())
		}
	case v1.Delete:
		if err := validateL3PassthroughDelete(&l3passthrough); err != nil {
			return admission.Denied(err.Error())
		}
	}
	return admission.Allowed("")
}

func validateL3PassthroughCreate(l3passthrough *v1alpha1.L3Passthrough) error {
	Logger.Debug("webhook l3passthrough", "action", "create", "name", l3passthrough.Name, "namespace", l3passthrough.Namespace)
	defer Logger.Debug("webhook l3passthrough", "action", "end create", "name", l3passthrough.Name, "namespace", l3passthrough.Namespace)

	return validateL3Passthrough(l3passthrough)
}

func validateL3PassthroughUpdate(l3passthrough *v1alpha1.L3Passthrough, _ *v1alpha1.L3Passthrough) error {
	Logger.Debug("webhook l3passthrough", "action", "update", "name", l3passthrough.Name, "namespace", l3passthrough.Namespace)
	defer Logger.Debug("webhook l3passthrough", "action", "end update", "name", l3passthrough.Name, "namespace", l3passthrough.Namespace)

	return validateL3Passthrough(l3passthrough)
}

func validateL3PassthroughDelete(_ *v1alpha1.L3Passthrough) error {
	return nil
}

func validateL3Passthrough(l3passthrough *v1alpha1.L3Passthrough) error {
	existingL3Passthroughs, err := getL3Passthroughs()
	if err != nil {
		return err
	}

	toValidate := make([]v1alpha1.L3Passthrough, 0, len(existingL3Passthroughs.Items))
	found := false
	for _, existingL3Passthrough := range existingL3Passthroughs.Items {
		if existingL3Passthrough.Name == l3passthrough.Name && existingL3Passthrough.Namespace == l3passthrough.Namespace {
			toValidate = append(toValidate, *l3passthrough.DeepCopy())
			found = true
			continue
		}
		toValidate = append(toValidate, existingL3Passthrough)
	}
	if !found {
		toValidate = append(toValidate, *l3passthrough.DeepCopy())
	}

	if err := ValidateL3Passthroughs(toValidate); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	l3vnis, err := getL3VNIs()
	if err != nil {
		return err
	}
	if err := conversion.ValidateHostSessions(l3vnis.Items, toValidate); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}
	return nil
}

var getL3Passthroughs = func() (*v1alpha1.L3PassthroughList, error) {
	l3passthroughList := &v1alpha1.L3PassthroughList{}
	err := WebhookClient.List(context.Background(), l3passthroughList, &client.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get existing L3Passthrough objects: %w", err)
	}
	return l3passthroughList, nil
}
