// SPDX-License-Identifier:Apache-2.0

package openperouter

import (
	"fmt"
	"net"
	"strconv"

	gocidr "github.com/apparentlymart/go-cidr/cidr"
	corev1 "k8s.io/api/core/v1"
)

func RouterIPFromCIDR(cidr string) (string, error) {
	_, routerIPCidr, err := net.ParseCIDR(cidr)
	if err != nil {
		return "", fmt.Errorf("failed to parse cidr %s: %w", cidr, err)
	}
	ip, err := gocidr.Host(routerIPCidr, 0)
	if err != nil {
		return "", fmt.Errorf("failed to get index %d from cidr %s", 1, routerIPCidr)
	}
	return ip.String(), nil
}

func HostIPFromCIDRForNode(cidr string, node *corev1.Node) (string, error) {
	if node.Annotations == nil ||
		node.Annotations[nodeIndexAnnotation] == "" {
		return "", fmt.Errorf("no index for node %s", node.Name)
	}
	nodeIndex, err := strconv.Atoi(node.Annotations[nodeIndexAnnotation])
	if err != nil {
		return "", fmt.Errorf("non int index %s for node %s", node.Annotations[nodeIndexAnnotation], node.Name)
	}
	_, hostCIDR, err := net.ParseCIDR(cidr)
	if err != nil {
		return "", fmt.Errorf("failed to parse cidr %s: %w", cidr, err)
	}

	ip, err := gocidr.Host(hostCIDR, nodeIndex+1)
	if err != nil {
		return "", fmt.Errorf("failed to get index %d from cidr %s", nodeIndex, hostCIDR)
	}
	return ip.String(), nil
}
