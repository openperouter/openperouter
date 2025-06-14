// SPDX-License-Identifier:Apache-2.0

package k8s

import (
	"context"
	"fmt"

	nad "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	nadclientset "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/client/clientset/versioned"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
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

func CreateMacvlanNad(name, namespace, master, gatewayIP string) (nad.NetworkAttachmentDefinition, error) {
	config := fmt.Sprintf(`{
      "cniVersion": "0.3.0",
      "type": "macvlan",
      "master": "%s",
      "mode": "bridge",
      "ipam": {
         "type": "static",
         "routes": [
              {
                "dst": "0.0.0.0/0",
                "gw": "%s"
              }
            ]
      }
    }`, master, gatewayIP)

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
