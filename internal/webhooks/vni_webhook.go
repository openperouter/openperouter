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

var ValidateVNIs func(vnis []v1alpha1.VNI) error

const (
	vniValidationWebhookPath = "/validate-openperouter-io-v1alpha1-vni"
)

type VNIValidator struct {
	client  client.Client
	decoder admission.Decoder
}

func SetupVNI(mgr ctrl.Manager) error {
	validator := &VNIValidator{
		client:  mgr.GetClient(),
		decoder: admission.NewDecoder(mgr.GetScheme()),
	}

	mgr.GetWebhookServer().Register(
		vniValidationWebhookPath,
		&webhook.Admission{Handler: validator})

	if _, err := mgr.GetCache().GetInformer(context.Background(), &v1alpha1.VNI{}); err != nil {
		return fmt.Errorf("failed to get informer for Vni: %w", err)
	}
	return nil
}

// +kubebuilder:webhook:verbs=create;update,path=/validate-openperouter-io-v1alpha1-vni,mutating=false,failurePolicy=fail,groups=openpe.openperouter.github.io,resources=vnis,versions=v1alpha1,name=vnivalidationwebhook.openperouter.io,sideEffects=None,admissionReviewVersions=v1

func (v *VNIValidator) Handle(ctx context.Context, req admission.Request) (resp admission.Response) {
	var vni v1alpha1.VNI
	var oldVNI v1alpha1.VNI
	if req.Operation == v1.Delete {
		if err := v.decoder.DecodeRaw(req.OldObject, &vni); err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}
	} else {
		if err := v.decoder.Decode(req, &vni); err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}
		if req.OldObject.Size() > 0 {
			if err := v.decoder.DecodeRaw(req.OldObject, &oldVNI); err != nil {
				return admission.Errored(http.StatusBadRequest, err)
			}
		}
	}

	var warnings []string
	switch req.Operation {
	case v1.Create:
		w, err := validateVNI(&vni)
		if err != nil {
			return admission.Denied(err.Error())
		}
		warnings = w
	case v1.Update:
		w, err := validateVNI(&vni)
		if err != nil {
			return admission.Denied(err.Error())
		}
		warnings = w
	case v1.Delete:
		w, err := validateVNIDelete(&vni)
		if err != nil {
			return admission.Denied(err.Error())
		}
		warnings = w
	}
	return admission.Allowed("").WithWarnings(warnings...)
}

func validateVNICreate(vni *v1alpha1.VNI) ([]string, error) {
	Logger.Debug("webhook vni", "action", "create", "name", vni.Name, "namespace", vni.Namespace)
	defer Logger.Debug("webhook vni", "action", "end create", "name", vni.Name, "namespace", vni.Namespace)

	return validateVNI(vni)
}

func validateVNIUpdate(vni *v1alpha1.VNI) ([]string, error) {
	Logger.Debug("webhook vni", "action", "update", "name", vni.Name, "namespace", vni.Namespace)
	defer Logger.Debug("webhook vni", "action", "end update", "name", vni.Name, "namespace", vni.Namespace)

	return validateVNI(vni)
}

func validateVNIDelete(_ *v1alpha1.VNI) ([]string, error) {
	return []string{}, nil
}

func validateVNI(vni *v1alpha1.VNI) ([]string, error) {
	var warnings []string

	existingVNIs, err := getVNIs()
	if err != nil {
		return warnings, err
	}

	toValidate := make([]v1alpha1.VNI, 0, len(existingVNIs.Items))
	found := false
	for _, existingVNI := range existingVNIs.Items {
		if existingVNI.Name == vni.Name && existingVNI.Namespace == vni.Namespace {
			toValidate = append(toValidate, *vni.DeepCopy())
			found = true
			continue
		}
		toValidate = append(toValidate, existingVNI)
	}
	if !found {
		toValidate = append(toValidate, *vni.DeepCopy())
	}

	if err := ValidateVNIs(toValidate); err != nil {
		return warnings, fmt.Errorf("validation failed: %w", err)
	}
	return warnings, nil
}

var getVNIs = func() (*v1alpha1.VNIList, error) {
	vniList := &v1alpha1.VNIList{}
	err := WebhookClient.List(context.Background(), vniList, &client.ListOptions{})
	if err != nil {
		return nil, errors.Join(err, errors.New("failed to get existing FRRConfiguration objects"))
	}
	return vniList, nil
}
