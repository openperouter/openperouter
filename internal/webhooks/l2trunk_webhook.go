// SPDX-License-Identifier:Apache-2.0

package webhooks

import (
	"context"
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

const l2trunkValidationWebhookPath = "/validate-openperouter-io-v1alpha1-l2trunk"

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
	l2trunk, err := v.decodeL2Trunk(req)
	if err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	switch req.Operation {
	case v1.Create, v1.Update:
		return validateL2TrunkAdmission(l2trunk)
	case v1.Delete:
		return warnOrphanedL2VNIs(l2trunk)
	}
	return admission.Allowed("")
}

func (v *L2TrunkValidator) decodeL2Trunk(req admission.Request) (v1alpha1.L2Trunk, error) {
	var l2trunk v1alpha1.L2Trunk
	var err error
	if req.Operation == v1.Delete {
		err = v.decoder.DecodeRaw(req.OldObject, &l2trunk)
	} else {
		err = v.decoder.Decode(req, &l2trunk)
	}
	return l2trunk, err
}

func validateL2TrunkAdmission(l2trunk v1alpha1.L2Trunk) admission.Response {
	if err := validateL2Trunk(&l2trunk); err != nil {
		return admission.Denied(err.Error())
	}
	if err := DatapathConfigValidator.Validate(conversion.APIConfigData{
		L2Trunks: []v1alpha1.L2Trunk{l2trunk},
	}); err != nil {
		return admission.Denied(err.Error())
	}
	return admission.Allowed("")
}

func warnOrphanedL2VNIs(l2trunk v1alpha1.L2Trunk) admission.Response {
	warnings, err := orphanedL2VNIWarnings(l2trunk)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}
	if len(warnings) == 0 {
		return admission.Allowed("")
	}
	return admission.Allowed("").WithWarnings(warnings...)
}

func validateL2Trunk(l2trunk *v1alpha1.L2Trunk) error {
	Logger.Debug("webhook l2trunk", "action", "validate", "name", l2trunk.Name, "namespace", l2trunk.Namespace)
	defer Logger.Debug("webhook l2trunk", "action", "end validate", "name", l2trunk.Name, "namespace", l2trunk.Namespace)

	trunks, err := getL2Trunks()
	if err != nil {
		return err
	}
	l2vnis, err := getL2VNIs()
	if err != nil {
		return err
	}
	nodes, err := getNodes()
	if err != nil {
		return err
	}

	toValidate := make([]v1alpha1.L2Trunk, 0, len(trunks.Items)+1)
	for _, existing := range trunks.Items {
		if existing.Name == l2trunk.Name && existing.Namespace == l2trunk.Namespace {
			continue
		}
		toValidate = append(toValidate, existing)
	}
	// The trunk being validated goes last, so a VF conflict is reported
	// against it rather than against the trunk already using the VF.
	toValidate = append(toValidate, *l2trunk.DeepCopy())

	if err := conversion.ValidateOverlayResourcesForNodes(
		nodes, l2vnis.Items, toValidate, nil, nil, nil); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}
	return nil
}

// orphanedL2VNIWarnings lists the L2VNIs whose dataplane will be torn down by
// this deletion. The delete is admitted, per the L2Trunk contract, but the
// operator gets an explicit signal before a whole set of tenant segments goes
// dark.
func orphanedL2VNIWarnings(l2trunk v1alpha1.L2Trunk) ([]string, error) {
	l2vnis, err := getL2VNIs()
	if err != nil {
		return nil, err
	}
	var warnings []string
	for _, l2vni := range l2vnis.Items {
		if l2vni.Namespace != l2trunk.Namespace {
			continue
		}
		if l2vni.Spec.Trunk == nil || l2vni.Spec.Trunk.Name != l2trunk.Name {
			continue
		}
		warnings = append(warnings, fmt.Sprintf(
			"L2VNI %s/%s references this L2Trunk and will lose its dataplane configuration",
			l2vni.Namespace, l2vni.Name))
	}
	slices.Sort(warnings)
	return warnings, nil
}

var getL2Trunks = func() (*v1alpha1.L2TrunkList, error) {
	l2trunkList := &v1alpha1.L2TrunkList{}
	if err := WebhookClient.List(context.Background(), l2trunkList, &client.ListOptions{}); err != nil {
		return nil, fmt.Errorf("failed to get existing L2Trunk objects: %w", err)
	}
	return l2trunkList, nil
}

var getNodes = func() ([]corev1.Node, error) {
	nodeList := &corev1.NodeList{}
	if err := WebhookClient.List(context.Background(), nodeList, &client.ListOptions{}); err != nil {
		return nil, fmt.Errorf("failed to get existing Node objects when validating L2Trunk: %w", err)
	}
	return nodeList.Items, nil
}
