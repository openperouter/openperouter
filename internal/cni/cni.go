// SPDX-License-Identifier:Apache-2.0

// Package cni provisions interfaces inside a network namespace by invoking CNI
// plugins programmatically (via containernetworking/cni libcni). It is used to
// create the underlay interface directly in the persistent router netns from a
// NetworkAttachmentDefinition, with the per-node IP supplied to the plugin via
// the CNI "ips" capability (runtimeConfig). The NAD must declare
// `"capabilities": {"ips": true}` and an IPAM that honours it (e.g. static).
package cni

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/containernetworking/cni/libcni"
	"github.com/containernetworking/cni/pkg/invoke"
	cnitypes "github.com/containernetworking/cni/pkg/types"
	cnicurrent "github.com/containernetworking/cni/pkg/types/100"
)

// ensureIPAMTimeout bounds a single EnsureIPAM invocation. The dhcp daemon's
// Allocate replies immediately when it already maintains the lease (it just
// pokes the maintenance loop), but if that loop is mid-retry against an
// unreachable server the poke can block; the timeout keeps a reconcile from
// stalling on it.
const ensureIPAMTimeout = 30 * time.Second

// Params holds everything needed to ADD or DEL a CNI network into a netns.
type Params struct {
	// Config is the CNI conf or conflist JSON (the NAD's .spec.config, passed
	// through unchanged).
	Config []byte
	// NetNS is the target network namespace path, e.g. /var/run/netns/perouter.
	NetNS string
	// IfName is the interface name to create inside the netns.
	IfName string
	// ContainerID is a stable identifier for the attachment (used by the libcni
	// result cache so DEL/CHECK can find the cached result).
	ContainerID string
	// BinDirs are the directories searched for CNI plugin binaries.
	BinDirs []string
	// CacheDir is where libcni stores the result cache; it must be stable across
	// controller restarts so DEL works.
	CacheDir string
	// Addresses are the IPs (CIDR notation) to assign, passed to the IPAM plugin
	// via the CNI "ips" capability (the NAD must declare `capabilities.ips=true`).
	Addresses []string
}

// Add invokes CNI ADD for the configured network into Params.NetNS and returns
// the CNI result. It is the programmatic equivalent of `cnitool add`.
func Add(ctx context.Context, p Params) (cnitypes.Result, error) {
	confList, err := confList(p.Config)
	if err != nil {
		return nil, err
	}
	cni := libcni.NewCNIConfigWithCacheDir(p.BinDirs, p.CacheDir, nil)
	result, err := cni.AddNetworkList(ctx, confList, runtimeConf(p))
	if err != nil {
		return nil, fmt.Errorf("cni add %q into %s: %w", confList.Name, p.NetNS, err)
	}
	return result, nil
}

// Del invokes CNI DEL for the configured network from Params.NetNS.
func Del(ctx context.Context, p Params) error {
	confList, err := confList(p.Config)
	if err != nil {
		return err
	}
	cni := libcni.NewCNIConfigWithCacheDir(p.BinDirs, p.CacheDir, nil)
	if err := cni.DelNetworkList(ctx, confList, runtimeConf(p)); err != nil {
		return fmt.Errorf("cni del %q from %s: %w", confList.Name, p.NetNS, err)
	}
	return nil
}

// DelCached deletes every CNI attachment cached under containerID using the
// libcni result cache, which stores the config and netns recorded at ADD time.
// This lets the controller tear an interface down even after its defining
// config is gone (e.g. the underlay or its NAD was removed). It is idempotent:
// no cached attachments means nothing to do.
func DelCached(ctx context.Context, binDirs []string, cacheDir, containerID string) error {
	cni := libcni.NewCNIConfigWithCacheDir(binDirs, cacheDir, nil)
	attachments, err := cni.GetCachedAttachments(containerID)
	if err != nil {
		return fmt.Errorf("failed to read cni cache for %q: %w", containerID, err)
	}
	for _, a := range attachments {
		confList, err := confList(a.Config)
		if err != nil {
			return fmt.Errorf("failed to parse cached cni config for network %q: %w", a.Network, err)
		}
		rt := &libcni.RuntimeConf{
			ContainerID:    a.ContainerID,
			NetNS:          a.NetNS,
			IfName:         a.IfName,
			CapabilityArgs: a.CapabilityArgs,
		}
		if err := cni.DelNetworkList(ctx, confList, rt); err != nil {
			return fmt.Errorf("cni del %q (%s) from %s: %w", a.Network, a.IfName, a.NetNS, err)
		}
	}
	return nil
}

// IPAMType returns the IPAM plugin type ("ipam.type") declared in a CNI config.
// For a conflist it returns the type of the first plugin that declares an IPAM
// section. It returns an empty string when no IPAM is configured.
func IPAMType(config []byte) (string, error) {
	_, ipam, err := ipamPluginConfig(config)
	if err != nil {
		return "", err
	}
	return ipam, nil
}

