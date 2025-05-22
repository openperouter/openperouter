// SPDX-License-Identifier:Apache-2.0

package frrk8s

import (
	"context"
	"fmt"

	frrk8sapi "github.com/metallb/frr-k8s/api/v1beta1"
	"github.com/openperouter/openperouter/api/v1alpha1"
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

// ConfigFromVNI converts a VNI object to a FRRConfiguration object.
func ConfigFromVNI(vni v1alpha1.VNI, tweak ...func(*frrk8sapi.FRRConfiguration)) (frrk8sapi.FRRConfiguration, error) {
	routerIP, err := openperouter.RouterIPFromCIDR(vni.Spec.LocalCIDR)
	if err != nil {
		return frrk8sapi.FRRConfiguration{}, err
	}

	res := frrk8sapi.FRRConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name:      vni.Name,
			Namespace: Namespace,
		},
		Spec: frrk8sapi.FRRConfigurationSpec{
			BGP: frrk8sapi.BGPConfig{
				Routers: []frrk8sapi.Router{
					{
						ASN: *vni.Spec.HostASN,
						Neighbors: []frrk8sapi.Neighbor{
							{
								ASN:     vni.Spec.ASN,
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
	for _, tweak := range tweak {
		tweak(&res)
	}
	return res, nil
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
