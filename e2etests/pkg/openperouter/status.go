// SPDX-License-Identifier:Apache-2.0

package openperouter

import (
	"context"

	"github.com/openperouter/openperouter/api/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func GetNodeStatus(cli client.Client, nodeName string) (*v1alpha1.RouterNodeConfigurationStatus, error) {
	status := &v1alpha1.RouterNodeConfigurationStatus{}
	err := cli.Get(context.Background(), client.ObjectKey{
		Name:      nodeName,
		Namespace: Namespace,
	}, status)
	if err != nil {
		return nil, err
	}
	return status, nil
}
