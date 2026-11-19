// SPDX-License-Identifier:Apache-2.0

//go:build runasroot

package routerconfiguration

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/openperouter/openperouter/internal/frr"
	"github.com/openperouter/openperouter/internal/netnamespace"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netlink/nl"
	"github.com/vishvananda/netns"
	"golang.org/x/sys/unix"
)

const (
	transitionLegacySource = "2001:db8:ffff::1"
	transitionNewSource    = "2001:db8:ffff::99"
)

func TestControllerFRRSourceTransitions(t *testing.T) {
	requireSourceTransitionFRR(t)
	resetFRRConfigCache(t)
	host, target, config := isolationTransitionFixture(t)
	updater := sourceTransitionFRR(t, target)
	apply := func() error {
		return netnamespace.In(host, func() error {
			return Reconcile(context.Background(), config.APIConfigData, 0, "warning", "", config.targetNamespace,
				updater, &KernelDatapathConfigurator{}, configureFRR)
		})
	}
	// The integrated config references the locator in bgpd before zebra's
	// backend has published it. A controller retry completes initial startup.
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		assert.NoError(collect, apply())
	}, 15*time.Second, 100*time.Millisecond)
	checkSource := func(source string, wantRules int) {
		t.Helper()
		require.NoError(t, netnamespace.In(target, func() error {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			output, err := exec.CommandContext(ctx, "ip", "sr", "tunsrc", "show").CombinedOutput()
			require.NoError(t, err, "%s", output)
			require.Contains(t, string(output), source)
			rules, err := netlink.RuleList(netlink.FAMILY_V6)
			require.NoError(t, err)
			count := 0
			for _, rule := range rules {
				if rule.Protocol == 242 {
					count++
				}
			}
			require.Equal(t, wantRules, count)
			return nil
		}))
	}
	checkSource(transitionLegacySource, 4)
	newSource := transitionNewSource
	config.Underlays[0].Spec.SRV6.EncapsulationSource = &newSource
	require.NoError(t, netnamespace.In(target, func() error {
		lo, err := netlink.LinkByName("lo")
		if err != nil {
			return err
		}
		return netlink.AddrAdd(lo, &netlink.Addr{IPNet: &net.IPNet{IP: net.ParseIP(newSource), Mask: net.CIDRMask(128, 128)}})
	}))
	config.Underlays[0].Spec.Interfaces[0].NetworkDevice.InterfaceName = "missing"
	require.ErrorContains(t, apply(), "failed to setup underlay")
	checkSource(newSource, 8)
	config.Underlays[0].Spec.Interfaces[0].NetworkDevice.InterfaceName = "uplink"
	require.NoError(t, apply())
	checkSource(newSource, 4)
	require.NoError(t, apply(), "unchanged source is idempotent")
	checkSource(newSource, 4)
	config.L3VPNs[0].Spec.RoutingIsolation.UnderlayTable = 701
	require.NoError(t, apply())
	checkSource(newSource, 4)
	require.NoError(t, netnamespace.In(target, func() error {
		rules, err := netlink.RuleList(netlink.FAMILY_V6)
		require.NoError(t, err)
		actions := isolationRuleActions(t)
		pairs := map[string][]netlink.Rule{}
		for _, rule := range rules {
			if rule.Protocol == 242 {
				pairs[rule.IifName] = append(pairs[rule.IifName], rule)
			}
		}
		require.Len(t, pairs, 2)
		for _, iface := range []string{"red", "lo"} {
			require.Len(t, pairs[iface], 2)
			var lookup, terminal *netlink.Rule
			for i := range pairs[iface] {
				rule := &pairs[iface][i]
				require.Equal(t, newSource, rule.Src.IP.String())
				require.Equal(t, "fc00:20::/48", rule.Dst.String())
				switch actions[rule.Priority] {
				case unix.FR_ACT_TO_TBL:
					lookup = rule
				case unix.FR_ACT_UNREACHABLE:
					terminal = rule
				default:
					t.Fatalf("unexpected rule action %v", rule)
				}
			}
			require.NotNil(t, lookup)
			require.NotNil(t, terminal)
			require.Equal(t, 701, lookup.Table)
			require.Zero(t, terminal.Table)
			require.Equal(t, lookup.Priority+1, terminal.Priority)
		}
		return nil
	}))
	config.L3VPNs[0].Spec.RoutingIsolation = nil
	config.Underlays[0].Spec.SRV6.EncapsulationSource = nil
	require.NoError(t, apply(), "the last isolated VPN transitions to legacy")
	checkSource(transitionLegacySource, 0)
	config.L3VPNs = nil
	require.NoError(t, apply(), "withdrawal retains the source still configured by FRR")
	checkSource(transitionLegacySource, 0)
}

