// SPDX-License-Identifier:Apache-2.0

package webhooks

import (
	"log/slog"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	Logger        *slog.Logger
	WebhookClient client.Reader
)
