// SPDX-License-Identifier:Apache-2.0

package openperouter

import (
	"fmt"
	"net"
	"strconv"

	gocidr "github.com/apparentlymart/go-cidr/cidr"
	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/e2etests/pkg/ipfamily"
	corev1 "k8s.io/api/core/v1"
)

const nodeIndexAnnotation = "openpe.io/nodeindex"

func GetVtepIPForNode(tunnelEndpoint *v1alpha1.TunnelEndpointConfig, node *corev1.Node, family ipfamily.Family) (string, error) {
	nodeIndex, err := nodeIndexFromAnnotation(node)
	if err != nil {
		return "", err
	}
	if tunnelEndpoint == nil {
		return "", fmt.Errorf("invalid nil tunnel endpoint")
	}

	matching := ipfamily.CIDRsForFamily(tunnelEndpoint.CIDRs, family)
	if len(matching) == 0 {
		return "", fmt.Errorf("no %s CIDR found in %v", family, tunnelEndpoint.CIDRs)
	}
	if len(matching) > 1 {
		return "", fmt.Errorf("multiple %s CIDRs found in %v", family, tunnelEndpoint.CIDRs)
	}

	_, vtepCIDR, err := net.ParseCIDR(matching[0])
	if err != nil {
		return "", fmt.Errorf("failed to parse cidr %s: %w", matching[0], err)
	}

	ip, err := gocidr.Host(vtepCIDR, nodeIndex)
	if err != nil {
		return "", fmt.Errorf("failed to get index %d from cidr %s: %w", nodeIndex, vtepCIDR, err)
	}

	maskSize := 32
	maskBits := 32
	if family == ipfamily.IPv6 {
		maskSize = 128
		maskBits = 128
	}
	netip := net.IPNet{
		IP:   ip,
		Mask: net.CIDRMask(maskSize, maskBits),
	}
	return netip.String(), nil
}

func GetVtepIPv4ForNode(tunnelEndpoint *v1alpha1.TunnelEndpointConfig, node *corev1.Node) (string, error) {
	return GetVtepIPForNode(tunnelEndpoint, node, ipfamily.IPv4)
}

func nodeIndexFromAnnotation(node *corev1.Node) (int, error) {
	if node.Annotations == nil || node.Annotations[nodeIndexAnnotation] == "" {
		return 0, fmt.Errorf("no index for node %s", node.Name)
	}
	nodeIndex, err := strconv.Atoi(node.Annotations[nodeIndexAnnotation])
	if err != nil {
		return 0, fmt.Errorf("non int index %s for node %s: %w", node.Annotations[nodeIndexAnnotation], node.Name, err)
	}
	return nodeIndex, nil
}
