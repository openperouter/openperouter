// SPDX-License-Identifier:Apache-2.0

package tests

import (
	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/e2etests/pkg/frrk8s"
	"github.com/openperouter/openperouter/e2etests/pkg/ipfamily"
	"github.com/openperouter/openperouter/e2etests/pkg/k8s"
	frrk8sapi "github.com/metallb/frr-k8s/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
	clientset "k8s.io/client-go/kubernetes"
)

// GetFRRK8sPodsAndDump retrieves all FRR-k8s pods and dumps their information.
// This is commonly used at the beginning of tests to get the FRR-k8s pods
// and display their status.
//
// Example usage:
//
//	BeforeEach(func() {
//	    frrk8sPods, err := GetFRRK8sPodsAndDump(cs)
//	    Expect(err).NotTo(HaveOccurred())
//	})
func GetFRRK8sPodsAndDump(cs clientset.Interface) ([]*corev1.Pod, error) {
	frrk8sPods, err := frrk8s.Pods(cs)
	if err != nil {
		return nil, err
	}

	DumpPods("FRRK8s pods", frrk8sPods)
	return frrk8sPods, nil
}

// AdvertisePodIPsToVNI creates FRR configurations to advertise all IPs of a pod
// to a specific L3VNI. It creates one configuration per IP address family (IPv4/IPv6)
// and ensures the configuration is applied to the node where the pod is running.
//
// The function:
// - Iterates through all pod IPs
// - Creates appropriate CIDR suffix (/32 for IPv4, /128 for IPv6)
// - Generates FRR configuration for each IP family
// - Uses node selector to target the specific node
//
// Example usage:
//
//	nodeSelector := k8s.NodeSelectorForPod(testPod)
//	configs, err := AdvertisePodIPsToVNI(testPod, vniRed, nodeSelector)
//	Expect(err).NotTo(HaveOccurred())
func AdvertisePodIPsToVNI(pod *corev1.Pod, vni v1alpha1.L3VNI, nodeSelector map[string]string) ([]frrk8sapi.FRRConfiguration, error) {
	res := []frrk8sapi.FRRConfiguration{}

	for _, podIP := range pod.Status.PodIPs {
		cidrSuffix := "/32"
		ipFamily, err := ipfamily.ForAddresses(podIP.IP)
		if err != nil {
			return nil, err
		}
		if ipFamily == ipfamily.IPv6 {
			cidrSuffix = "/128"
		}

		config, err := frrk8s.ConfigFromHostSessionForIPFamily(
			*vni.Spec.HostSession,
			vni.Name,
			ipFamily,
			frrk8s.WithNodeSelector(nodeSelector),
			frrk8s.AdvertisePrefixes(podIP.IP+cidrSuffix),
		)
		if err != nil {
			return nil, err
		}
		res = append(res, *config)
	}

	return res, nil
}

// AdvertisePodIPsToPassthrough creates FRR configurations to advertise all IPs of a pod
// to a specific L3Passthrough. Similar to AdvertisePodIPsToVNI, but for passthrough configs.
//
// The function:
// - Iterates through all pod IPs
// - Creates appropriate CIDR suffix (/32 for IPv4, /128 for IPv6)
// - Generates FRR configuration for each IP family
// - Uses node selector to target the specific node
//
// Example usage:
//
//	nodeSelector := k8s.NodeSelectorForPod(testPod)
//	configs, err := AdvertisePodIPsToPassthrough(testPod, passthrough, nodeSelector)
//	Expect(err).NotTo(HaveOccurred())
func AdvertisePodIPsToPassthrough(pod *corev1.Pod, passthrough v1alpha1.L3Passthrough, nodeSelector map[string]string) ([]frrk8sapi.FRRConfiguration, error) {
	res := []frrk8sapi.FRRConfiguration{}

	for _, podIP := range pod.Status.PodIPs {
		cidrSuffix := "/32"
		ipFamily, err := ipfamily.ForAddresses(podIP.IP)
		if err != nil {
			return nil, err
		}
		if ipFamily == ipfamily.IPv6 {
			cidrSuffix = "/128"
		}

		config, err := frrk8s.ConfigFromHostSessionForIPFamily(
			passthrough.Spec.HostSession,
			passthrough.Name,
			ipFamily,
			frrk8s.WithNodeSelector(nodeSelector),
			frrk8s.AdvertisePrefixes(podIP.IP+cidrSuffix),
		)
		if err != nil {
			return nil, err
		}
		res = append(res, *config)
	}

	return res, nil
}

// GetCIDRSuffixForIPFamily returns the appropriate CIDR suffix for an IP family.
// Returns "/32" for IPv4 and "/128" for IPv6.
//
// Example usage:
//
//	suffix := GetCIDRSuffixForIPFamily(ipfamily.IPv4) // returns "/32"
func GetCIDRSuffixForIPFamily(ipFamily ipfamily.Family) string {
	if ipFamily == ipfamily.IPv6 {
		return "/128"
	}
	return "/32"
}

// GetNodeSelectorForPod is a convenience wrapper around k8s.NodeSelectorForPod.
// It returns a node selector map that targets the node where the pod is running.
//
// Example usage:
//
//	nodeSelector := GetNodeSelectorForPod(testPod)
func GetNodeSelectorForPod(pod *corev1.Pod) map[string]string {
	return k8s.NodeSelectorForPod(pod)
}
