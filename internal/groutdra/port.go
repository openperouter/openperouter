// SPDX-License-Identifier:Apache-2.0

package groutdra

import (
	"context"
	"fmt"
	"net"

	"github.com/zeeke/grout-cni/pkg/groutapi"
)

// portManager owns only the grout-facing part of a DRA allocation.
type portManager interface {
	Create(context.Context, string, string, string) error
	Running(context.Context, string) (bool, error)
	Delete(context.Context, string) error
}

type apiPorts struct {
	socketPath string
}

func (p apiPorts) Create(ctx context.Context, name, socketPath, gatewayCIDR string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	client, err := groutapi.DialContext(ctx, p.socketPath)
	if err != nil {
		return err
	}
	defer client.Close()
	iface, err := findPort(client, name)
	if err != nil {
		return err
	}
	if iface == nil {
		added, err := client.InterfaceAdd(groutapi.InterfaceAddRequest{
			Name: name, Type: groutapi.InterfaceTypePort, VhostUserPath: socketPath,
			VhostClient: true, RXQueues: 1, QueueSize: 4096,
		})
		if err != nil {
			return fmt.Errorf("creating grout vhost port %s: %w", name, err)
		}
		iface = &groutapi.InterfaceInfo{ID: added.IfaceID, Name: name}
	}
	if gatewayCIDR == "" {
		return nil
	}
	ip, network, err := net.ParseCIDR(gatewayCIDR)
	if err != nil {
		return fmt.Errorf("invalid gateway %q: %w", gatewayCIDR, err)
	}
	network.IP = ip
	if err := client.AddressAdd(groutapi.AddressAddRequest{IfaceID: iface.ID, Address: *network, ExistOK: true}); err != nil {
		return fmt.Errorf("assigning %s to %s: %w", gatewayCIDR, name, err)
	}
	return nil
}

func (p apiPorts) Running(ctx context.Context, name string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	client, err := groutapi.DialContext(ctx, p.socketPath)
	if err != nil {
		return false, err
	}
	defer client.Close()
	iface, err := findPort(client, name)
	if err != nil || iface == nil {
		return false, err
	}
	return iface.State&groutapi.InterfaceStateRunning != 0, nil
}

func (p apiPorts) Delete(ctx context.Context, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	client, err := groutapi.DialContext(ctx, p.socketPath)
	if err != nil {
		return err
	}
	defer client.Close()
	iface, err := findPort(client, name)
	if err != nil || iface == nil {
		return err
	}
	if err := client.InterfaceDel(groutapi.InterfaceDelRequest{IfaceID: iface.ID}); err != nil {
		return fmt.Errorf("deleting grout vhost port %s: %w", name, err)
	}
	return nil
}

func findPort(client *groutapi.Client, name string) (*groutapi.InterfaceInfo, error) {
	ports, err := client.InterfaceList(groutapi.InterfaceListRequest{Type: groutapi.InterfaceTypePort})
	if err != nil {
		return nil, fmt.Errorf("listing grout ports: %w", err)
	}
	for _, port := range ports {
		if port.Name == name {
			return &port, nil
		}
	}
	return nil, nil
}
