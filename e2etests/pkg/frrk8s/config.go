// SPDX-License-Identifier:Apache-2.0

package frrk8s

import (
	"context"
	"fmt"

	frrk8sapi "github.com/metallb/frr-k8s/api/v1beta1"
	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/e2etests/pkg/ipfamily"
	"github.com/openperouter/openperouter/e2etests/pkg/k8s"
	"github.com/openperouter/openperouter/e2etests/pkg/openperouter"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
)

var (
	Namespace           = "frr-k8s-system"
	frrk8sLabelSelector = "app=frr-k8s"
)

// ConfigFromHostSession converts a HostSession object to FRRConfiguration objects.
// Returns a slice with one configuration for each IP family (IPv4 and/or IPv6).
func ConfigFromHostSession(hostsession v1alpha1.HostSession, name string, tweak ...func(*frrk8sapi.FRRConfiguration)) ([]frrk8sapi.FRRConfiguration, error) {
	if hostsession.LocalCIDR.IPv4 == "" && hostsession.LocalCIDR.IPv6 == "" {
		return nil, fmt.Errorf("LocalCIDR is required for HostSession %s", name)
	}

	var configs []frrk8sapi.FRRConfiguration
	if hostsession.LocalCIDR.IPv4 != "" {
		config, err := createFRRConfig(hostsession, name, hostsession.LocalCIDR.IPv4, ipfamily.IPv4, tweak...)
		if err != nil {
			return nil, err
		}
		configs = append(configs, config)
	}

	if hostsession.LocalCIDR.IPv6 != "" {
		config, err := createFRRConfig(hostsession, name, hostsession.LocalCIDR.IPv6, ipfamily.IPv6, tweak...)
		if err != nil {
			return nil, err
		}
		configs = append(configs, config)
	}

	if len(configs) == 0 {
		return nil, fmt.Errorf("no IPv4 or IPv6 CIDR provided")
	}

	return configs, nil
}

// ConfigFromHostSessionForIPFamily converts a HostSession object to FRRConfiguration objects for a specific IP family.
// Returns a slice with one configuration for the specified IP family.
func ConfigFromHostSessionForIPFamily(hostsession v1alpha1.HostSession, name string, family ipfamily.Family, tweak ...func(*frrk8sapi.FRRConfiguration)) (*frrk8sapi.FRRConfiguration, error) {
	if hostsession.LocalCIDR.IPv4 == "" && hostsession.LocalCIDR.IPv6 == "" {
		return nil, fmt.Errorf("LocalCIDR is required for HostSession %s", name)
	}
	if family == ipfamily.IPv4 {
		if hostsession.LocalCIDR.IPv4 == "" {
			return nil, fmt.Errorf("IPv4 CIDR not provided for HostSession %s", name)
		}
		res, err := createFRRConfig(hostsession, name, hostsession.LocalCIDR.IPv4, family, tweak...)
		if err != nil {
			return nil, err
		}
		return &res, nil
	}
	if hostsession.LocalCIDR.IPv6 == "" {
		return nil, fmt.Errorf("IPv6 CIDR not provided for HostSession %s", name)
	}
	res, err := createFRRConfig(hostsession, name, hostsession.LocalCIDR.IPv6, family, tweak...)
	if err != nil {
		return nil, err
	}

	return &res, nil
}

func WithNodeSelector(nodeSelector map[string]string) func(frrConfig *frrk8sapi.FRRConfiguration) {
	return func(frrConfig *frrk8sapi.FRRConfiguration) {
		frrConfig.Spec.NodeSelector.MatchLabels = nodeSelector
	}
}

func AdvertisePrefixes(prefixes ...string) func(frrConfig *frrk8sapi.FRRConfiguration) {
	return func(frrConfig *frrk8sapi.FRRConfiguration) {
		frrConfig.Spec.BGP.Routers[0].Neighbors[0].ToAdvertise.Allowed = frrk8sapi.AllowedOutPrefixes{
			Mode: frrk8sapi.AllowAll,
		}
		if frrConfig.Spec.BGP.Routers[0].Prefixes == nil {
			frrConfig.Spec.BGP.Routers[0].Prefixes = []string{}
		}
		frrConfig.Spec.BGP.Routers[0].Prefixes = append(frrConfig.Spec.BGP.Routers[0].Prefixes, prefixes...)
	}
}

func Pods(cs clientset.Interface) ([]*corev1.Pod, error) {
	return k8s.PodsForLabel(cs, Namespace, frrk8sLabelSelector)
}

func PodForNode(cs clientset.Interface, nodeName string) (*corev1.Pod, error) {
	pods, err := cs.CoreV1().Pods(Namespace).List(context.TODO(), metav1.ListOptions{
		FieldSelector: "spec.nodeName=" + nodeName,
		LabelSelector: frrk8sLabelSelector,
	})
	if err != nil {
		return nil, err
	}
	if len(pods.Items) == 0 {
		return nil, fmt.Errorf("no pods found with label %s for node %s", frrk8sLabelSelector, nodeName)
	}
	if len(pods.Items) > 1 {
		return nil, fmt.Errorf("multiple pods found with label %s for node %s", frrk8sLabelSelector, nodeName)
	}
	return &pods.Items[0], nil
}

func createFRRConfig(hostsession v1alpha1.HostSession, name, cidr string, family ipfamily.Family, tweak ...func(*frrk8sapi.FRRConfiguration)) (frrk8sapi.FRRConfiguration, error) {
	routerIP, err := openperouter.RouterIPFromCIDR(cidr)
	if err != nil {
		return frrk8sapi.FRRConfiguration{}, err
	}

	config := frrk8sapi.FRRConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name + string(family),
			Namespace: Namespace,
		},
		Spec: frrk8sapi.FRRConfigurationSpec{
			BGP: frrk8sapi.BGPConfig{
				Routers: []frrk8sapi.Router{
					{
						ASN: hostsession.HostASN,
						Neighbors: []frrk8sapi.Neighbor{
							{
								ASN:     hostsession.ASN,
								Address: routerIP,
								ToReceive: frrk8sapi.Receive{
									Allowed: frrk8sapi.AllowedInPrefixes{
										Mode: frrk8sapi.AllowAll,
									},
								},
							},
						},
					},
				},
			},
		},
	}
	for _, t := range tweak {
		t(&config)
	}
	return config, nil
}
