// SPDX-License-Identifier:Apache-2.0

package k8s

import (
	"context"
	"fmt"
	"strings"

	nad "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	nadclientset "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/client/clientset/versioned"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/openperouter/openperouter/e2etests/pkg/ipfamily"
)

var nadClient *nadclientset.Clientset

func initNADClient() error {
	if nadClient != nil {
		return nil
	}
	config, err := ctrl.GetConfig()
	if err != nil {
		return fmt.Errorf("failed to load kubeconfig for NAD client: %w", err)
	}
	nadClient, err = nadclientset.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("failed to create NAD client: %w", err)
	}
	return nil
}

func CreateMacvlanNad(name, namespace, master string, gatewayIPs []string) (nad.NetworkAttachmentDefinition, error) {
	ipam, err := staticIPAM(gatewayIPs)
	if err != nil {
		return nad.NetworkAttachmentDefinition{}, err
	}
	config := fmt.Sprintf(`{
      "cniVersion": "0.3.0",
      "type": "macvlan",
      "master": "%s",
      "mode": "bridge",
      "ipam": %s
    }`, master, ipam)

	return createNad(name, namespace, config)
}

// CreateBridgeNad attaches the pod to an existing bridge as an access port of
// the given VLAN: the plugin creates a veth pair, enslaves the host side and
// makes it pvid untagged for that VLAN. A vlan of 0 leaves the port untagged.
func CreateBridgeNad(name, namespace, bridgeName string, vlan int, gatewayIPs []string) (nad.NetworkAttachmentDefinition, error) {
	ipam, err := staticIPAM(gatewayIPs)
	if err != nil {
		return nad.NetworkAttachmentDefinition{}, err
	}
	vlanConfig := ""
	if vlan > 0 {
		vlanConfig = fmt.Sprintf(",\n      \"vlan\": %d", vlan)
	}
	config := fmt.Sprintf(`{
      "cniVersion": "0.3.0",
      "type": "bridge",
      "bridge": "%s"%s,
      "ipam": %s
    }`, bridgeName, vlanConfig, ipam)

	return createNad(name, namespace, config)
}

func createNad(name, namespace, config string) (nad.NetworkAttachmentDefinition, error) {
	if err := initNADClient(); err != nil {
		return nad.NetworkAttachmentDefinition{}, err
	}
	n := nad.NetworkAttachmentDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: nad.NetworkAttachmentDefinitionSpec{
			Config: config,
		},
	}
	if _, err := nadClient.K8sCniCncfIoV1().NetworkAttachmentDefinitions(namespace).Create(context.Background(), &n, metav1.CreateOptions{}); err != nil {
		return nad.NetworkAttachmentDefinition{}, fmt.Errorf("failed to create nad %s: %w", name, err)
	}
	return n, nil
}

// staticIPAM renders the static IPAM block, routing the default gateway of
// each address family through the given gateway addresses.
func staticIPAM(gatewayIPs []string) (string, error) {
	var routes []string
	for _, gwIP := range gatewayIPs {
		gwIP := strings.Split(gwIP, "/")[0]
		dst := "0.0.0.0/0"
		ipFamily, err := ipfamily.ForAddresses(gwIP)
		if err != nil {
			return "", err
		}
		if ipFamily == ipfamily.IPv6 {
			dst = "::/0"
		}
		routes = append(routes, fmt.Sprintf(`{
                "dst": "%s",
                "gw": "%s"
              }`, dst, gwIP))
	}
	return fmt.Sprintf(`{
         "type": "static",
         "routes": [
%s
            ]
      }`, strings.Join(routes, ",\n")), nil
}
