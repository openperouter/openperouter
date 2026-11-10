// SPDX-License-Identifier:Apache-2.0

package crdschema

import (
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/util/validation/field"
	celconfig "k8s.io/apiserver/pkg/apis/cel"
)

func TestL2TrunkSchema(t *testing.T) {
	tests := []struct {
		name    string
		kind    string
		spec    map[string]any
		wantErr string
	}{
		{
			name: "host type is not in the enum", kind: "L2Trunk",
			spec:    map[string]any{"type": "Host"},
			wantErr: `Unsupported value: "Host"`,
		},
		{
			name: "missing type", kind: "L2Trunk", spec: map[string]any{},
			wantErr: "type: Required value",
		},
		{
			name: "unknown type", kind: "L2Trunk", spec: map[string]any{"type": "Other"},
			wantErr: `Unsupported value: "Other"`,
		},
		{
			name: "SR-IOV VF", kind: "L2Trunk",
			spec: map[string]any{"type": "SRIOV", "sriov": trunkSRIOV("enp1s0", 1)},
		},
		{
			name: "SR-IOV VF with accelerated config", kind: "L2Trunk",
			spec: map[string]any{"type": "SRIOV", "sriov": map[string]any{
				"pfName": "enp1s0", "vfIndex": int64(0),
				"acceleratedConfig": map[string]any{"rxQueues": int64(4), "qSize": int64(2048)},
			}},
		},
		{
			name: "SR-IOV without sriov", kind: "L2Trunk", spec: map[string]any{"type": "SRIOV"},
			wantErr: "type/config mismatch: sriov must be set if and only if type is 'SRIOV'",
		},
		{
			name: "SR-IOV missing pfName", kind: "L2Trunk",
			spec:    map[string]any{"type": "SRIOV", "sriov": map[string]any{"vfIndex": int64(1)}},
			wantErr: "pfName: Required value",
		},
		{
			name: "SR-IOV missing vfIndex", kind: "L2Trunk",
			spec:    map[string]any{"type": "SRIOV", "sriov": map[string]any{"pfName": "enp1s0"}},
			wantErr: "vfIndex: Required value",
		},
		{
			name: "SR-IOV negative vfIndex", kind: "L2Trunk",
			spec:    map[string]any{"type": "SRIOV", "sriov": trunkSRIOV("enp1s0", -1)},
			wantErr: "should be greater than or equal to 0",
		},
		{
			name: "SR-IOV invalid pfName", kind: "L2Trunk",
			spec:    map[string]any{"type": "SRIOV", "sriov": trunkSRIOV("1enp", 1)},
			wantErr: "should match",
		},
		{
			name: "SR-IOV pfName too long", kind: "L2Trunk",
			spec:    map[string]any{"type": "SRIOV", "sriov": trunkSRIOV("enp1s0f0abcdefgh", 1)},
			wantErr: "may not be more than 15 bytes",
		},
		{
			name: "standalone L2VNI", kind: "L2VNI", spec: map[string]any{"vni": int64(100)},
		},
		{
			name: "standalone managed bridge", kind: "L2VNI",
			spec: map[string]any{"vni": int64(100), "hostMaster": trunkHostMaster("LinuxBridge", "Managed")},
		},
		{
			name: "trunk with hostMaster", kind: "L2VNI",
			spec: map[string]any{
				"vni": int64(100), "trunk": trunkReference("workloads", 100),
				"hostMaster": trunkHostMaster("LinuxBridge", "External"),
			},
			wantErr: "hostMaster and trunk are mutually exclusive",
		},
		{
			name: "missing trunk name", kind: "L2VNI",
			spec:    map[string]any{"vni": int64(100), "trunk": map[string]any{"vlan": map[string]any{"tag": int64(100)}}},
			wantErr: "name: Required value",
		},
		{
			name: "empty trunk name", kind: "L2VNI",
			spec:    map[string]any{"vni": int64(100), "trunk": trunkReference("", 100)},
			wantErr: "should be at least 1 chars long",
		},
		{
			name: "missing VLAN", kind: "L2VNI",
			spec:    map[string]any{"vni": int64(100), "trunk": map[string]any{"name": "workloads"}},
			wantErr: "vlan: Required value",
		},
		{
			name: "missing VLAN tag", kind: "L2VNI",
			spec: map[string]any{
				"vni": int64(100), "trunk": map[string]any{"name": "workloads", "vlan": map[string]any{}},
			},
			wantErr: "tag: Required value",
		},
		{
			name: "VLAN zero", kind: "L2VNI",
			spec:    map[string]any{"vni": int64(100), "trunk": trunkReference("workloads", 0)},
			wantErr: "should be greater than or equal to 1",
		},
		{
			name: "minimum VLAN", kind: "L2VNI",
			spec: map[string]any{"vni": int64(100), "trunk": trunkReference("workloads", 1)},
		},
		{
			name: "maximum VLAN", kind: "L2VNI",
			spec: map[string]any{"vni": int64(100), "trunk": trunkReference("workloads", 4094)},
		},
		{
			name: "reserved VLAN", kind: "L2VNI",
			spec:    map[string]any{"vni": int64(100), "trunk": trunkReference("workloads", 4095)},
			wantErr: "should be less than or equal to 4094",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			obj := newUnstructured(tc.kind, tc.spec)
			gvk := parityGVK(tc.kind)
			err := Validate(t.Context(), obj, gvk).ToAggregate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected validation error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestTrunkImmutability(t *testing.T) {
	tests := []struct {
		name    string
		kind    string
		oldSpec map[string]any
		newSpec map[string]any
		wantErr string
	}{
		{
			name: "unchanged reference", kind: "L2VNI",
			oldSpec: map[string]any{"vni": int64(100), "trunk": trunkReference("workloads", 100)},
			newSpec: map[string]any{"vni": int64(100), "trunk": trunkReference("workloads", 100)},
		},
		{
			name: "change reference", kind: "L2VNI",
			oldSpec: map[string]any{"vni": int64(100), "trunk": trunkReference("workloads", 100)},
			newSpec: map[string]any{"vni": int64(100), "trunk": trunkReference("other", 100)},
			wantErr: "trunk is immutable",
		},
		{
			name: "change tag", kind: "L2VNI",
			oldSpec: map[string]any{"vni": int64(100), "trunk": trunkReference("workloads", 100)},
			newSpec: map[string]any{"vni": int64(100), "trunk": trunkReference("workloads", 200)},
			wantErr: "trunk is immutable",
		},
		{
			name: "add reference", kind: "L2VNI",
			oldSpec: map[string]any{"vni": int64(100)},
			newSpec: map[string]any{"vni": int64(100), "trunk": trunkReference("workloads", 100)},
			wantErr: "trunk is immutable",
		},
		{
			name: "remove reference", kind: "L2VNI",
			oldSpec: map[string]any{"vni": int64(100), "trunk": trunkReference("workloads", 100)},
			newSpec: map[string]any{"vni": int64(100)},
			wantErr: "trunk is immutable",
		},
		{
			name: "standalone update", kind: "L2VNI",
			oldSpec: map[string]any{"vni": int64(100)},
			newSpec: map[string]any{"vni": int64(100), "exportRTs": []any{"65000:100"}},
		},
		{
			name: "unchanged VF", kind: "L2Trunk",
			oldSpec: map[string]any{"type": "SRIOV", "sriov": trunkSRIOV("enp1s0", 1)},
			newSpec: map[string]any{"type": "SRIOV", "sriov": trunkSRIOV("enp1s0", 1)},
		},
		{
			name: "change VF", kind: "L2Trunk",
			oldSpec: map[string]any{"type": "SRIOV", "sriov": trunkSRIOV("enp1s0", 1)},
			newSpec: map[string]any{"type": "SRIOV", "sriov": trunkSRIOV("enp1s0", 2)},
			wantErr: "sriov is immutable",
		},
		{
			name: "change type", kind: "L2Trunk",
			oldSpec: map[string]any{"type": "Host"},
			newSpec: map[string]any{"type": "SRIOV", "sriov": trunkSRIOV("enp1s0", 1)},
			wantErr: "type is immutable",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gvk := parityGVK(tc.kind)
			obj := newUnstructured(tc.kind, tc.newSpec)
			old := newUnstructured(tc.kind, tc.oldSpec)
			errs, _ := validators[gvk].Validate(t.Context(), field.NewPath(""), schemas[gvk],
				obj.Object, old.Object, celconfig.RuntimeCELCostBudget)
			err := errs.ToAggregate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected validation error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func trunkHostMaster(kind, lifecycle string) map[string]any {
	bridge := map[string]any{"lifecycle": lifecycle}
	if lifecycle == "External" {
		bridge["name"] = "br-workloads"
	}
	fieldName := "linuxBridge"
	if kind == "OVSBridge" {
		fieldName = "ovsBridge"
	}
	return map[string]any{"type": kind, fieldName: bridge}
}

func trunkReference(name string, tag int64) map[string]any {
	return map[string]any{"name": name, "vlan": map[string]any{"tag": tag}}
}

func trunkSRIOV(pfName string, vfIndex int64) map[string]any {
	return map[string]any{"pfName": pfName, "vfIndex": vfIndex}
}
