// SPDX-License-Identifier:Apache-2.0

package tests

import (
	"context"
	"fmt"
	"net"
	"strings"

	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/e2etests/pkg/ipfamily"
	"github.com/openperouter/openperouter/e2etests/pkg/k8s"
	"github.com/openperouter/openperouter/e2etests/pkg/openperouter"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
)

// GetPodIPByFamily returns the pod's IP address for the specified IP family (IPv4 or IPv6).
// Returns an error if no IP is found for the requested family.
//
// Example usage:
//
//	ipv4, err := GetPodIPByFamily(testPod, ipfamily.IPv4)
//	Expect(err).NotTo(HaveOccurred())
func GetPodIPByFamily(pod *corev1.Pod, family ipfamily.Family) (string, error) {
	for _, podIP := range pod.Status.PodIPs {
		ip := net.ParseIP(podIP.IP)
		if ip == nil {
			continue
		}
		if ipfamily.ForAddress(ip) == family {
			return podIP.IP, nil
		}
	}
	return "", fmt.Errorf("no %s IP found for pod %s", family, pod.Name)
}

// ExtractClientIPFromResponse extracts the client IP from a curl response string.
// This function handles different response formats including:
// - IPv6 addresses in brackets: [2001:db8::1]:port -> 2001:db8::1
// - IPv4 addresses with port: 192.168.1.1:port -> 192.168.1.1
//
// Example usage:
//
//	response, _ := podExecutor.Exec("curl", "-sS", "http://host:8090/clientip")
//	clientIP, err := ExtractClientIPFromResponse(response)
//	Expect(err).NotTo(HaveOccurred())
func ExtractClientIPFromResponse(res string) (string, error) {
	res = strings.TrimSpace(res)

	// Handle IPv6 format: [2001:db8::1]:port
	if strings.HasPrefix(res, "[") {
		endBracket := strings.Index(res, "]")
		if endBracket != -1 {
			return res[1:endBracket], nil
		}
	}

	// Handle IPv4 format: 192.168.1.1:port
	if strings.Contains(res, ":") {
		parts := strings.Split(res, ":")
		return parts[0], nil
	}

	return "", fmt.Errorf("invalid response format: no client IP found in response: %s", res)
}

// CreateTestPodWithNode creates an agnhost test pod in the specified namespace
// and retrieves the node it's running on.
//
// Returns the created pod and the node object.
//
// Example usage:
//
//	testPod, podNode, err := CreateTestPodWithNode(cs, "test-pod", "test-namespace")
//	Expect(err).NotTo(HaveOccurred())
func CreateTestPodWithNode(cs clientset.Interface, name, namespace string) (*corev1.Pod, *corev1.Node, error) {
	pod, err := k8s.CreateAgnhostPod(cs, name, namespace)
	if err != nil {
		return nil, nil, err
	}

	node, err := cs.CoreV1().Nodes().Get(context.Background(), pod.Spec.NodeName, metav1.GetOptions{})
	if err != nil {
		return nil, nil, err
	}

	return pod, node, nil
}

// GetHostSideIPFromVNI calculates the host-side IP address from a VNI's LocalCIDR
// for a specific node and IP family.
//
// Example usage:
//
//	hostIP, err := GetHostSideIPFromVNI(vniRed, podNode, ipfamily.IPv4)
//	Expect(err).NotTo(HaveOccurred())
func GetHostSideIPFromVNI(vni v1alpha1.L3VNI, node *corev1.Node, ipFamily ipfamily.Family) (string, error) {
	if vni.Spec.HostSession == nil {
		return "", fmt.Errorf("VNI %s has no host session configured", vni.Name)
	}

	var localCIDR string
	if ipFamily == ipfamily.IPv4 {
		localCIDR = vni.Spec.HostSession.LocalCIDR.IPv4
	} else {
		localCIDR = vni.Spec.HostSession.LocalCIDR.IPv6
	}

	if localCIDR == "" {
		return "", fmt.Errorf("VNI %s has no %s LocalCIDR configured", vni.Name, ipFamily)
	}

	return openperouter.HostIPFromCIDRForNode(localCIDR, node)
}

// GetHostSideIPFromPassthrough calculates the host-side IP address from a L3Passthrough's LocalCIDR
// for a specific node and IP family.
//
// Example usage:
//
//	hostIP, err := GetHostSideIPFromPassthrough(passthrough, podNode, ipfamily.IPv4)
//	Expect(err).NotTo(HaveOccurred())
func GetHostSideIPFromPassthrough(passthrough v1alpha1.L3Passthrough, node *corev1.Node, ipFamily ipfamily.Family) (string, error) {
	var localCIDR string
	if ipFamily == ipfamily.IPv4 {
		localCIDR = passthrough.Spec.HostSession.LocalCIDR.IPv4
	} else {
		localCIDR = passthrough.Spec.HostSession.LocalCIDR.IPv6
	}

	if localCIDR == "" {
		return "", fmt.Errorf("Passthrough %s has no %s LocalCIDR configured", passthrough.Name, ipFamily)
	}

	return openperouter.HostIPFromCIDRForNode(localCIDR, node)
}
