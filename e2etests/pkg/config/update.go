// SPDX-License-Identifier:Apache-2.0

package config

import (
	"context"
	"time"

	frrk8sv1beta1 "github.com/metallb/frr-k8s/api/v1beta1"
	"github.com/openperouter/openperouter/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

type Resources struct {
	Underlays         []v1alpha1.Underlay `json:"underlays"`
	L3VNIs            []v1alpha1.L3VNI    `json:"l3vnis"`
	L2VNIs            []v1alpha1.L2VNI    `json:"l2vnis"`
	FRRConfigurations []frrk8sv1beta1.FRRConfiguration
}

type Updater struct {
	cli       client.Client
	namespace string
}

func UpdaterForCRs(r *rest.Config, ns string) (*Updater, error) {
	myScheme := runtime.NewScheme()

	if err := v1alpha1.AddToScheme(myScheme); err != nil {
		return nil, err
	}

	if err := corev1.AddToScheme(myScheme); err != nil {
		return nil, err
	}

	if err := frrk8sv1beta1.AddToScheme(myScheme); err != nil {
		return nil, err
	}

	cl, err := client.New(r, client.Options{
		Scheme: myScheme,
	})

	if err != nil {
		return nil, err
	}

	return &Updater{
		cli:       cl,
		namespace: ns,
	}, nil
}

func (o Updater) Update(r Resources) error {
	// we fill a map of objects to keep the order we add the resources random, as
	// it would happen by throwing a set of manifests against a cluster, hoping to
	// find corner cases that we would not find by adding them always in the same
	// order.
	objects := map[int]client.Object{}
	oldValues := map[int]client.Object{}
	key := 0
	for _, underlay := range r.Underlays {
		objects[key] = underlay.DeepCopy()
		oldValues[key] = underlay.DeepCopy()
		key++
	}
	for _, vni := range r.L3VNIs {
		objects[key] = vni.DeepCopy()
		oldValues[key] = vni.DeepCopy()
		key++
	}
	for _, vni := range r.L2VNIs {
		objects[key] = vni.DeepCopy()
		oldValues[key] = vni.DeepCopy()
		key++
	}
	for _, frrConfig := range r.FRRConfigurations {
		objects[key] = frrConfig.DeepCopy()
		oldValues[key] = frrConfig.DeepCopy()
		key++
	}

	// Iterating over the map will return the items in a random order.
	for i, obj := range objects {
		obj.SetNamespace(o.namespace)
		_, err := controllerutil.CreateOrUpdate(context.Background(), o.cli, obj, func() error {
			// the mutate function is expected to change the object when updating.
			// we always override with the old version, and we change only the spec part.
			switch toChange := obj.(type) {
			case *v1alpha1.Underlay:
				old := oldValues[i].(*v1alpha1.Underlay)
				toChange.Spec = *old.Spec.DeepCopy()
			case *v1alpha1.L3VNI:
				old := oldValues[i].(*v1alpha1.L3VNI)
				toChange.Spec = *old.Spec.DeepCopy()
			case *v1alpha1.L2VNI:
				old := oldValues[i].(*v1alpha1.L2VNI)
				toChange.Spec = *old.Spec.DeepCopy()
			case *frrk8sv1beta1.FRRConfiguration:
				old := oldValues[i].(*frrk8sv1beta1.FRRConfiguration)
				toChange.Spec = *old.Spec.DeepCopy()
			}

			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// CleanAll deletes all relevant resources in the namespace and waits for them to be fully deleted.
func (o Updater) CleanAll() error {
	if err := o.CleanButUnderlay(); err != nil {
		return err
	}

	if err := o.CleanUnderlay(); err != nil {
		return err
	}

	return nil
}

// waitForDeletionOfAllOf waits for all resources of a given type to be deleted from the namespace.
func (o Updater) waitForDeletionOfAllOf(objList client.ObjectList) error {
	return wait.PollImmediate(1*time.Second, 30*time.Second, func() (bool, error) {
		if err := o.cli.List(context.Background(), objList, client.InNamespace(o.namespace)); err != nil {
			return false, err
		}

		// Use reflection to check if list is empty
		switch list := objList.(type) {
		case *v1alpha1.UnderlayList:
			return len(list.Items) == 0, nil
		case *v1alpha1.UnderlayNodeStatusList:
			return len(list.Items) == 0, nil
		case *v1alpha1.L3VNIList:
			return len(list.Items) == 0, nil
		case *v1alpha1.L2VNIList:
			return len(list.Items) == 0, nil
		case *frrk8sv1beta1.FRRConfigurationList:
			return len(list.Items) == 0, nil
		}
		return true, nil
	})
}

// CleanUnderlay deletes underlay resources and waits for them to be fully deleted.
func (o Updater) CleanUnderlay() error {
	// Delete Underlays, and wait for them to be removed
	if err := o.cli.DeleteAllOf(context.Background(), &v1alpha1.Underlay{},
		client.InNamespace(o.namespace)); err != nil {
		return err
	}
	if err := o.waitForDeletionOfAllOf(&v1alpha1.UnderlayList{}); err != nil {
		return err
	}

	// Wait for UnderlayNodeStatus to be deleted (via owner references)
	if err := o.waitForDeletionOfAllOf(&v1alpha1.UnderlayNodeStatusList{}); err != nil {
		return err
	}

	return nil
}

// CleanButUnderlay deletes all resources but the underlays and waits for them to be fully deleted.
// This is needed as deleting underlays is a time consuming operation that
// will cause the router pods to be recreated.
func (o Updater) CleanButUnderlay() error {
	// Delete L3VNIs, and wait for them to be removed
	if err := o.cli.DeleteAllOf(context.Background(), &v1alpha1.L3VNI{},
		client.InNamespace(o.namespace)); err != nil {
		return err
	}
	if err := o.waitForDeletionOfAllOf(&v1alpha1.L3VNIList{}); err != nil {
		return err
	}

	// Delete L2VNIs, and wait for them to be removed
	if err := o.cli.DeleteAllOf(context.Background(), &v1alpha1.L2VNI{},
		client.InNamespace(o.namespace)); err != nil {
		return err
	}
	if err := o.waitForDeletionOfAllOf(&v1alpha1.L2VNIList{}); err != nil {
		return err
	}

	// Delete FRRConfigurations, and wait for them to be removed
	if err := o.cli.DeleteAllOf(context.Background(), &frrk8sv1beta1.FRRConfiguration{},
		client.InNamespace(o.namespace)); err != nil {
		return err
	}
	if err := o.waitForDeletionOfAllOf(&frrk8sv1beta1.FRRConfigurationList{}); err != nil {
		return err
	}

	return nil
}

func (o Updater) Client() client.Client {
	return o.cli
}

func (o Updater) Namespace() string {
	return o.namespace
}