func TestControllerFRRSupersededSourceReloads(t *testing.T) {
	requireSourceTransitionFRR(t)
	resetFRRConfigCache(t)
	host, target, config := isolationTransitionFixture(t)
	updater := sourceTransitionFRR(t, target)
	apply := func(configure frrConfiguratorType) error {
		return netnamespace.In(host, func() error {
			return Reconcile(context.Background(), config.APIConfigData, 0, "warning", "", config.targetNamespace,
				updater, &KernelDatapathConfigurator{}, configure)
		})
	}
	require.EventuallyWithT(t, func(collect *assert.CollectT) { assert.NoError(collect, apply(configureFRR)) },
		15*time.Second, 100*time.Millisecond)
	active := transitionLegacySource
	failure := errors.New("reload interrupted")
	for _, step := range []struct {
		source string
		apply  bool
		fail   bool
	}{
		{source: transitionNewSource, fail: true},
		{source: "2001:db8:ffff::100", fail: true},
		{source: "2001:db8:ffff::101", apply: true, fail: true},
		{source: transitionNewSource, apply: true},
		{source: transitionLegacySource, apply: true},
	} {
		t.Run(step.source+fmt.Sprint(step.fail), func(t *testing.T) {
			config.Underlays[0].Spec.SRV6.EncapsulationSource = &step.source
			require.NoError(t, netnamespace.In(target, func() error {
				lo, err := netlink.LinkByName("lo")
				if err != nil {
					return err
				}
				return netlink.AddrReplace(lo, &netlink.Addr{IPNet: &net.IPNet{
					IP: net.ParseIP(step.source), Mask: net.CIDRMask(128, 128),
				}})
			}))
			calls := 0
			err := apply(func(ctx context.Context, data frrConfigData) error {
				calls++
				require.NoError(t, netnamespace.In(target, func() error {
					require.True(t, configuratorTunnelSource(t).Equal(net.ParseIP(active)))
					rules, err := netlink.RuleList(netlink.FAMILY_V6)
					require.NoError(t, err)
					requireOwnedSourcePairs(t, rules, isolationRuleActions(t), []string{active, step.source})
					requireLegacyGuards(t, []string{active})
					return nil
				}))
				if step.apply {
					require.NoError(t, configureFRR(ctx, data))
					active = step.source
				}
				if step.fail {
					return failure
				}
				return nil
			})
			require.Equal(t, 1, calls)
			if step.fail {
				require.ErrorIs(t, err, failure)
			} else {
				require.NoError(t, err)
			}
			require.NoError(t, netnamespace.In(target, func() error {
				require.True(t, configuratorTunnelSource(t).Equal(net.ParseIP(active)))
				if !step.fail {
					rules, err := netlink.RuleList(netlink.FAMILY_V6)
					require.NoError(t, err)
					requireOwnedSourcePairs(t, rules, isolationRuleActions(t), []string{active})
				}
				return nil
			}))
		})
	}
}

func requireSourceTransitionFRR(t *testing.T) {
	t.Helper()
	for _, binary := range []string{
		"/usr/lib/frr/mgmtd", "/usr/lib/frr/zebra", "/usr/lib/frr/bgpd", "/usr/lib/frr/isisd", "/usr/bin/vtysh",
	} {
		if _, err := os.Stat(binary); errors.Is(err, os.ErrNotExist) {
			t.Skip("real FRR is required: " + binary)
		}
	}
}

