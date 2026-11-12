// SPDX-License-Identifier:Apache-2.0

package webhooks

import (
	"context"
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
	l2trunkValidationWebhookPath = "/validate-openperouter-io-v1alpha1-l2trunk"
)

type L2TrunkValidator struct {
	client  client.Client
	decoder admission.Decoder
}

func SetupL2Trunk(mgr ctrl.Manager) error {
	validator := &L2TrunkValidator{
		client:  mgr.GetClient(),
		decoder: admission.NewDecoder(mgr.GetScheme()),
	}

	mgr.GetWebhookServer().Register(
		l2trunkValidationWebhookPath,
		&webhook.Admission{Handler: validator})

	if _, err := mgr.GetCache().GetInformer(context.Background(), &v1alpha1.L2Trunk{}); err != nil {
		return fmt.Errorf("failed to get informer for L2Trunk: %w", err)
	}
	return nil
}

func (v *L2TrunkValidator) Handle(_ context.Context, req admission.Request) admission.Response {
	if req.Operation != v1.Create && req.Operation != v1.Update {
		return admission.Allowed("")
	}

	var l2trunk v1alpha1.L2Trunk
	if err := v.decoder.Decode(req, &l2trunk); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}
	if err := validateL2Trunk(&l2trunk); err != nil {
		return admission.Denied(err.Error())
	}
	if err := DatapathConfigValidator.Validate(conversion.APIConfigData{L2Trunks: []v1alpha1.L2Trunk{l2trunk}}); err != nil {
		return admission.Denied(err.Error())
	}
	return admission.Allowed("")
}

func validateL2Trunk(l2trunk *v1alpha1.L2Trunk) error {
	Logger.Debug("webhook l2trunk", "action", "validate", "name", l2trunk.Name, "namespace", l2trunk.Namespace)
	defer Logger.Debug("webhook l2trunk", "action", "end validate", "name", l2trunk.Name, "namespace", l2trunk.Namespace)

	existingL2Trunks, err := getL2Trunks()
	if err != nil {
		return err
	}

	toValidate := make([]v1alpha1.L2Trunk, 0, len(existingL2Trunks.Items)+1)
	for _, existing := range existingL2Trunks.Items {
		if existing.Name == l2trunk.Name && existing.Namespace == l2trunk.Namespace {
			continue
		}
		toValidate = append(toValidate, existing)
	}
	// The trunk being validated goes last, so a VF conflict is reported
	// against it rather than against the trunk already using the VF.
	toValidate = append(toValidate, *l2trunk.DeepCopy())

	nodeList := &corev1.NodeList{}
	if err := WebhookClient.List(context.Background(), nodeList, &client.ListOptions{}); err != nil {
		return fmt.Errorf("failed to get existing Node objects when validating L2Trunk: %w", err)
	}

	if err := conversion.ValidateL2TrunksForNodes(nodeList.Items, toValidate); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}
	return nil
}

var getL2Trunks = func() (*v1alpha1.L2TrunkList, error) {
	l2trunkList := &v1alpha1.L2TrunkList{}
	if err := WebhookClient.List(context.Background(), l2trunkList, &client.ListOptions{}); err != nil {
		return nil, fmt.Errorf("failed to get existing L2Trunk objects: %w", err)
	}
	return l2trunkList, nil
}