// EnsureIPAM invokes the IPAM plugin (e.g. dhcp) selected by config for an ADD,
// directly and out-of-band of the parent/chained plugin, to (re)establish IPAM
// state for an interface that already exists in the netns. For the dhcp IPAM
// this makes the daemon re-acquire and resume maintaining the lease (Allocate is
// idempotent by clientID = containerID/name/ifName), which is what lets a DHCP
// underlay survive a controller/daemon restart. It returns the address the IPAM
// plugin assigned (CIDR form), so the caller can reconcile interface drift.
//
// netNS/containerID/ifName MUST match the values used for the original ADD, and
// config MUST carry the same network "name", so the derived clientID is stable.
func EnsureIPAM(ctx context.Context, binDirs []string, netNS, containerID, ifName string, config []byte) (net.IPNet, error) {
	netconf, ipamType, err := ipamPluginConfig(config)
	if err != nil {
		return net.IPNet{}, err
	}
	if ipamType == "" {
		return net.IPNet{}, fmt.Errorf("no ipam configured in CNI config")
	}
	pluginPath, err := invoke.FindInPath(ipamType, binDirs)
	if err != nil {
		return net.IPNet{}, fmt.Errorf("failed to find ipam plugin %q: %w", ipamType, err)
	}

	ctx, cancel := context.WithTimeout(ctx, ensureIPAMTimeout)
	defer cancel()

	args := &invoke.Args{
		Command:     "ADD",
		ContainerID: containerID,
		NetNS:       netNS,
		IfName:      ifName,
		Path:        strings.Join(binDirs, string(os.PathListSeparator)),
	}
	res, err := invoke.ExecPluginWithResult(ctx, pluginPath, netconf, args, nil)
	if err != nil {
		return net.IPNet{}, fmt.Errorf("ipam %q add: %w", ipamType, err)
	}
	result, err := cnicurrent.NewResultFromResult(res)
	if err != nil {
		return net.IPNet{}, fmt.Errorf("failed to parse ipam %q result: %w", ipamType, err)
	}
	if len(result.IPs) == 0 {
		return net.IPNet{}, fmt.Errorf("ipam %q returned no IPs", ipamType)
	}
	return result.IPs[0].Address, nil
}

// ipamPluginConfig returns the CNI config bytes to hand to the IPAM plugin and
// the IPAM plugin type. For a single-plugin conf the config is returned
// unchanged (the IPAM plugin ignores parent fields like "type"/"master"). For a
// conflist the first plugin declaring an IPAM section is selected and the
// network-level "name"/"cniVersion" are injected into it, mirroring how libcni
// builds per-plugin stdin (so the dhcp clientID matches the original ADD).
func ipamPluginConfig(config []byte) ([]byte, string, error) {
	raw := map[string]json.RawMessage{}
	if err := json.Unmarshal(config, &raw); err != nil {
		return nil, "", fmt.Errorf("invalid CNI config JSON: %w", err)
	}

	ipamTypeOf := func(plugin map[string]json.RawMessage) string {
		ipamRaw, ok := plugin["ipam"]
		if !ok {
			return ""
		}
		var ipam struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(ipamRaw, &ipam); err != nil {
			return ""
		}
		return ipam.Type
	}

	pluginsRaw, isList := raw["plugins"]
	if !isList {
		return config, ipamTypeOf(raw), nil
	}

	var plugins []map[string]json.RawMessage
	if err := json.Unmarshal(pluginsRaw, &plugins); err != nil {
		return nil, "", fmt.Errorf("invalid CNI conflist plugins: %w", err)
	}
	for _, plugin := range plugins {
		ipamType := ipamTypeOf(plugin)
		if ipamType == "" {
			continue
		}
		if name, ok := raw["name"]; ok {
			plugin["name"] = name
		}
		if cniVersion, ok := raw["cniVersion"]; ok {
			plugin["cniVersion"] = cniVersion
		}
		netconf, err := json.Marshal(plugin)
		if err != nil {
			return nil, "", fmt.Errorf("failed to render ipam plugin config: %w", err)
		}
		return netconf, ipamType, nil
	}
	return nil, "", nil
}

func runtimeConf(p Params) *libcni.RuntimeConf {
	return &libcni.RuntimeConf{
		ContainerID:    p.ContainerID,
		NetNS:          p.NetNS,
		IfName:         p.IfName,
		CapabilityArgs: ipsCapability(p.Addresses),
	}
}

// ipsCapability renders the addresses as the CNI "ips" capability argument,
// which libcni injects into the matching plugin's runtimeConfig. The NAD must
// declare `"capabilities": {"ips": true}` and an IPAM that honours it (e.g.
// static). Returns nil when there are none.
func ipsCapability(addresses []string) map[string]interface{} {
	if len(addresses) == 0 {
		return nil
	}
	return map[string]interface{}{"ips": addresses}
}

// confList builds a libcni NetworkConfigList from a CNI config that is either a
// conflist (has "plugins") or a single-plugin conf.
func confList(config []byte) (*libcni.NetworkConfigList, error) {
	probe := map[string]any{}
	if err := json.Unmarshal(config, &probe); err != nil {
		return nil, fmt.Errorf("invalid CNI config JSON: %w", err)
	}
	if _, isList := probe["plugins"]; isList {
		return libcni.ConfListFromBytes(config)
	}
	conf, err := libcni.ConfFromBytes(config)
	if err != nil {
		return nil, fmt.Errorf("parse single CNI conf: %w", err)
	}
	return libcni.ConfListFromConf(conf)
}
