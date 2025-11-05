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

func init() {
	var err error
	config := ctrl.GetConfigOrDie()
	nadClient, err = nadclientset.NewForConfig(config)
	if err != nil {
		panic(err)
	}
}

func CreateMacvlanNad(name, namespace, master string, gatewayIPs []string) (nad.NetworkAttachmentDefinition, error) {
	// Build routes based on gateway IPs (IPv4 and/or IPv6)
	var routes []string
	for _, gwIP := range gatewayIPs {
		gwIP := strings.Split(gwIP, "/")[0]
		dst := "0.0.0.0/0"
		ipFamily, err := ipfamily.ForAddresses(gwIP)
		if err != nil {
			return nad.NetworkAttachmentDefinition{}, err
		}
		if ipFamily == ipfamily.IPv6 {
			dst = "::/0"
		}

		routes = append(routes, fmt.Sprintf(`{
                "dst": "%s",
                "gw": "%s"
              }`, dst, gwIP))
	}

	routesStr := strings.Join(routes, ",\n")

	config := fmt.Sprintf(`{
      "cniVersion": "0.3.0",
      "type": "macvlan",
      "master": "%s",
      "mode": "bridge",
      "ipam": {
         "type": "static",
         "routes": [
%s
            ]
      }
    }`, master, routesStr)

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
