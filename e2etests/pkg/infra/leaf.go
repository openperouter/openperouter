// SPDX-License-Identifier:Apache-2.0

package infra

import (
	"bytes"
	"fmt"
	"html/template"
	"net"
	"os"
	"path/filepath"
	"runtime"

	"github.com/openperouter/openperouter/e2etests/pkg/frr"
	corev1 "k8s.io/api/core/v1"
)

var (
	HostARedIPv4     string
	HostABlueIPv4    string
	HostADefaultIPv4 string
	HostBRedIPv4     string
	HostBBlueIPv4    string

	HostARedIPv6  string
	HostABlueIPv6 string
	HostBRedIPv6  string
	HostBBlueIPv6 string
)

var (
	LeafAConfig    Leaf
	LeafBConfig    Leaf
	LeafKindConfig LeafKind
)

func init() {
	topo := Topology()

	mustPeerIP := func(node, peer string, family IPFamily) string {
		ip, err := topo.GetPeerIP(node, peer, family)
		if err != nil {
			panic(fmt.Sprintf("topology: %v", err))
		}
		return stripCIDR(ip)
	}

	mustLinkIP := func(node, peer string, family IPFamily) string {
		ip, err := topo.GetLinkIP(node, peer, family)
		if err != nil {
			panic(fmt.Sprintf("topology: %v", err))
		}
		return stripCIDR(ip)
	}

	// Host IPs are the peer end of leaf interfaces
	HostARedIPv4 = mustPeerIP("leafA", "hostA_red", IPv4)
	HostABlueIPv4 = mustPeerIP("leafA", "hostA_blue", IPv4)
	HostADefaultIPv4 = mustPeerIP("leafA", "hostA_default", IPv4)
	HostBRedIPv4 = mustPeerIP("leafB", "hostB_red", IPv4)
	HostBBlueIPv4 = mustPeerIP("leafB", "hostB_blue", IPv4)

	HostARedIPv6 = mustPeerIP("leafA", "hostA_red", IPv6)
	HostABlueIPv6 = mustPeerIP("leafA", "hostA_blue", IPv6)
	HostBRedIPv6 = mustPeerIP("leafB", "hostB_red", IPv6)
	HostBBlueIPv6 = mustPeerIP("leafB", "hostB_blue", IPv6)

	// Leaf configurations
	leafA := topo.Nodes["leafA"]
	leafB := topo.Nodes["leafB"]

	LeafAConfig = Leaf{
		VTEPIP:       leafA.VTEPIP,
		SpineAddress: mustLinkIP("spine", "leafA", IPv4),
		ASN:          leafA.BGP.ASN,
		SpineASN:     leafA.BGP.Peers[0].ASN,
		Container:    LeafAContainer,
	}
	LeafBConfig = Leaf{
		VTEPIP:       leafB.VTEPIP,
		SpineAddress: mustLinkIP("spine", "leafB", IPv4),
		ASN:          leafB.BGP.ASN,
		SpineASN:     leafB.BGP.Peers[0].ASN,
		Container:    LeafBContainer,
	}
	LeafKindConfig = LeafKind{
		Container: KindLeafContainer,
	}
}

type LeafConfiguration struct {
	Leaf
	Red     Addresses
	Blue    Addresses
	Default Addresses
}

type LeafKindConfiguration struct {
	RedistributeConnected bool
	Neighbors             []string
	ASN                   uint32
	SpineAddress          string
	SpineASN              uint32
	KindNodeASN           uint32
	EnableBFD             bool
	Neighbors             []string
}

type Addresses struct {
	RedistributeConnected bool
	IPV4                  []string
	IPV6                  []string
}

type Leaf struct {
	VTEPIP       string
	SpineAddress string
	ASN          uint32
	SpineASN     uint32
	frr.Container
}

type LeafKind struct {
	frr.Container
}

func (l Leaf) VTEPPrefix() string {
	return l.VTEPIP + "/32"
}

