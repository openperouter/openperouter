// SPDX-License-Identifier:Apache-2.0

package webhooks

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/internal/conversion"
	v1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

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

	if localCIDR(oldL3VNI.Spec.HostSession) != localCIDR(l3vni.Spec.HostSession) {
		return errors.New("LocalCIDR cannot be changed")
	}

	return validateL3VNI(l3vni)
}

func localCIDR(hostSession *v1alpha1.HostSession) v1alpha1.LocalCIDRConfig {
	if hostSession == nil {
		return v1alpha1.LocalCIDRConfig{}
	}
	return hostSession.LocalCIDR
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

	if err := conversion.ValidateRouteTargets(l3vni.Spec); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	nodeList := &corev1.NodeList{}
	if err := WebhookClient.List(context.Background(), nodeList, &client.ListOptions{}); err != nil {
		return fmt.Errorf("failed to get existing Node objects when validating L3VNI: %w", err)
	}

	if err := conversion.ValidateL3VNIsForNodes(nodeList.Items, toValidate); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	l3passthroughs, err := getL3Passthroughs()
	if err != nil {
		return err
	}
	if err := conversion.ValidateHostSessionsForNodes(nodeList.Items, toValidate, l3passthroughs.Items); err != nil {
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
