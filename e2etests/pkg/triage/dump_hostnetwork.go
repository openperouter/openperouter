// SPDX-License-Identifier:Apache-2.0

package triage

import (
	"context"
	"fmt"
	"io"
	"maps"
	"net"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/openperouter/openperouter/e2etests/pkg/executor"
	"github.com/openperouter/openperouter/e2etests/pkg/k8s"
	"github.com/openperouter/openperouter/e2etests/pkg/openperouter"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
)

type hostNetworkCommand struct {
	description string
	command     []string
}

func dumpHostNetworkInfo(basePath, testName string, cs clientset.Interface, namespaces ...string) {
	testPath, err := createTestOutput(basePath, testName)
	if err != nil {
		ginkgo.GinkgoWriter.Printf("dumpHostNetworkInfo: failed to create test dir: %v", err)
		return
	}

	nodes, err := k8s.GetNodes(cs)
	if err != nil {
		ginkgo.GinkgoWriter.Printf("dumpHostNetworkInfo: failed to get nodes: %v", err)
		return
	}

	for _, node := range nodes {
		nodeExec := executor.ForNode(node.Name)
		dumpNodeNetworkCommands(testPath, node.Name, nodeExec, hostNetworkCommands())
		dumpNodeNetworkCommands(testPath, node.Name, nodeExec, routerNetworkCommands())
		dumpOVSInfo(testPath, node.Name, nodeExec)
	}

	dumpOverlayReachabilityInfo(testPath, cs, namespaces)
}

func hostNetworkCommands() []hostNetworkCommand {
	return []hostNetworkCommand{
		{"kernel version", []string{"uname", "-a"}},
		{"detailed links", []string{"ip", "-details", "link", "show"}},
		{"addresses", []string{"ip", "-details", "address", "show"}},
		{"detailed bridge links", []string{"bridge", "-details", "link", "show"}},
		{"bridge forwarding database", []string{"bridge", "fdb", "show"}},
		{"bridge VLANs", []string{"bridge", "vlan", "show"}},
		{"timestamped neighbors", []string{"ip", "-ts", "neigh", "show"}},
		{"link statistics", []string{"ip", "-stats", "link", "show"}},
		{"OVS version", []string{"ovs-vsctl", "--version"}},
		{"OVS database", []string{"ovs-vsctl", "show"}},
	}
}

func routerNetworkCommands() []hostNetworkCommand {
	netnsPrefix := []string{"ip", "netns", "exec", openperouter.NamedNetns}
	return []hostNetworkCommand{
		{"router namespace detailed links", append(netnsPrefix, "ip", "-details", "link", "show")},
		{"router namespace addresses", append(netnsPrefix, "ip", "-details", "address", "show")},
		{"router namespace detailed bridge links", append(netnsPrefix, "bridge", "-details", "link", "show")},
		{"router namespace bridge forwarding database", append(netnsPrefix, "bridge", "fdb", "show")},
		{"router namespace bridge VLANs", append(netnsPrefix, "bridge", "vlan", "show")},
		{"router namespace timestamped neighbors", append(netnsPrefix, "ip", "-ts", "neigh", "show")},
		{"router namespace link statistics", append(netnsPrefix, "ip", "-stats", "link", "show")},
	}
}

func dumpNodeNetworkCommands(testPath, nodeName string, nodeExec executor.Executor, commands []hostNetworkCommand) {
	fileName := fmt.Sprintf("hostnetworkdump-node-%s", nodeName)
	f, err := logFileFor(testPath, fileName)
	if err != nil {
		ginkgo.GinkgoWriter.Printf("dumpNodeNetworkCommands: failed to open %s: %v", fileName, err)
		return
	}
	defer func() {
		if err := f.Close(); err != nil {
			ginkgo.GinkgoWriter.Printf("dumpNodeNetworkCommands: failed to close %s: %v", fileName, err)
		}
	}()

	for _, command := range commands {
		fmt.Fprintf(f, "\n######## %s\n\n", command.description)
		out, err := nodeExec.Exec(command.command[0], command.command[1:]...)
		if err != nil {
			fmt.Fprintf(f, "Failed to run %q: %v\n", strings.Join(command.command, " "), err)
		}
		fmt.Fprint(f, out)
	}
}

