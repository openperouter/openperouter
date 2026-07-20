// SPDX-License-Identifier:Apache-2.0

package infra

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/e2etests/pkg/executor"
	"github.com/openperouter/openperouter/e2etests/pkg/openperouter"
)

const (
	// CNIUnderlayInterface is the name of the interface the CNI plugin
	// creates inside the router netns for CNI provisioned underlays.
	CNIUnderlayInterface = "underlay0"

	// CNIUnderlayASN is the local AS number of the CNI provisioned underlays.
	CNIUnderlayASN = 64514
)

// CNIUnderlayNeighborIP returns the static IP assigned to the CNI-provisioned
// underlay interface of the i-th node, on the subnet shared with leafkind1.
// The .10+ addresses are clear of the ones the dev environment assigns
// (leaf .2, nodes .3/.4).
func CNIUnderlayNeighborIP(nodeIndex int) string {
	return fmt.Sprintf("192.168.11.%d", 10+nodeIndex)
}

// CNIUnderlaysForNodes builds one node-scoped Underlay per node, whose
// interface is provisioned by the macvlan CNI plugin on top of toswitch1
// (which stays in the host netns) with a per-node static IP. The node index
// used for addressing is the position in the given slice, so callers must
// pass a deterministic order.
func CNIUnderlaysForNodes(nodes []corev1.Node, ifName string) []v1alpha1.Underlay {
	underlays := make([]v1alpha1.Underlay, 0, len(nodes))
	for i, node := range nodes {
		underlays = append(underlays, cniUnderlayForNode(i, node, ifName))
	}
	return underlays
}

// cniUnderlayForNode builds the node-scoped Underlay of the i-th node for
// the CNI provisioned underlay flavor.
func cniUnderlayForNode(nodeIndex int, node corev1.Node, ifName string) v1alpha1.Underlay {
	rawConfig := fmt.Sprintf(`{
  "cniVersion": "1.0.0",
  "name": "macvlan-underlay",
  "plugins": [
    {
      "type": "macvlan",
      "master": "toswitch1",
      "mode": "bridge",
      "ipam": {
        "type": "static",
        "addresses": [
          {"address": "%s/24"}
        ]
      }
    }
  ]
}`, CNIUnderlayNeighborIP(nodeIndex))

	return v1alpha1.Underlay{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("underlay-cni-%d", nodeIndex),
			Namespace: openperouter.Namespace,
		},
		Spec: v1alpha1.UnderlaySpec{
			ASN: CNIUnderlayASN,
			NodeSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"kubernetes.io/hostname": node.Name},
			},
			Interfaces: []v1alpha1.UnderlayInterface{
				{
					Type: v1alpha1.UnderlayInterfaceTypeCNIDevice,
					CNIDevice: &v1alpha1.CNIDevice{
						Type:          v1alpha1.CNIConfigTypeRawConfig,
						RawConfig:     &apiextensionsv1.JSON{Raw: []byte(rawConfig)},
						InterfaceName: new(ifName),
					},
				},
			},
			Neighbors: []v1alpha1.Neighbor{
				{
					ASN:     new(int64(64512)),
					Address: new("192.168.11.2"),
				},
			},
			TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{
				CIDRs: []string{"100.65.0.0/24"},
			},
		},
	}
}

// ConfigureLeafKind1ForCNIUnderlay points leafkind1 at the static addresses
// of the CNI provisioned interfaces, since the standard leaf configuration
// only knows the node addresses. There are no sessions on the leafkind2 side,
// which keeps the standard configuration.
func ConfigureLeafKind1ForCNIUnderlay(nodes []corev1.Node) error {
	neighbors := make([]Neighbor, 0, len(nodes))
	for i := range nodes {
		neighbors = append(neighbors, Neighbor{ID: CNIUnderlayNeighborIP(i)})
	}
	return LeafKind1Config.Configure(LeafKindConfiguration{Neighbors: neighbors})
}

