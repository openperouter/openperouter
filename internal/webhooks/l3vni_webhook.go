// SPDX-License-Identifier:Apache-2.0

package webhooks

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/openperouter/openperouter/api/v1alpha1"
	v1 "k8s.io/api/admission/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

var ValidateL3VNIs func(l3vnis []v1alpha1.L3VNI) error

const (
	l3vniValidationWebhookPath = "/validate-openperouter-io-v1alpha1-l3vni"
)

type L3VNIValidator struct {
	client  client.Client
	decoder admission.Decoder
}

func SetupL3VNI(mgr ctrl.Manager) error {
	validator := &L3VNIValidator{
		client:  mgr.GetClient(),
		decoder: admission.NewDecoder(mgr.GetScheme()),
	}

	mgr.GetWebhookServer().Register(
		l3vniValidationWebhookPath,
		&webhook.Admission{Handler: validator})

	if _, err := mgr.GetCache().GetInformer(context.Background(), &v1alpha1.L3VNI{}); err != nil {
		return fmt.Errorf("failed to get informer for L3VNI: %w", err)
	}
	return nil
}

func (v *L3VNIValidator) Handle(ctx context.Context, req admission.Request) (resp admission.Response) {
	var l3vni v1alpha1.L3VNI
	var oldL3VNI v1alpha1.L3VNI
	if req.Operation == v1.Delete {
		if err := v.decoder.DecodeRaw(req.OldObject, &l3vni); err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}
	} else {
		if err := v.decoder.Decode(req, &l3vni); err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}
		if req.OldObject.Size() > 0 {
			if err := v.decoder.DecodeRaw(req.OldObject, &oldL3VNI); err != nil {
				return admission.Errored(http.StatusBadRequest, err)
			}
		}
	}

	switch req.Operation {
	case v1.Create:
		if err := validateL3VNICreate(&l3vni); err != nil {
			return admission.Denied(err.Error())
		}
	case v1.Update:
		if err := validateL3VNIUpdate(&l3vni, &oldL3VNI); err != nil {
			return admission.Denied(err.Error())
		}
	case v1.Delete:
		if err := validateL3VNIDelete(&l3vni); err != nil {
			return admission.Denied(err.Error())
		}
	}
	return admission.Allowed("")
}

func validateL3VNICreate(l3vni *v1alpha1.L3VNI) error {
	Logger.Debug("webhook l3vni", "action", "create", "name", l3vni.Name, "namespace", l3vni.Namespace)
	defer Logger.Debug("webhook l3vni", "action", "end create", "name", l3vni.Name, "namespace", l3vni.Namespace)

	return validateL3VNI(l3vni)
}

func validateL3VNIUpdate(l3vni *v1alpha1.L3VNI, oldL3VNI *v1alpha1.L3VNI) error {
	Logger.Debug("webhook l3vni", "action", "update", "name", l3vni.Name, "namespace", l3vni.Namespace)
	defer Logger.Debug("webhook l3vni", "action", "end update", "name", l3vni.Name, "namespace", l3vni.Namespace)

	if oldL3VNI.Spec.HostSession != nil && l3vni.Spec.HostSession != nil &&
		oldL3VNI.Spec.HostSession.LocalCIDR != l3vni.Spec.HostSession.LocalCIDR {
		return errors.New("LocalCIDR cannot be changed")
	}

	return validateL3VNI(l3vni)
}

func validateL3VNIDelete(_ *v1alpha1.L3VNI) error {
	return nil
}

func validateL3VNI(l3vni *v1alpha1.L3VNI) error {
	existingL3VNIs, err := getL3VNIs()
	if err != nil {
		return err
	}

	toValidate := make([]v1alpha1.L3VNI, 0, len(existingL3VNIs.Items))
	found := false
	for _, existingL3VNI := range existingL3VNIs.Items {
		if existingL3VNI.Name == l3vni.Name && existingL3VNI.Namespace == l3vni.Namespace {
			toValidate = append(toValidate, *l3vni.DeepCopy())
			found = true
			continue
		}
		toValidate = append(toValidate, existingL3VNI)
	}
	if !found {
		toValidate = append(toValidate, *l3vni.DeepCopy())
	}

	if err := ValidateL3VNIs(toValidate); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}
	return nil
}

var getL3VNIs = func() (*v1alpha1.L3VNIList, error) {
	l3vniList := &v1alpha1.L3VNIList{}
	err := WebhookClient.List(context.Background(), l3vniList, &client.ListOptions{})
	if err != nil {
		return nil, errors.Join(err, errors.New("failed to get existing L3VNI objects"))
	}
	return l3vniList, nil
}
