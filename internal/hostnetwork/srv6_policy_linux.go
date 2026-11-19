// SPDX-License-Identifier:Apache-2.0

package hostnetwork

import (
	"encoding/binary"
	"fmt"
	"net"
	"slices"
	"strings"

	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netlink/nl"
	"golang.org/x/sys/unix"
)

type isolationPolicyRule struct {
	rule    netlink.Rule
	l3mdev  bool
	unknown bool
}

// netlink v1.3.1 RuleList omits FRA_L3MDEV and the rule action. Both matter
// for ownership and ordering, so read the complete dump through rtnetlink.
func listIsolationPolicyRules() ([]isolationPolicyRule, error) {
	req := nl.NewNetlinkRequest(unix.RTM_GETRULE, unix.NLM_F_DUMP|unix.NLM_F_REQUEST)
	req.AddData(nl.NewIfInfomsg(netlink.FAMILY_V6))
	messages, err := req.Execute(unix.NETLINK_ROUTE, unix.RTM_NEWRULE)
	if err != nil {
		return nil, err
	}
	var rules []isolationPolicyRule
	for _, data := range messages {
		header := nl.DeserializeRtMsg(data)
		attrs, err := nl.ParseRouteAttr(data[header.Len():])
		if err != nil {
			return nil, err
		}
		rule := netlink.NewRule()
		rule.Priority, rule.Table, rule.Family, rule.Type = 0, int(header.Table), int(header.Family), header.Type
		entry := isolationPolicyRule{rule: *rule, unknown: header.Flags&^uint32(unix.FIB_RULE_IIF_DETACHED) != 0 || header.Tos != 0}
		for _, attr := range attrs {
			value := attr.Value
			switch attr.Attr.Type {
			case nl.FRA_SRC:
				entry.rule.Src = &net.IPNet{IP: value, Mask: net.CIDRMask(int(header.Src_len), len(value)*8)}
			case nl.FRA_DST:
				entry.rule.Dst = &net.IPNet{IP: value, Mask: net.CIDRMask(int(header.Dst_len), len(value)*8)}
			case nl.FRA_IIFNAME:
				entry.rule.IifName = strings.TrimSuffix(string(value), "\x00")
			case nl.FRA_PRIORITY:
				entry.rule.Priority = int(binary.NativeEndian.Uint32(value))
			case nl.FRA_PROTOCOL:
				entry.rule.Protocol = value[0]
			case nl.FRA_TABLE:
				entry.rule.Table = int(binary.NativeEndian.Uint32(value))
			case nl.FRA_L3MDEV:
				entry.l3mdev = value[0] == 1
			case nl.FRA_SUPPRESS_PREFIXLEN:
				entry.unknown = entry.unknown || binary.NativeEndian.Uint32(value) != ^uint32(0)
			default:
				entry.unknown = true
			}
		}
		rules = append(rules, entry)
	}
	return rules, nil
}

func ownedIsolationPolicyRule(entry isolationPolicyRule) bool {
	r := entry.rule
	if entry.unknown || entry.l3mdev || r.Protocol != uint8(vrfGuardProtocol) || r.Src == nil || r.IifName == "" {
		return false
	}
	ones, bits := r.Src.Mask.Size()
	if ones != 128 || bits != 128 || r.Src.IP.To4() != nil || !r.Src.IP.IsGlobalUnicast() {
		return false
	}
	if r.Dst == nil {
		return false
	}
	destinationBits, destinationSize := r.Dst.Mask.Size()
	if destinationBits == 0 || destinationSize != 128 || !r.Dst.IP.IsGlobalUnicast() {
		return false
	}
	if r.Type == unix.FR_ACT_UNREACHABLE {
		return r.Table == 0
	}
	return r.Type == unix.FR_ACT_TO_TBL && r.Table != 0 && r.Table != 253 && r.Table != 255
}

func isolationRuleKey(r netlink.Rule) string {
	return fmt.Sprintf("%s/%s/%s/%d/%d/%d", r.Src, r.Dst, r.IifName, r.Table, r.Type, r.Protocol)
}

