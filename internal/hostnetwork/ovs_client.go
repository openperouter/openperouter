// SPDX-License-Identifier:Apache-2.0

package hostnetwork

import (
	"context"
	"fmt"

	openpeerrors "github.com/openperouter/openperouter/internal/errors"
	"github.com/openperouter/openperouter/internal/ovsmodel"
	libovsclient "github.com/ovn-kubernetes/libovsdb/client"
)

const (
	defaultOVSSocketPath = "unix:/var/run/openvswitch/db.sock"
)

var (
	OVSSocketPath = defaultOVSSocketPath
)

type OVSClient struct {
	client libovsclient.Client
}

func (ovs *OVSClient) Close() {
	if ovs.client != nil {
		ovs.client.Close()
		ovs.client = nil
	}
}

func (ovs *OVSClient) Get(ctx context.Context) (libovsclient.Client, error) {
	if ovs.client == nil {
		var err error
		ovs.client, err = newOVSClient(ctx)
		if err != nil {
			return nil, err
		}
	}
	return ovs.client, nil
}

// NewOVSClient creates a new OVS database client
func newOVSClient(ctx context.Context) (libovsclient.Client, error) {
	dbModel, err := ovsmodel.FullDatabaseModel()
	if err != nil {
		return nil, fmt.Errorf("failed to build OVS DB model: %w", err)
	}

	ovs, err := libovsclient.NewOVSDBClient(
		dbModel,
		libovsclient.WithEndpoint(OVSSocketPath),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create OVSDB client: %w", err)
	}

	if err := ovs.Connect(ctx); err != nil {
		ovs.Close()
		return nil, fmt.Errorf("%w: failed to connect to OVSDB: %w", openpeerrors.ErrTransient, err)
	}

	// Cache for indexed operations
	if _, err := ovs.Monitor(ctx,
		ovs.NewMonitor(
			libovsclient.WithTable(&ovsmodel.OpenvSwitch{}),
			libovsclient.WithTable(&ovsmodel.Bridge{}),
			libovsclient.WithTable(&ovsmodel.Port{}),
			libovsclient.WithTable(&ovsmodel.Interface{}),
		),
	); err != nil {
		ovs.Close()
		return nil, fmt.Errorf("failed to setup monitor: %w", err)
	}

	return ovs, nil
}
