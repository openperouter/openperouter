// SPDX-License-Identifier:Apache-2.0

package hostnetwork

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
)

const (
	UnderlayLoopback = "lound"
)

type LoopbackParams struct {
	TargetNS string `json:"target_ns"`
	VtepIP   string `json:"vtep_ip"`
}

func SetupLoopback(ctx context.Context, params LoopbackParams) error {
	slog.DebugContext(ctx, "setup loopback", "targetNS", params.TargetNS, "vtepIP", params.VtepIP)
	defer slog.DebugContext(ctx, "setup loopback done")

	ns, err := netns.GetFromName(params.TargetNS)
	if err != nil {
		return fmt.Errorf("SetupLoopback: Failed to find network namespace %s: %w", params.TargetNS, err)
	}
	defer func() {
		if err := ns.Close(); err != nil {
			slog.Error("failed to close namespace", "namespace", params.TargetNS, "error", err)
		}
	}()

	if err := createLoopback(ctx, ns, params.VtepIP); err != nil {
		return err
	}
	return nil
}

func createLoopback(ctx context.Context, ns netns.NsHandle, vtepIP string) error {
	slog.DebugContext(ctx, "setup underlay loopback", "step", "creating loopback interface")
	defer slog.DebugContext(ctx, "setup underlay loopback", "step", "loopback interface created")

	if err := inNamespace(ns, func() error {
		loopback, err := netlink.LinkByName(UnderlayLoopback)
		if errors.As(err, &netlink.LinkNotFoundError{}) {
			slog.DebugContext(ctx, "setup underlay loopback", "step", "creating loopback interface")
			loopback = &netlink.Dummy{LinkAttrs: netlink.LinkAttrs{Name: UnderlayLoopback}}
			if err := netlink.LinkAdd(loopback); err != nil {
				return fmt.Errorf("assignVTEPToLoopback: failed to create loopback underlay - %w", err)
			}
		}

		err = assignIPToInterface(loopback, vtepIP)
		if err != nil {
			return err
		}

		return nil
	}); err != nil {
		return err
	}

	return nil
}
