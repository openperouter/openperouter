// SPDX-License-Identifier:Apache-2.0

package webhooks

import (
	"context"
	"errors"
	"log/slog"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	Logger        *slog.Logger
	WebhookClient client.Reader
)

func getNodes() ([]corev1.Node, error) {
	nodeList := &corev1.NodeList{}
	err := WebhookClient.List(context.Background(), nodeList, &client.ListOptions{})
	if err != nil {
		return nil, errors.Join(err, errors.New("failed to get existing Node objects"))
	}
	return nodeList.Items, nil
}