func TestControllerFRRLastIsolatedToLegacyAfterHostFailure(t *testing.T) {
	requireSourceTransitionFRR(t)
	resetFRRConfigCache(t)
	host, target, config := isolationTransitionFixture(t)
	oldSource := transitionNewSource
	legacySource := transitionLegacySource
	config.Underlays[0].Spec.SRV6.EncapsulationSource = &oldSource
	require.NoError(t, netnamespace.In(target, func() error {
		lo, err := netlink.LinkByName("lo")
		if err != nil {
			return err
		}
		return netlink.AddrAdd(lo, &netlink.Addr{IPNet: &net.IPNet{
			IP: net.ParseIP(oldSource), Mask: net.CIDRMask(128, 128),
		}})
	}))
	updateFRR := sourceTransitionFRR(t, target)
	updater := updateFRR
	apply := func() error {
		return netnamespace.In(host, func() error {
			return Reconcile(context.Background(), config.APIConfigData, 0, "warning", "", config.targetNamespace,
				updater, &KernelDatapathConfigurator{}, configureFRR)
		})
	}
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		assert.NoError(collect, apply())
	}, 15*time.Second, 100*time.Millisecond)
	config.L3VPNs[0].Spec.RoutingIsolation = nil
	config.Underlays[0].Spec.SRV6.EncapsulationSource = nil

	// A reload may change the kernel before reporting an error. Both source
	// versions survive that attempt; retry can reclaim the now inactive one.
	reloadFailure := errors.New("injected failure after FRR applied the source")
	updater = func(ctx context.Context, configuration string) error {
		if err := updateFRR(ctx, configuration); err != nil {
			return err
		}
		return reloadFailure
	}
	require.ErrorIs(t, apply(), reloadFailure)
	requireLegacySourcePairs(t, target, []string{oldSource, legacySource})

	updater = updateFRR
	config.Underlays[0].Spec.Interfaces[0].NetworkDevice.InterfaceName = "missing"
	require.ErrorContains(t, apply(), "failed to setup underlay")
	requireLegacySourcePairs(t, target, []string{legacySource})
	config.Underlays[0].Spec.Interfaces[0].NetworkDevice.InterfaceName = "uplink"
	require.NoError(t, apply())
	requireLegacySourcePairs(t, target, nil)
	require.NoError(t, apply(), "retry after completion is idempotent")
	requireLegacySourcePairs(t, target, nil)
}

func requireLegacySourcePairs(t *testing.T, target netns.NsHandle, protected []string) {
	t.Helper()
	require.NoError(t, netnamespace.In(target, func() error {
		require.True(t, configuratorTunnelSource(t).Equal(net.ParseIP(transitionLegacySource)))
		rules, err := netlink.RuleList(netlink.FAMILY_V6)
		require.NoError(t, err)
		requireOwnedSourcePairs(t, rules, isolationRuleActions(t), protected)
		requireLegacyGuards(t, protected)
		return nil
	}))
}

func requireOwnedSourcePairs(t *testing.T, rules []netlink.Rule, actions map[int]uint8, protected []string) {
	t.Helper()
	owned := map[string][]netlink.Rule{}
	for _, rule := range rules {
		if rule.Protocol == 242 {
			require.NotNil(t, rule.Src)
			owned[rule.Src.IP.String()] = append(owned[rule.Src.IP.String()], rule)
		}
	}
	require.Len(t, owned, len(protected))
	for _, address := range protected {
		pairs := owned[net.ParseIP(address).String()]
		require.Len(t, pairs, 4, "source %s", address)
		for _, iface := range []string{"red", "lo"} {
			var lookup, terminal *netlink.Rule
			for i := range pairs {
				rule := &pairs[i]
				if rule.IifName != iface {
					continue
				}
				require.Equal(t, "fc00:20::/48", rule.Dst.String())
				switch rule.Table {
				case 254:
					lookup = rule
				case 0:
					terminal = rule
				}
			}
			require.NotNil(t, lookup)
			require.NotNil(t, terminal)
			require.Equal(t, uint8(unix.FR_ACT_TO_TBL), actions[lookup.Priority])
			require.Equal(t, uint8(unix.FR_ACT_UNREACHABLE), actions[terminal.Priority])
			require.Equal(t, uint8(242), lookup.Protocol)
			require.Equal(t, uint8(242), terminal.Protocol)
			require.Equal(t, iface, lookup.IifName)
			require.Equal(t, iface, terminal.IifName)
			require.Equal(t, lookup.Priority+1, terminal.Priority)
		}
	}
}

func isolationRuleActions(t *testing.T) map[int]uint8 {
	t.Helper()
	request := nl.NewNetlinkRequest(unix.RTM_GETRULE, unix.NLM_F_DUMP|unix.NLM_F_REQUEST)
	request.AddData(nl.NewIfInfomsg(netlink.FAMILY_V6))
	messages, err := request.Execute(unix.NETLINK_ROUTE, unix.RTM_NEWRULE)
	require.NoError(t, err)
	actions := make(map[int]uint8, len(messages))
	for _, data := range messages {
		header := nl.DeserializeRtMsg(data)
		attrs, err := nl.ParseRouteAttr(data[header.Len():])
		require.NoError(t, err)
		for _, attr := range attrs {
			if attr.Attr.Type == nl.FRA_PRIORITY {
				actions[int(binary.NativeEndian.Uint32(attr.Value))] = header.Type
			}
		}
	}
	return actions
}

