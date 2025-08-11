// SPDX-License-Identifier:Apache-2.0

package hostnetwork

import (
	"context"
	"errors"
	"fmt"

	libovsclient "github.com/ovn-kubernetes/libovsdb/client"
	"github.com/ovn-kubernetes/libovsdb/model"
	"github.com/ovn-kubernetes/libovsdb/ovsdb"
)

// Minimal models for OVS schema tables we need.
type ovsOpenVSwitch struct {
	UUID    string   `ovsdb:"_uuid"`
	Bridges []string `ovsdb:"bridges"`
}

type ovsBridge struct {
	UUID  string   `ovsdb:"_uuid"`
	Name  string   `ovsdb:"name"`
	Ports []string `ovsdb:"ports"`
}

type ovsPort struct {
	UUID       string   `ovsdb:"_uuid"`
	Name       string   `ovsdb:"name"`
	Interfaces []string `ovsdb:"interfaces"`
}

type ovsInterface struct {
	UUID string  `ovsdb:"_uuid"`
	Name string  `ovsdb:"name"`
	Type *string `ovsdb:"type"`
}

func newOVSClient(ctx context.Context) (libovsclient.Client, error) {
	dbModel, err := model.NewClientDBModel("Open_vSwitch", map[string]model.Model{
		"Open_vSwitch": &ovsOpenVSwitch{},
		"Bridge":       &ovsBridge{},
		"Port":         &ovsPort{},
		"Interface":    &ovsInterface{},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to build OVS DB model: %w", err)
	}
	c, err := libovsclient.NewOVSDBClient(dbModel, libovsclient.WithEndpoint("unix:/var/run/openvswitch/db.sock"))
	if err != nil {
		return nil, fmt.Errorf("failed to create OVSDB client: %w", err)
	}
	if err := c.Connect(ctx); err != nil {
		return nil, fmt.Errorf("failed to connect to OVSDB: %w", err)
	}
	return c, nil
}

// ensureOVSBridgeAndAttach ensures an OVS bridge exists and attaches ifaceName as a port.
func ensureOVSBridgeAndAttach(ctx context.Context, bridgeName, ifaceName string) error {
	c, err := newOVSClient(ctx)
	if err != nil {
		return err
	}
	defer c.Close()

	// Cache for indexed operations
	if _, err := c.MonitorAll(ctx); err != nil {
		return fmt.Errorf("failed to monitor OVSDB: %w", err)
	}

	// Ensure Bridge exists
	br := &ovsBridge{Name: bridgeName}
	if err := c.Get(ctx, br); err != nil {
		if !errors.Is(err, libovsclient.ErrNotFound) {
			return fmt.Errorf("failed to check if bridge %s exists: %w", bridgeName, err)
		}
		// Bridge not found, create it
		ops, err := c.Create(br)
		if err != nil {
			return fmt.Errorf("failed to build create bridge ops: %w", err)
		}
		if _, err := c.Transact(ctx, ops...); err != nil {
			return fmt.Errorf("failed to create bridge %s: %w", bridgeName, err)
		}
		// Reload bridge to get UUID
		if err := c.Get(ctx, br); err != nil {
			return fmt.Errorf("failed to get created bridge %s: %w", bridgeName, err)
		}
		// Attach bridge to root Open_vSwitch
		root := &ovsOpenVSwitch{}
		if err := c.Get(ctx, root); err != nil {
			return fmt.Errorf("failed to get Open_vSwitch root: %w", err)
		}
		mutOps, err := c.Where(root).Mutate(root, model.Mutation{
			Field:   &root.Bridges,
			Mutator: ovsdb.MutateOperationInsert,
			Value:   []string{br.UUID},
		})
		if err != nil {
			return fmt.Errorf("failed to build root mutation ops: %w", err)
		}
		if _, err := c.Transact(ctx, mutOps...); err != nil {
			return fmt.Errorf("failed to attach bridge to root: %w", err)
		}
	}

	// Ensure Interface exists
	iface := &ovsInterface{Name: ifaceName}
	if err := c.Get(ctx, iface); err != nil {
		if !errors.Is(err, libovsclient.ErrNotFound) {
			return fmt.Errorf("failed to check if interface %s exists: %w", ifaceName, err)
		}
		// Interface not found, create it
		ops, err := c.Create(iface)
		if err != nil {
			return fmt.Errorf("failed to build create interface ops: %w", err)
		}
		if _, err := c.Transact(ctx, ops...); err != nil {
			return fmt.Errorf("failed to create interface %s: %w", ifaceName, err)
		}
		if err := c.Get(ctx, iface); err != nil {
			return fmt.Errorf("failed to get created interface %s: %w", ifaceName, err)
		}
	}

	// Ensure Port exists and references Interface
	port := &ovsPort{Name: ifaceName}
	if err := c.Get(ctx, port); err != nil {
		if !errors.Is(err, libovsclient.ErrNotFound) {
			return fmt.Errorf("failed to check if port %s exists: %w", ifaceName, err)
		}
		// Port not found, create it
		port.Interfaces = []string{iface.UUID}
		ops, err := c.Create(port)
		if err != nil {
			return fmt.Errorf("failed to build create port ops: %w", err)
		}
		if _, err := c.Transact(ctx, ops...); err != nil {
			return fmt.Errorf("failed to create port %s: %w", port.Name, err)
		}
		if err := c.Get(ctx, port); err != nil {
			return fmt.Errorf("failed to get created port %s: %w", port.Name, err)
		}
	}

	// Ensure Bridge.Ports contains this port
	if err := c.Get(ctx, br); err != nil {
		return fmt.Errorf("failed to get bridge %s: %w", bridgeName, err)
	}
	already := false
	for _, p := range br.Ports {
		if p == port.UUID {
			already = true
			break
		}
	}
	if !already {
		mutOps, err := c.Where(br).Mutate(br, model.Mutation{
			Field:   &br.Ports,
			Mutator: ovsdb.MutateOperationInsert,
			Value:   []string{port.UUID},
		})
		if err != nil {
			return fmt.Errorf("failed to build bridge mutate ops: %w", err)
		}
		if _, err := c.Transact(ctx, mutOps...); err != nil {
			return fmt.Errorf("failed to attach port %s to bridge %s: %w", port.Name, bridgeName, err)
		}
	}

	return nil
}