func dumpOVSInfo(testPath, nodeName string, nodeExec executor.Executor) {
	bridgesOutput, err := nodeExec.Exec("ovs-vsctl", "list-br")
	if err != nil {
		fileName := fmt.Sprintf("ovsdump-node-%s", nodeName)
		f, fileErr := logFileFor(testPath, fileName)
		if fileErr != nil {
			ginkgo.GinkgoWriter.Printf("dumpOVSInfo: failed to open %s: %v", fileName, fileErr)
			return
		}
		fmt.Fprintf(f, "Failed to list OVS bridges: %v\n", err)
		if closeErr := f.Close(); closeErr != nil {
			ginkgo.GinkgoWriter.Printf("dumpOVSInfo: failed to close %s: %v", fileName, closeErr)
		}
		return
	}

	var commands []hostNetworkCommand
	for _, bridge := range strings.Fields(bridgesOutput) {
		portsOutput, err := nodeExec.Exec("ovs-vsctl", "list-ports", bridge)
		if err != nil {
			portsOutput = ""
		}
		commands = append(commands,
			hostNetworkCommand{
				description: fmt.Sprintf("OVS bridge %s database record", bridge),
				command:     []string{"ovs-vsctl", "list", "Bridge", bridge},
			},
			hostNetworkCommand{
				description: fmt.Sprintf("OVS bridge %s port list", bridge),
				command:     []string{"ovs-vsctl", "list-ports", bridge},
			},
			hostNetworkCommand{
				description: fmt.Sprintf("OVS bridge %s OpenFlow flows", bridge),
				command:     []string{"ovs-ofctl", "dump-flows", bridge},
			},
			hostNetworkCommand{
				description: fmt.Sprintf("OVS bridge %s OpenFlow port descriptions", bridge),
				command:     []string{"ovs-ofctl", "dump-ports-desc", bridge},
			},
			hostNetworkCommand{
				description: fmt.Sprintf("OVS bridge %s OpenFlow port statistics", bridge),
				command:     []string{"ovs-ofctl", "dump-ports", bridge},
			},
			hostNetworkCommand{
				description: fmt.Sprintf("OVS bridge %s forwarding database", bridge),
				command:     []string{"ovs-appctl", "fdb/show", bridge},
			},
			hostNetworkCommand{
				description: fmt.Sprintf("OVS bridge %s datapath flows", bridge),
				command:     []string{"ovs-appctl", "dpif/dump-flows", bridge},
			},
		)
		for _, port := range strings.Fields(portsOutput) {
			commands = append(commands,
				hostNetworkCommand{
					description: fmt.Sprintf("OVS bridge %s port %s database record", bridge, port),
					command:     []string{"ovs-vsctl", "list", "Port", port},
				},
				hostNetworkCommand{
					description: fmt.Sprintf("OVS bridge %s interface %s database record", bridge, port),
					command:     []string{"ovs-vsctl", "list", "Interface", port},
				},
			)
		}
	}
	if len(commands) == 0 {
		return
	}

	dumpNodeNetworkCommands(testPath, nodeName, nodeExec, commands)
}

type overlayCapture struct {
	name    string
	exec    executor.Executor
	command []string
	output  string
	err     error
}

// overlayProbe is everything the reachability probe produced for one namespace.
type overlayProbe struct {
	namespace       string
	sourceNode      string
	destinationNode string
	destinationIP   string
	filter          string
	pingOutput      string
	pingErr         error
	captures        []overlayCapture
	states          []overlayStateDelta
}

// overlayStateDelta pairs the router namespace state of one node taken before
// the probe traffic with the one taken after it.
type overlayStateDelta struct {
	role   string
	before overlayNodeState
	after  overlayNodeState
}

type overlayNodeState struct {
	nodeName string
	fdb      string
	fdbErr   error
	stats    map[string]linkStats
	statsErr error
}

