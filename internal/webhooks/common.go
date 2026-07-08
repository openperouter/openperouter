// SPDX-License-Identifier:Apache-2.0

package webhooks

import (
	"fmt"
	"log/slog"

	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/internal/conversion"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var (
	Logger                  *slog.Logger
	WebhookClient           client.Reader
	DatapathConfigValidator conversion.DatapathConfigValidator
)

// setupFakeWebhookClient creates a new fake client from the provided slice of client.Object.
func setupFakeWebhookClient(initObjects []client.Object) (client.Reader, error) {
	scheme := runtime.NewScheme()
	if err := v1.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("failed to add scheme: %w", err)
	}
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("failed to add scheme: %w", err)
	}

	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(initObjects...).
		Build(), nil
}

// objectsFromResources converts the provided slice of CRs to a slice of client.Object.
func objectsFromResources[T client.Object](objs []T) []client.Object {
	output := make([]client.Object, 0, len(objs))
	for _, obj := range objs {
		output = append(output, obj)
	}
	return output
}