// LeafConfigToFRR reads a Go template from the testdata directory and generates a string.
func LeafConfigToFRR(config LeafConfiguration) (string, error) {
	_, currentFile, _, _ := runtime.Caller(0) // current file's path
	templatePath := filepath.Join(filepath.Dir(currentFile), "testdata", "leaf.tmpl")

	// Read the template file
	tmplContent, err := os.ReadFile(templatePath)
	if err != nil {
		return "", err
	}

	tmpl, err := template.New("leaf.tmpl").Parse(string(tmplContent))
	if err != nil {
		return "", err
	}

	var result bytes.Buffer
	if err := tmpl.Execute(&result, config); err != nil {
		return "", err
	}

	return result.String(), nil
}

// LeafKindConfigToFRR reads a Go template from the testdata directory and generates a string for leafkind.
func LeafKindConfigToFRR(config LeafKindConfiguration) (string, error) {
	_, currentFile, _, _ := runtime.Caller(0) // current file's path
	templatePath := filepath.Join(filepath.Dir(currentFile), "testdata", "leafkind.tmpl")

	// Read the template file
	tmplContent, err := os.ReadFile(templatePath)
	if err != nil {
		return "", err
	}

	tmpl, err := template.New("leafkind.tmpl").Parse(string(tmplContent))
	if err != nil {
		return "", err
	}

	var result bytes.Buffer
	if err := tmpl.Execute(&result, config); err != nil {
		return "", err
	}

	return result.String(), nil
}

const EnableBFD = true

// UpdateLeafKindConfig updates the leafkind configuration file with the given configuration.
// It takes nodes and automatically builds the neighbors list from their IPs.
func UpdateLeafKindConfig(nodes []corev1.Node, enableBFD bool) error {
	topo := Topology()

	neighbors := []string{}
	for _, node := range nodes {
		neighborIP, err := NeighborIP(KindLeaf, node.Name)
		if err != nil {
			return err
		}
		neighbors = append(neighbors, neighborIP)
	}

	leafkind := topo.Nodes["leafkind"]

	config := LeafKindConfiguration{
		ASN:          leafkind.BGP.ASN,
		SpineAddress: stripCIDR(leafkind.BGP.Peers[0].IPv4Address),
		SpineASN:     leafkind.BGP.Peers[0].ASN,
		KindNodeASN:  64514,
		EnableBFD:    enableBFD,
		Neighbors:    neighbors,
	}

	configString, err := LeafKindConfigToFRR(config)
	if err != nil {
		return err
	}

	return LeafKindConfig.ReloadConfig(configString)
}

// ChangePrefixes updates the leaf configuration with the given prefixes for each VRF.
func (l Leaf) ChangePrefixes(defaultPrefixes, redPrefixes, bluePrefixes []string) error {
	defaultIPv4, defaultIPv6 := SeparateIPFamilies(defaultPrefixes)
	redIPv4, redIPv6 := SeparateIPFamilies(redPrefixes)
	blueIPv4, blueIPv6 := SeparateIPFamilies(bluePrefixes)

	leafConfiguration := LeafConfiguration{
		Leaf: l,
		Default: Addresses{
			IPV4: defaultIPv4,
			IPV6: defaultIPv6,
		},
		Red: Addresses{
			IPV4: redIPv4,
			IPV6: redIPv6,
		},
		Blue: Addresses{
			IPV4: blueIPv4,
			IPV6: blueIPv6,
		},
	}
	config, err := LeafConfigToFRR(leafConfiguration)
	if err != nil {
		return err
	}
	return l.ReloadConfig(config)
}

// RemovePrefixes removes all prefixes from the leaf configuration.
func (l Leaf) RemovePrefixes() error {
	return l.ChangePrefixes([]string{}, []string{}, []string{})
}

// SeparateIPFamilies separates a slice of CIDR prefixes into IPv4 and IPv6 slices
func SeparateIPFamilies(prefixes []string) ([]string, []string) {
	var ipv4Prefixes []string
	var ipv6Prefixes []string

	for _, prefix := range prefixes {
		_, ipNet, err := net.ParseCIDR(prefix)
		if err != nil {
			continue
		}

		if ipNet.IP.To4() != nil {
			ipv4Prefixes = append(ipv4Prefixes, prefix)
		} else {
			ipv6Prefixes = append(ipv6Prefixes, prefix)
		}
	}

	return ipv4Prefixes, ipv6Prefixes
}