func desiredIsolationRules(domains map[string]SRv6Isolation) []netlink.Rule {
	rules := map[string]netlink.Rule{}
	for vrf, config := range domains {
		for _, prefix := range config.RemoteLocatorPrefixes {
			_, destination, _ := net.ParseCIDR(prefix)
			for _, iface := range []string{vrf, "lo"} {
				rule := netlink.NewRule()
				rule.Family, rule.Protocol = netlink.FAMILY_V6, uint8(vrfGuardProtocol)
				rule.Src = &net.IPNet{IP: net.ParseIP(config.Source).To16(), Mask: net.CIDRMask(128, 128)}
				rule.Dst, rule.IifName = destination, iface
				rule.Table, rule.Type = int(config.UnderlayTable), unix.FR_ACT_TO_TBL
				rules[isolationRuleKey(*rule)] = *rule
				rule.Table, rule.Type = 0, unix.FR_ACT_UNREACHABLE
				rules[isolationRuleKey(*rule)] = *rule
			}
		}
	}
	keys := make([]string, 0, len(rules))
	for key := range rules {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	var result []netlink.Rule
	// Each lookup precedes its terminal failure action. No later tenant lookup
	// may handle an outer packet when the selected underlay table has no route.
	for _, key := range keys {
		r := rules[key]
		if r.Type != unix.FR_ACT_TO_TBL {
			continue
		}
		result = append(result, r)
		r.Table, r.Type = 0, unix.FR_ACT_UNREACHABLE
		result = append(result, r)
	}

	return result
}

// Use deterministic adjacent priority pairs, retaining matching pairs (including
// shared lo rules). Foreign occupants are conflicts, never moved or overwritten.
func planIsolationPriorities(desired []netlink.Rule, current []isolationPolicyRule) ([]netlink.Rule, error) {
	if len(desired) == 0 {
		return nil, nil
	}
	before := isolationL3MDevPriority(current)
	if before == 0 {
		return nil, fmt.Errorf("IPv6 l3mdev policy rule is missing")
	}
	result := slices.Clone(desired)
	used := map[int]struct{}{}
	reserved := map[int]struct{}{}
	for _, entry := range current {
		if ownedIsolationPolicyRule(entry) {
			reserved[entry.rule.Priority] = struct{}{}
		}
	}
	for i := 0; i < len(result); i += 2 {
		priority := stagedIsolationPair(desired[i], desired[i+1], current)
		if priority == 0 {
			continue
		}
		_, firstUsed := used[priority]
		_, secondUsed := used[priority+1]
		if priority < 1 || priority+1 >= before || firstUsed || secondUsed {
			return nil, fmt.Errorf("conflicting owned SRv6 priority pair %d", priority)
		}
		if conflictsWithOwnedPriorityPair(current, priority, desired[i], desired[i+1]) {
			return nil, fmt.Errorf("conflicting owned SRv6 priority pair %d", priority)
		}
		result[i].Priority, result[i+1].Priority = priority, priority+1
		used[priority], used[priority+1] = struct{}{}, struct{}{}
	}
	for i := 0; i < len(result); i += 2 {
		if result[i].Priority > 0 {
			continue
		}
		priority := freeIsolationPriority(before, used, reserved)
		if priority+1 >= before {
			return nil, fmt.Errorf("not enough priorities before IPv6 l3mdev rule %d", before)
		}
		result[i].Priority, result[i+1].Priority = priority, priority+1
		used[priority], used[priority+1] = struct{}{}, struct{}{}
	}
	for _, entry := range current {
		_, occupied := used[entry.rule.Priority]
		if !ownedIsolationPolicyRule(entry) && occupied {
			return nil, fmt.Errorf("foreign rule conflicts with isolation priority %d", entry.rule.Priority)
		}
	}
	return result, nil
}

func conflictsWithOwnedPriorityPair(current []isolationPolicyRule, priority int, lookup, terminal netlink.Rule) bool {
	for _, entry := range current {
		if !ownedIsolationPolicyRule(entry) {
			continue
		}
		if entry.rule.Priority == priority && isolationRuleKey(entry.rule) != isolationRuleKey(lookup) {
			return true
		}
		if entry.rule.Priority == priority+1 && isolationRuleKey(entry.rule) != isolationRuleKey(terminal) {
			return true
		}
	}
	return false
}

func freeIsolationPriority(before int, used, reserved map[int]struct{}) int {
	for priority := 1; priority+1 < before; priority += 2 {
		_, firstUsed := used[priority]
		_, secondUsed := used[priority+1]
		_, firstReserved := reserved[priority]
		_, secondReserved := reserved[priority+1]
		if !firstUsed && !secondUsed && !firstReserved && !secondReserved {
			return priority
		}
	}
	return before
}

func existingIsolationPair(lookup, terminal netlink.Rule, current []isolationPolicyRule) int {
	priority := stagedIsolationPair(lookup, terminal, current)
	if priority == 0 {
		return 0
	}
	for _, entry := range current {
		if ownedIsolationPolicyRule(entry) && entry.rule.Priority == priority &&
			isolationRuleKey(entry.rule) == isolationRuleKey(lookup) {
			for _, other := range current {
				if ownedIsolationPolicyRule(other) && other.rule.Priority == priority+1 &&
					isolationRuleKey(other.rule) == isolationRuleKey(terminal) {
					return priority
				}
			}
		}
	}
	return 0
}

func stagedIsolationPair(lookup, terminal netlink.Rule, current []isolationPolicyRule) int {
	var lookups, terminals []netlink.Rule
	for _, entry := range current {
		if !ownedIsolationPolicyRule(entry) {
			continue
		}
		if isolationRuleKey(entry.rule) == isolationRuleKey(lookup) {
			lookups = append(lookups, entry.rule)
		}
		if isolationRuleKey(entry.rule) == isolationRuleKey(terminal) {
			terminals = append(terminals, entry.rule)
		}
	}
	if len(lookups) > 1 {
		return 0
	}
	if len(lookups) == 1 {
		for _, candidate := range terminals {
			if candidate.Priority == lookups[0].Priority+1 {
				return lookups[0].Priority
			}
		}
		return lookups[0].Priority
	}
	for _, candidate := range terminals {
		occupied := false
		for _, entry := range current {
			if ownedIsolationPolicyRule(entry) && entry.rule.Priority == candidate.Priority-1 {
				occupied = true
				break
			}
		}
		if !occupied {
			return candidate.Priority - 1
		}
	}
	return 0
}

func isolationL3MDevPriority(current []isolationPolicyRule) int {
	before := 0
	for _, entry := range current {
		if entry.l3mdev && (before == 0 || entry.rule.Priority < before) {
			before = entry.rule.Priority
		}
	}
	return before
}

func isolationPoliciesEqual(desired []netlink.Rule, current []isolationPolicyRule) bool {
	return len(obsoleteIsolationRules(current, desired)) == 0 && len(missingIsolationRules(current, desired)) == 0
}

func containsIsolationRule(rules []netlink.Rule, want netlink.Rule) bool {
	for _, r := range rules {
		if r.Priority == want.Priority && isolationRuleKey(r) == isolationRuleKey(want) {
			return true
		}
	}
	return false
}

func obsoleteIsolationRules(current []isolationPolicyRule, desired []netlink.Rule) []netlink.Rule {
	kept := map[string]struct{}{}
	var remove []netlink.Rule
	for _, entry := range current {
		if !ownedIsolationPolicyRule(entry) {
			continue
		}
		r := entry.rule
		key := fmt.Sprintf("%d/%s", r.Priority, isolationRuleKey(r))
		_, exists := kept[key]
		if containsIsolationRule(desired, r) && !exists {
			kept[key] = struct{}{}
			continue
		}
		remove = append(remove, r)
	}
	return remove
}

func missingIsolationRules(current []isolationPolicyRule, desired []netlink.Rule) []netlink.Rule {
	var missing []netlink.Rule
	for _, want := range desired {
		found := false
		for _, entry := range current {
			if ownedIsolationPolicyRule(entry) && containsIsolationRule([]netlink.Rule{want}, entry.rule) {
				found = true
				break
			}
		}
		if !found {
			missing = append(missing, want)
		}
	}
	return missing
}
