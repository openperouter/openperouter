// SPDX-License-Identifier:Apache-2.0

package webhooks

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"

	v1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/internal/cniinvoker"
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
	}
	if req.Operation != v1.Delete {
		if err := v.decoder.Decode(req, &underlay); err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}
	}
	if req.Operation != v1.Delete && req.OldObject.Size() > 0 {
		if err := v.decoder.DecodeRaw(req.OldObject, &oldUnderlay); err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}
	}

	switch req.Operation {
	case v1.Create:
		if err := validateUnderlayCreate(&underlay); err != nil {
			return admission.Denied(err.Error())
		}
		if err := DatapathConfigValidator.Validate(conversion.APIConfigData{Underlays: []v1alpha1.Underlay{underlay}}); err != nil {
			return admission.Denied(err.Error())
		}
	case v1.Update:
		if err := validateUnderlayUpdate(&oldUnderlay, &underlay); err != nil {
			return admission.Denied(err.Error())
		}
		if err := DatapathConfigValidator.Validate(conversion.APIConfigData{Underlays: []v1alpha1.Underlay{underlay}}); err != nil {
			return admission.Denied(err.Error())
		}
	case v1.Delete:
		if err := validateUnderlayDelete(&underlay); err != nil {
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

func validateUnderlayUpdate(oldUnderlay *v1alpha1.Underlay, newUnderlay *v1alpha1.Underlay) error {
	Logger.Debug("webhook underlay", "action", "update", "name", newUnderlay.Name, "namespace", newUnderlay.Namespace)
	defer Logger.Debug("webhook underlay", "action", "end update", "name", newUnderlay.Name, "namespace", newUnderlay.Namespace)

	if err := validateInterfaceTypeImmutable(oldUnderlay, newUnderlay); err != nil {
		return err
	}
	if err := validateCNIDeviceImmutable(oldUnderlay, newUnderlay); err != nil {
		return err
	}
	return validateUnderlay(newUnderlay)
}

func validateUnderlayDelete(deletedUnderlay *v1alpha1.Underlay) error {
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

// validateCNIDeviceImmutable rejects in-place changes to a CNI interface's
// rawConfig and runtimeConfig. Reconciling a config change in place would
// require a DEL/ADD cycle with partial-failure states where the old interface
// is torn down but the new one fails to provision; operators must delete and
// recreate the Underlay instead. Enforced here because CEL transition rules
// (oldSelf) cannot be evaluated inside atomic lists. Configuration paths that
// bypass admission (e.g. host mode static files) are enforced at reconcile
// time by cni.Invoker.Add, which compares the desired config against the
// attachment recorded in the libcni result cache.
func validateCNIDeviceImmutable(oldUnderlay, newUnderlay *v1alpha1.Underlay) error {
	oldDevices := cniDevicesByInterface(oldUnderlay.Spec.Interfaces)
	for _, iface := range newUnderlay.Spec.Interfaces {
		if iface.Type != v1alpha1.UnderlayInterfaceTypeCNIDevice || iface.CNIDevice == nil {
			continue
		}
		ifName := cniInterfaceName(iface)
		oldDevice, found := oldDevices[ifName]
		if !found {
			continue
		}
		if !reflect.DeepEqual(oldDevice.RawConfig, iface.CNIDevice.RawConfig) {
			return fmt.Errorf("cni rawConfig for interface %q is immutable, delete and recreate the Underlay to change it",
				ifName)
		}
		if !reflect.DeepEqual(oldDevice.RuntimeConfig, iface.CNIDevice.RuntimeConfig) {
			return fmt.Errorf("cni runtimeConfig for interface %q is immutable, delete and recreate the Underlay to change it",
				ifName)
		}
	}
	return nil
}

// validateInterfaceTypeImmutable rejects changing the type of an underlay
// interface that keeps its name: reconciling it in place would require a
// teardown/re-add cycle with partial-failure states, so the Underlay must be
// deleted and recreated instead. Enforced here because CEL transition rules
// (oldSelf) cannot be evaluated inside atomic lists.
func validateInterfaceTypeImmutable(oldUnderlay, newUnderlay *v1alpha1.Underlay) error {
	oldTypes := interfaceTypesByName(oldUnderlay.Spec.Interfaces)
	for name, newType := range interfaceTypesByName(newUnderlay.Spec.Interfaces) {
		oldType, found := oldTypes[name]
		if !found {
			continue
		}
		if oldType != newType {
			return fmt.Errorf("type of interface %q is immutable (%s to %s), delete and recreate the Underlay to change it",
				name, oldType, newType)
		}
	}
	return nil
}

// interfaceTypesByName indexes the type of every underlay interface by its
// resolved interface name.
func interfaceTypesByName(interfaces []v1alpha1.UnderlayInterface) map[string]v1alpha1.UnderlayInterfaceType {
	res := map[string]v1alpha1.UnderlayInterfaceType{}
	for _, iface := range interfaces {
		switch iface.Type {
		case v1alpha1.UnderlayInterfaceTypeNetworkDevice:
			if iface.NetworkDevice != nil {
				res[iface.NetworkDevice.InterfaceName] = iface.Type
			}
		case v1alpha1.UnderlayInterfaceTypeCNIDevice:
			if iface.CNIDevice != nil {
				res[cniInterfaceName(iface)] = iface.Type
			}
		}
	}
	return res
}

// cniDevicesByInterface indexes the cniDevice of every CNI interface by its
// interface name inside the router netns.
func cniDevicesByInterface(interfaces []v1alpha1.UnderlayInterface) map[string]*v1alpha1.CNIDevice {
	res := map[string]*v1alpha1.CNIDevice{}
	for _, iface := range interfaces {
		if iface.Type != v1alpha1.UnderlayInterfaceTypeCNIDevice || iface.CNIDevice == nil {
			continue
		}
		res[cniInterfaceName(iface)] = iface.CNIDevice
	}
	return res
}

// cniInterfaceName resolves the interface name of a CNI interface, applying
// the same "net1" default used at runtime so that omitted and explicit
// defaults compare equal.
func cniInterfaceName(iface v1alpha1.UnderlayInterface) string {
	return ptr.Deref(iface.CNIDevice.InterfaceName, cniinvoker.DefaultInterfaceName)
}