// DHCPCNIUnderlaysForNodes builds one node-scoped Underlay per node, whose
// interface is provisioned by the macvlan CNI plugin on top of toswitch1
// with DHCP IPAM. Addresses are acquired from the DHCP server (dhcp1) on
// the leafkind1-sw bridge segment.
func DHCPCNIUnderlaysForNodes(nodes []corev1.Node, ifName string) []v1alpha1.Underlay {
	underlays := make([]v1alpha1.Underlay, 0, len(nodes))
	for i, node := range nodes {
		underlays = append(underlays, dhcpCNIUnderlayForNode(i, node, ifName))
	}
	return underlays
}

func dhcpCNIUnderlayForNode(nodeIndex int, node corev1.Node, ifName string) v1alpha1.Underlay {
	rawConfig := `{
  "cniVersion": "1.0.0",
  "name": "macvlan-underlay",
  "plugins": [
    {
      "type": "macvlan",
      "master": "toswitch1",
      "mode": "bridge",
      "ipam": {
        "type": "dhcp"
      }
    }
  ]
}`
	return v1alpha1.Underlay{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("underlay-dhcp-%d", nodeIndex),
			Namespace: openperouter.Namespace,
		},
		Spec: v1alpha1.UnderlaySpec{
			ASN: CNIUnderlayASN,
			NodeSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"kubernetes.io/hostname": node.Name},
			},
			Interfaces: []v1alpha1.UnderlayInterface{
				{
					Type: v1alpha1.UnderlayInterfaceTypeCNIDevice,
					CNIDevice: &v1alpha1.CNIDevice{
						Type:          v1alpha1.CNIConfigTypeRawConfig,
						RawConfig:     &apiextensionsv1.JSON{Raw: []byte(rawConfig)},
						InterfaceName: new(ifName),
					},
				},
			},
			Neighbors: []v1alpha1.Neighbor{
				{
					ASN:     new(int64(64512)),
					Address: new("192.168.11.2"),
				},
			},
			TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{
				CIDRs: []string{"100.65.0.0/24"},
			},
		},
	}
}

// ConfigureLeafKind1ForDHCPUnderlay discovers the DHCP-assigned addresses on
// each node's CNI interface in the perouter netns and configures leafkind1 to
// peer with them. Call this after the underlay is provisioned and the
// interfaces have acquired their leases.
func ConfigureLeafKind1ForDHCPUnderlay(nodes []corev1.Node, ifName string) error {
	neighbors := make([]Neighbor, 0, len(nodes))
	for _, node := range nodes {
		ip, err := openperouter.InterfaceIPv4InNetns(node.Name, ifName, openperouter.NamedNetns)
		if err != nil {
			return fmt.Errorf("discovering DHCP address on %s: %w", node.Name, err)
		}
		neighbors = append(neighbors, Neighbor{ID: ip})
	}
	return LeafKind1Config.Configure(LeafKindConfiguration{Neighbors: neighbors})
}

// DHCPNeighborIP discovers the IPv4 address assigned by DHCP on the CNI
// interface inside the perouter netns of nodeName.
func DHCPNeighborIP(nodeName, ifName string) (string, error) {
	return openperouter.InterfaceIPv4InNetns(nodeName, ifName, openperouter.NamedNetns)
}

const dhcpServerContainer = ClabPrefix + "dhcp1"

var (
	ErrLeaseNotFound = errors.New("no lease found")
	ErrLeaseExpired  = errors.New("lease expired")
)

// DHCPServerLeaseValid checks the dnsmasq server's leases file for the
// given IP address. It returns nil when a valid (non-expired) lease exists,
// ErrLeaseNotFound when no lease entry is present, and ErrLeaseExpired
// when the lease exists but its expiry is in the past.
func DHCPServerLeaseValid(ip string) error {
	exec := executor.ForContainer(dhcpServerContainer)
	out, err := exec.Exec("cat", "/var/lib/misc/dnsmasq.leases")
	if err != nil {
		return fmt.Errorf("reading dnsmasq leases: %w", err)
	}
	now := time.Now().Unix()
	for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		if fields[2] != ip {
			continue
		}
		expiry, err := strconv.ParseInt(fields[0], 10, 64)
		if err != nil {
			return fmt.Errorf("parsing lease expiry %q: %w", fields[0], err)
		}
		if expiry <= now {
			return ErrLeaseExpired
		}
		return nil
	}
	return ErrLeaseNotFound
}