func requireLegacyGuards(t *testing.T, protected []string) {
	t.Helper()
	vrf, err := netlink.LinkByName("red")
	require.NoError(t, err)
	for _, family := range []int{netlink.FAMILY_V4, netlink.FAMILY_V6} {
		guards, err := netlink.RouteListFiltered(family,
			&netlink.Route{Table: int(vrf.(*netlink.Vrf).Table), Protocol: 242},
			netlink.RT_FILTER_TABLE|netlink.RT_FILTER_PROTOCOL)
		require.NoError(t, err)
		if len(protected) == 0 {
			require.Empty(t, guards)
		} else {
			require.Len(t, guards, 1)
			requireConfiguratorDefaultGuard(t, guards[0])
		}
	}
}

// Only this source-transition test needs FRR; its daemons and files live in the
// isolated fixture namespace/container and are reaped before removing their paths.
func sourceTransitionFRR(t *testing.T, ns netns.NsHandle) frr.ConfigUpdater {
	t.Helper()
	frrUser, err := user.Lookup("frr")
	require.NoError(t, err)
	uid, err := strconv.Atoi(frrUser.Uid)
	require.NoError(t, err)
	gid, err := strconv.Atoi(frrUser.Gid)
	require.NoError(t, err)
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll("/var/run/frr", 0755))
	runPath, err := os.MkdirTemp("/var/run/frr", "source-transition-")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, os.RemoveAll(runPath)) })
	name := filepath.Base(runPath)
	for _, root := range []string{"/var/run/frr", "/var/lib/frr", "/etc/frr"} {
		require.NoError(t, os.MkdirAll(root, 0755))
		path := filepath.Join(root, name)
		if path != runPath {
			require.NoError(t, os.Mkdir(path, 0755))
			t.Cleanup(func() { require.NoError(t, os.RemoveAll(path)) })
		}
		require.NoError(t, os.Chmod(path, 0755))
		require.NoError(t, os.Chown(path, uid, gid))
	}
	require.NoError(t, os.WriteFile(filepath.Join("/etc/frr", name, "frr.conf"), nil, 0644))
	require.NoError(t, os.WriteFile(filepath.Join("/etc/frr", name, "vtysh.conf"), nil, 0644))
	for _, daemon := range []string{"mgmtd", "zebra", "bgpd", "isisd"} {
		log, err := os.Create(filepath.Join(dir, daemon+".log"))
		require.NoError(t, err)
		cmd := exec.Command("/usr/lib/frr/"+daemon, "-N", name, "--log", "stdout")
		cmd.Stdout, cmd.Stderr = log, log
		err = netnamespace.In(ns, cmd.Start)
		closeErr := log.Close()
		if err == nil {
			t.Cleanup(func() { stopSourceTransitionFRR(t, cmd) })
		}
		require.NoError(t, err)
		require.NoError(t, closeErr)
	}
	require.Eventually(t, func() bool {
		for _, daemon := range []string{"mgmtd", "zebra", "bgpd", "isisd"} {
			if _, err := os.Stat(filepath.Join("/var/run/frr", name, daemon+".vty")); err != nil {
				return false
			}
		}
		return true
	}, 15*time.Second, 100*time.Millisecond)
	return func(ctx context.Context, config string) error {
		path := filepath.Join(dir, "frr.conf")
		if err := os.WriteFile(path, []byte(config), 0644); err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		output, err := exec.CommandContext(ctx, "/usr/bin/vtysh", "-N", name, "-f", path).CombinedOutput()
		if err != nil {
			return fmt.Errorf("apply FRR source transition: %w: %s", errors.Join(err, ctx.Err()), output)
		}
		if strings.Contains(string(output), "% Unknown command") {
			return fmt.Errorf("FRR rejected source transition configuration: %s", output)
		}
		return nil
	}
}

func stopSourceTransitionFRR(t *testing.T, cmd *exec.Cmd) {
	t.Helper()
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
		t.Errorf("stop FRR: %v", err)
	}
	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()
	select {
	case err := <-waited:
		if err == nil {
			return
		}
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.Sys().(syscall.WaitStatus).Signal() != syscall.SIGTERM {
			t.Errorf("wait FRR: %v", err)
		}
	case <-time.After(3 * time.Second):
		require.NoError(t, cmd.Process.Kill())
		select {
		case err := <-waited:
			var exit *exec.ExitError
			require.ErrorAs(t, err, &exit)
			require.Equal(t, syscall.SIGKILL, exit.Sys().(syscall.WaitStatus).Signal())
		case <-time.After(3 * time.Second):
			t.Error("FRR did not exit after kill")
		}
	}
}
