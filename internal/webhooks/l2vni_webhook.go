// SPDX-License-Identifier:Apache-2.0

package webhooks

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"

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
	l2vniValidationWebhookPath = "/validate-openperouter-io-v1alpha1-l2vni"
)

type L2VNIValidator struct {
	client  client.Client
	decoder admission.Decoder
}

func SetupL2VNI(mgr ctrl.Manager) error {
	validator := &L2VNIValidator{
		client:  mgr.GetClient(),
		decoder: admission.NewDecoder(mgr.GetScheme()),
	}

	mgr.GetWebhookServer().Register(
		l2vniValidationWebhookPath,
		&webhook.Admission{Handler: validator})

	if _, err := mgr.GetCache().GetInformer(context.Background(), &v1alpha1.L2VNI{}); err != nil {
		return fmt.Errorf("failed to get informer for L2VNI: %w", err)
	}
	return nil
}

func (v *L2VNIValidator) Handle(ctx context.Context, req admission.Request) (resp admission.Response) {
	var l2vni v1alpha1.L2VNI
	var oldL2VNI v1alpha1.L2VNI
	if req.Operation == v1.Delete {
		if err := v.decoder.DecodeRaw(req.OldObject, &l2vni); err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}
	} else {
		if err := v.decoder.Decode(req, &l2vni); err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}
		if req.OldObject.Size() > 0 {
			if err := v.decoder.DecodeRaw(req.OldObject, &oldL2VNI); err != nil {
				return admission.Errored(http.StatusBadRequest, err)
			}
		}
	}

	switch req.Operation {
	case v1.Create:
		if err := validateL2VNICreate(&l2vni); err != nil {
			return admission.Denied(err.Error())
		}
	case v1.Update:
		if err := validateL2VNIUpdate(&oldL2VNI, &l2vni); err != nil {
			return admission.Denied(err.Error())
		}
	case v1.Delete:
		if err := validateL2VNIDelete(&l2vni); err != nil {
			return admission.Denied(err.Error())
		}
	}
	return admission.Allowed("")
}

func validateL2VNICreate(l2vni *v1alpha1.L2VNI) error {
	Logger.Debug("webhook l2vni", "action", "create", "name", l2vni.Name, "namespace", l2vni.Namespace)
	defer Logger.Debug("webhook l2vni", "action", "end create", "name", l2vni.Name, "namespace", l2vni.Namespace)

	return validateL2VNI(l2vni)
}

func validateL2VNIUpdate(l2vni *v1alpha1.L2VNI, oldL2VNI *v1alpha1.L2VNI) error {
	Logger.Debug("webhook l2vni", "action", "update", "name", l2vni.Name, "namespace", l2vni.Namespace)
	defer Logger.Debug("webhook l2vni", "action", "end update", "name", l2vni.Name, "namespace", l2vni.Namespace)

	if !slices.Equal(oldL2VNI.Spec.L2GatewayIPs, l2vni.Spec.L2GatewayIPs) {
		return errors.New("L2GatewayIPs cannot be changed")
	}

	return validateL2VNI(l2vni)
}

func validateL2VNIDelete(_ *v1alpha1.L2VNI) error {
	return nil
}

func validateL2VNI(l2vni *v1alpha1.L2VNI) error {
	existingL2VNIs, err := getL2VNIs()
	if err != nil {
		return err
	}

	toValidate := make([]v1alpha1.L2VNI, 0, len(existingL2VNIs.Items))
	found := false
	for _, existingL2VNI := range existingL2VNIs.Items {
		if existingL2VNI.Name == l2vni.Name && existingL2VNI.Namespace == l2vni.Namespace {
			toValidate = append(toValidate, *l2vni.DeepCopy())
			found = true
			continue
		}
		toValidate = append(toValidate, existingL2VNI)
	}
	if !found {
		toValidate = append(toValidate, *l2vni.DeepCopy())
	}

	nodeList := &corev1.NodeList{}
	if err := WebhookClient.List(context.Background(), nodeList, &client.ListOptions{}); err != nil {
		return fmt.Errorf("failed to get existing Node objects when validating L2VNI: %w", err)
	}

	if err := conversion.ValidateL2VNIsForNodes(nodeList.Items, toValidate); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}
	return nil
}

var getL2VNIs = func() (*v1alpha1.L2VNIList, error) {
	l2vniList := &v1alpha1.L2VNIList{}
	err := WebhookClient.List(context.Background(), l2vniList, &client.ListOptions{})
	if err != nil {
		return nil, errors.Join(err, errors.New("failed to get existing L2VNI objects"))
	}
	return l2vniList, nil
}