type linkStats struct {
	rxPackets, rxDropped int64
	txPackets, txDropped int64
}

var linkHeaderPattern = regexp.MustCompile(`^\d+:\s+([^:@\s]+)[@:]`)

func dumpOverlayReachabilityInfo(testPath string, cs clientset.Interface, namespaces []string) {
	for _, namespace := range namespaces {
		pods, err := cs.CoreV1().Pods(namespace).List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			ginkgo.GinkgoWriter.Printf("dumpOverlayReachabilityInfo: failed to list pods in namespace %s: %v", namespace, err)
			continue
		}

		var firstPod, secondPod *corev1.Pod
		for i := range pods.Items {
			switch pods.Items[i].Name {
			case "pod1":
				firstPod = &pods.Items[i]
			case "pod2":
				secondPod = &pods.Items[i]
			}
		}
		if firstPod == nil || secondPod == nil ||
			firstPod.Spec.NodeName == "" || secondPod.Spec.NodeName == "" {
			continue
		}

		firstPodExec := executor.ForPod(firstPod.Namespace, firstPod.Name, "agnhost")
		secondPodExec := executor.ForPod(secondPod.Namespace, secondPod.Name, "agnhost")
		destinationIP, err := podAddressOnNet1(secondPodExec)
		if err != nil {
			ginkgo.GinkgoWriter.Printf("dumpOverlayReachabilityInfo: failed to get pod2 address in namespace %s: %v", namespace, err)
			continue
		}

		filter := "arp host " + destinationIP
		if strings.Contains(destinationIP, ":") {
			filter = "icmp6"
		}
		probe := overlayProbe{
			namespace:       namespace,
			sourceNode:      firstPod.Spec.NodeName,
			destinationNode: secondPod.Spec.NodeName,
			destinationIP:   destinationIP,
			filter:          filter,
		}

		// Snapshot before any probe traffic: this is the state the reachability
		// check was failing against. A dump taken only afterwards cannot tell a
		// data path that was never programmed from one that converged late.
		probe.states = []overlayStateDelta{
			{role: "source", before: captureOverlayNodeState(probe.sourceNode)},
			{role: "destination", before: captureOverlayNodeState(probe.destinationNode)},
		}

		probe.captures = overlayCaptures(probe.sourceNode, probe.destinationNode, filter)

		var waitGroup sync.WaitGroup
		for i := range probe.captures {
			capture := &probe.captures[i]
			waitGroup.Add(1)
			go func() {
				defer waitGroup.Done()
				capture.output, capture.err = capture.exec.Exec(capture.command[0], capture.command[1:]...)
			}()
		}

		time.Sleep(2 * time.Second)
		probe.pingOutput, probe.pingErr = firstPodExec.Exec(
			"ping", "-c", "3", "-W", "1", "-I", "net1", destinationIP)
		waitGroup.Wait()

		for i := range probe.states {
			probe.states[i].after = captureOverlayNodeState(probe.states[i].before.nodeName)
		}

		writeOverlayProbeResult(testPath, probe)
	}
}

func overlayCaptures(sourceNode, destinationNode, filter string) []overlayCapture {
	captureCommand := []string{
		"timeout", "12s", "tcpdump", "-i", "any", "-nn", "-e", "-tttt", filter,
	}
	routerCaptureCommand := []string{
		"ip", "netns", "exec", openperouter.NamedNetns,
		"timeout", "12s", "tcpdump", "-i", "any", "-nn", "-e", "-tttt", filter,
	}

	return []overlayCapture{
		{
			name:    "source-node-host",
			exec:    executor.ForNodeCapture(sourceNode),
			command: captureCommand,
		},
		{
			name:    "destination-node-host",
			exec:    executor.ForNodeCapture(destinationNode),
			command: captureCommand,
		},
		{
			name:    "source-node-router-namespace",
			exec:    executor.ForNodeCapture(sourceNode),
			command: routerCaptureCommand,
		},
		{
			name:    "destination-node-router-namespace",
			exec:    executor.ForNodeCapture(destinationNode),
			command: routerCaptureCommand,
		},
	}
}

