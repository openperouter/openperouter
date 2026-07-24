// SPDX-License-Identifier:Apache-2.0

package dhcp

import (
	"testing"
)

func TestIPAMTypeSingleConfDHCP(t *testing.T) {
	config := `{"cniVersion":"1.0.0","name":"n","type":"macvlan","ipam":{"type":"dhcp"}}`
	got, err := IPAMType([]byte(config))
	if err != nil {
		t.Fatal(err)
	}
	if got != IPAMTypeDHCP {
		t.Errorf("IPAMType() = %q, want %q", got, IPAMTypeDHCP)
	}
}

func TestIPAMTypeConflistDHCP(t *testing.T) {
	config := `{"cniVersion":"1.0.0","name":"n","plugins":[{"type":"macvlan","ipam":{"type":"dhcp"}}]}`
	got, err := IPAMType([]byte(config))
	if err != nil {
		t.Fatal(err)
	}
	if got != IPAMTypeDHCP {
		t.Errorf("IPAMType() = %q, want %q", got, IPAMTypeDHCP)
	}
}

func TestIPAMTypeConflistStatic(t *testing.T) {
	config := `{"cniVersion":"1.0.0","name":"n","plugins":[{"type":"macvlan","ipam":{"type":"static"}}]}`
	got, err := IPAMType([]byte(config))
	if err != nil {
		t.Fatal(err)
	}
	if got != "static" {
		t.Errorf("IPAMType() = %q, want %q", got, "static")
	}
}

func TestIPAMTypeNoIPAM(t *testing.T) {
	config := `{"cniVersion":"1.0.0","name":"n","plugins":[{"type":"bridge"}]}`
	got, err := IPAMType([]byte(config))
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Errorf("IPAMType() = %q, want empty", got)
	}
}

func TestIPAMTypeInvalidJSON(t *testing.T) {
	_, err := IPAMType([]byte(`{`))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}
