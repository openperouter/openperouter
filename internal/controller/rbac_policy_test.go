// SPDX-License-Identifier:Apache-2.0

package controller_test

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/util/yaml"
)

type expectedRule struct {
	APIGroups     []string
	Resources     []string
	ResourceNames []string
	Verbs         []string
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot determine repo root")
	}
	return filepath.Join(filepath.Dir(thisFile), "..", "..")
}

func loadClusterRole(t *testing.T, filename string) rbacv1.ClusterRole {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "config", "rbac", filename))
	if err != nil {
		t.Fatalf("read %s: %v", filename, err)
	}
	var cr rbacv1.ClusterRole
	if err := yaml.UnmarshalStrict(data, &cr); err != nil {
		t.Fatalf("parse %s: %v", filename, err)
	}
	return cr
}

func loadRole(t *testing.T, filename string) rbacv1.Role {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "config", "rbac", filename))
	if err != nil {
		t.Fatalf("read %s: %v", filename, err)
	}
	var r rbacv1.Role
	if err := yaml.UnmarshalStrict(data, &r); err != nil {
		t.Fatalf("parse %s: %v", filename, err)
	}
	return r
}

func sortedCopy(s []string) []string {
	c := slices.Clone(s)
	slices.Sort(c)
	return c
}

func ruleKey(r expectedRule) string {
	return fmt.Sprintf("apiGroups=%v resources=%v resourceNames=%v",
		sortedCopy(r.APIGroups), sortedCopy(r.Resources), sortedCopy(r.ResourceNames))
}

func policyRuleToExpected(r rbacv1.PolicyRule) expectedRule {
	return expectedRule{
		APIGroups:     r.APIGroups,
		Resources:     r.Resources,
		ResourceNames: r.ResourceNames,
		Verbs:         r.Verbs,
	}
}

func assertRules(t *testing.T, roleName string, actual []rbacv1.PolicyRule, expected []expectedRule) {
	t.Helper()

	if len(actual) != len(expected) {
		t.Errorf("%s: expected %d rules, got %d", roleName, len(expected), len(actual))
		for i, r := range actual {
			t.Logf("  actual[%d]: %s verbs=%v", i, ruleKey(policyRuleToExpected(r)), sortedCopy(r.Verbs))
		}
		return
	}

	matched := make([]bool, len(expected))
	for _, got := range actual {
		gotKey := ruleKey(policyRuleToExpected(got))
		found := false
		for i, want := range expected {
			if matched[i] {
				continue
			}
			if ruleKey(want) == gotKey {
				gotVerbs := strings.Join(sortedCopy(got.Verbs), ",")
				wantVerbs := strings.Join(sortedCopy(want.Verbs), ",")
				if gotVerbs != wantVerbs {
					t.Errorf("%s: rule %s has verbs [%s], expected [%s]", roleName, gotKey, gotVerbs, wantVerbs)
				}
				matched[i] = true
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%s: unexpected rule: %s verbs=%v", roleName, gotKey, sortedCopy(got.Verbs))
		}
	}
	for i, m := range matched {
		if !m {
			t.Errorf("%s: missing expected rule: %s verbs=%v", roleName, ruleKey(expected[i]), sortedCopy(expected[i].Verbs))
		}
	}
}

var crdGroup = []string{"network.openperouter.io"}

func TestControllerClusterRole(t *testing.T) {
	cr := loadClusterRole(t, "role.yaml")
	assertRules(t, cr.Name, cr.Rules, []expectedRule{
		{APIGroups: []string{""}, Resources: []string{"nodes"}, Verbs: []string{"get", "list", "patch", "watch"}},
		{APIGroups: crdGroup, Resources: []string{"l2vnis", "l3passthroughs", "l3vnis", "l3vpns", "rawfrrconfigs", "routernodeconfigurationstatuses", "underlays"}, Verbs: []string{"create", "delete", "get", "list", "patch", "update", "watch"}},
		{APIGroups: crdGroup, Resources: []string{"l2vnis/finalizers", "l3passthroughs/finalizers", "l3vnis/finalizers", "l3vpns/finalizers", "rawfrrconfigs/finalizers", "underlays/finalizers"}, Verbs: []string{"update"}},
		{APIGroups: crdGroup, Resources: []string{"l2vnis/status", "l3passthroughs/status", "l3vnis/status", "l3vpns/status", "rawfrrconfigs/status", "routernodeconfigurationstatuses/status", "underlays/status"}, Verbs: []string{"get", "patch", "update"}},
	})
}

func TestNodemarkerClusterRole(t *testing.T) {
	cr := loadClusterRole(t, "nodemarker_cluster_role.yaml")
	assertRules(t, cr.Name, cr.Rules, []expectedRule{
		{APIGroups: []string{""}, Resources: []string{"nodes"}, Verbs: []string{"get", "list", "patch", "update", "watch"}},
		{APIGroups: []string{"admissionregistration.k8s.io"}, Resources: []string{"validatingwebhookconfigurations"}, Verbs: []string{"get", "list", "watch"}},
		{APIGroups: []string{"admissionregistration.k8s.io"}, Resources: []string{"validatingwebhookconfigurations"}, ResourceNames: []string{"openpe-validating-webhook-configuration"}, Verbs: []string{"update"}},
		{APIGroups: crdGroup, Resources: []string{"l2vnis", "l3passthroughs", "l3vnis", "l3vpns", "rawfrrconfigs", "underlays"}, Verbs: []string{"get", "list", "watch"}},
	})
}

func TestControllerNamespacedRole(t *testing.T) {
	role := loadRole(t, "secrets_role.yaml")
	assertRules(t, role.Name, role.Rules, []expectedRule{
		{APIGroups: []string{""}, Resources: []string{"secrets"}, Verbs: []string{"get", "list", "watch"}},
		{APIGroups: []string{""}, Resources: []string{"pods"}, Verbs: []string{"delete", "get", "list", "watch"}},
	})
}

func TestNodemarkerNamespacedRole(t *testing.T) {
	role := loadRole(t, "nodemarker_role.yaml")
	assertRules(t, role.Name, role.Rules, []expectedRule{
		{APIGroups: []string{""}, Resources: []string{"secrets"}, Verbs: []string{"get", "list", "watch"}},
		{APIGroups: []string{""}, Resources: []string{"secrets"}, ResourceNames: []string{"openpe-webhook-server-cert"}, Verbs: []string{"update"}},
	})
}