func podAddressOnNet1(podExec executor.Executor) (string, error) {
	var outputs []string
	for _, family := range []string{"-4", "-6"} {
		output, err := podExec.Exec("ip", "-o", family, "address", "show", "dev", "net1", "scope", "global")
		if err != nil {
			outputs = append(outputs, fmt.Sprintf("ip %s address: %v\n%s", family, err, output))
			continue
		}
		if address, found := firstAddress(output); found {
			return address, nil
		}
	}
	return "", fmt.Errorf("no global address found on net1\n%s", strings.Join(outputs, "\n"))
}

func firstAddress(output string) (string, bool) {
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		for i := 0; i < len(fields)-1; i++ {
			if fields[i] != "inet" && fields[i] != "inet6" {
				continue
			}
			address := strings.Split(fields[i+1], "/")[0]
			if net.ParseIP(address) != nil {
				return address, true
			}
		}
	}
	return "", false
}

func writeOverlayProbeResult(testPath string, probe overlayProbe) {
	fileName := fmt.Sprintf("overlay-probe-namespace-%s", probe.namespace)
	f, err := logFileFor(testPath, fileName)
	if err != nil {
		ginkgo.GinkgoWriter.Printf("writeOverlayProbeResult: failed to open %s: %v", fileName, err)
		return
	}
	defer func() {
		if err := f.Close(); err != nil {
			ginkgo.GinkgoWriter.Printf("writeOverlayProbeResult: failed to close %s: %v", fileName, err)
		}
	}()

	fmt.Fprintf(f, "Overlay reachability probe\n")
	fmt.Fprintf(f, "namespace: %s\n", probe.namespace)
	fmt.Fprintf(f, "source node: %s\n", probe.sourceNode)
	fmt.Fprintf(f, "destination node: %s\n", probe.destinationNode)
	fmt.Fprintf(f, "destination address: %s\n", probe.destinationIP)
	fmt.Fprintf(f, "capture filter: %s\n\n", probe.filter)

	fmt.Fprintf(f, "######## probe result\n\n")
	if probe.pingErr != nil {
		fmt.Fprintf(f, "probe command failed: %v\n", probe.pingErr)
	}
	fmt.Fprintf(f, "%s\n", probe.pingOutput)

	for _, state := range probe.states {
		writeOverlayNodeState(f, state)
	}

	for _, capture := range probe.captures {
		fmt.Fprintf(f, "\n######## %s capture\n\n", capture.name)
		fmt.Fprintf(f, "command: %q\n", strings.Join(capture.command, " "))
		if capture.err != nil {
			fmt.Fprintf(f, "capture command failed: %v\n", capture.err)
		}
		fmt.Fprintf(f, "%s\n", capture.output)
	}
}

// captureOverlayNodeState reads the two things that say whether a router
// namespace can forward overlay traffic: the forwarding entries that carry a
// remote VXLAN destination, and the per-interface counters.
func captureOverlayNodeState(nodeName string) overlayNodeState {
	nodeExec := executor.ForNode(nodeName)
	state := overlayNodeState{nodeName: nodeName}

	fdb, err := nodeExec.Exec("ip", "netns", "exec", openperouter.NamedNetns, "bridge", "fdb", "show")
	state.fdb, state.fdbErr = remoteFDBEntries(fdb), err

	stats, err := nodeExec.Exec("ip", "netns", "exec", openperouter.NamedNetns, "ip", "-s", "link", "show")
	state.stats, state.statsErr = parseLinkStats(stats), err

	return state
}

