// SPDX-License-Identifier:Apache-2.0

package routerconfiguration

import (
	"context"
)

type RouterProvider interface {
	New() (Router, error)
	NodeIndex(ctx context.Context) (int, error)
}

type Router interface {
	TargetNS(ctx context.Context) (string, error)
	CanReconcile(ctx context.Context) (bool, error)
}