// writeOverlayNodeState reports the forwarding state the node had while the
// reachability check was failing, and how its counters moved while the probe
// ran. An empty FDB means the control plane never programmed the peer; a
// populated FDB with no counter movement on the receiving side means the frames
// were sent but never arrived.
func writeOverlayNodeState(f io.Writer, state overlayStateDelta) {
	fmt.Fprintf(f, "\n######## %s node %s router namespace: remote forwarding entries before probe\n\n",
		state.role, state.before.nodeName)
	switch {
	case state.before.fdbErr != nil:
		fmt.Fprintf(f, "failed to read the forwarding database: %v\n", state.before.fdbErr)
	case state.before.fdb == "":
		fmt.Fprintf(f, "no forwarding entry has a remote VXLAN destination\n")
	default:
		fmt.Fprint(f, state.before.fdb)
	}
	if state.after.fdb != state.before.fdb {
		fmt.Fprintf(f, "\nthe entries changed while the probe ran, after:\n\n%s", state.after.fdb)
	}

	fmt.Fprintf(f, "\n######## %s node %s router namespace: counter delta across probe\n\n",
		state.role, state.before.nodeName)
	writeLinkStatsDelta(f, state.before, state.after)
}

func writeLinkStatsDelta(f io.Writer, before, after overlayNodeState) {
	if before.statsErr != nil {
		fmt.Fprintf(f, "failed to read the counters before the probe: %v\n", before.statsErr)
	}
	if after.statsErr != nil {
		fmt.Fprintf(f, "failed to read the counters after the probe: %v\n", after.statsErr)
	}

	wroteHeader := false
	for _, name := range slices.Sorted(maps.Keys(after.stats)) {
		previous, found := before.stats[name]
		current := after.stats[name]
		if !found || current == previous {
			continue
		}
		if !wroteHeader {
			fmt.Fprintf(f, "%-18s %9s %9s %9s %9s\n", "interface", "rx_pkts", "rx_drop", "tx_pkts", "tx_drop")
			wroteHeader = true
		}
		fmt.Fprintf(f, "%-18s %+9d %+9d %+9d %+9d\n", name,
			current.rxPackets-previous.rxPackets, current.rxDropped-previous.rxDropped,
			current.txPackets-previous.txPackets, current.txDropped-previous.txDropped)
	}
	if !wroteHeader {
		fmt.Fprintf(f, "no interface counter moved while the probe ran\n")
	}
}

// remoteFDBEntries keeps the forwarding entries that point at a remote VXLAN
// endpoint. Those are the ones unicast forwarding and head-end replication ride
// on, and they are a small fraction of a full dump.
func remoteFDBEntries(fdb string) string {
	var entries strings.Builder
	for line := range strings.SplitSeq(fdb, "\n") {
		if !strings.Contains(line, " dst ") {
			continue
		}
		entries.WriteString(line)
		entries.WriteString("\n")
	}
	return entries.String()
}

// parseLinkStats pulls the per-interface counters out of "ip -s link show".
// Each RX:/TX: header is followed by the counter row and then, on recent
// iproute2, a second row holding the extended counters: only the first is read.
func parseLinkStats(output string) map[string]linkStats {
	parsed := map[string]linkStats{}
	var name, section string
	for line := range strings.SplitSeq(output, "\n") {
		if match := linkHeaderPattern.FindStringSubmatch(line); match != nil {
			name, section = match[1], ""
			continue
		}
		switch {
		case strings.Contains(line, "RX:"):
			section = "RX"
			continue
		case strings.Contains(line, "TX:"):
			section = "TX"
			continue
		}
		if name == "" || section == "" {
			continue
		}
		packets, dropped, ok := packetsAndDropped(line)
		if !ok {
			continue
		}
		stats := parsed[name]
		if section == "RX" {
			stats.rxPackets, stats.rxDropped = packets, dropped
		} else {
			stats.txPackets, stats.txDropped = packets, dropped
		}
		parsed[name] = stats
		section = ""
	}
	return parsed
}

// packetsAndDropped reads the "bytes packets errors dropped ..." counter row.
func packetsAndDropped(line string) (int64, int64, bool) {
	fields := strings.Fields(line)
	if len(fields) < 4 {
		return 0, 0, false
	}
	counters := make([]int64, 0, 4)
	for _, field := range fields[:4] {
		counter, err := strconv.ParseInt(field, 10, 64)
		if err != nil {
			return 0, 0, false
		}
		counters = append(counters, counter)
	}
	return counters[1], counters[3], true
}
