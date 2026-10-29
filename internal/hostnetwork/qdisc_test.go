// SPDX-License-Identifier:Apache-2.0

//go:build runasroot

package hostnetwork

import (
	"errors"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openperouter/openperouter/internal/netnamespace"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netlink/nl"
	"github.com/vishvananda/netns"
	"golang.org/x/sys/unix"
)

var _ = Describe("VLAN bridge transmit queue", func() {
	var ns netns.NsHandle
	BeforeEach(func() { ns = createTestNS("qdisctestns") })
	AfterEach(func() {
		Expect(netns.DeleteNamed("qdisctestns")).To(Succeed())
		Expect(ns.Close()).To(Succeed())
	})

	It("creates fq_codel with positive queue length and repairs removed or wrong root queues", func() {
		Expect(netnamespace.In(ns, func() error {
			params := VNIParams{VNI: 6001}
			bridge, err := setupBridge(params)
			Expect(err).NotTo(HaveOccurred())
			current, err := netlink.LinkByIndex(bridge.Index)
			Expect(err).NotTo(HaveOccurred())
			Expect(current.Attrs().TxQLen).To(Equal(1000))
			root := rootBridgeQdisc(bridge)
			Expect(root).To(BeAssignableToTypeOf(&netlink.FqCodel{}))
			Expect(root.(*netlink.FqCodel).ECN).To(Equal(uint32(1)))
			Expect(netlink.QdiscDel(root)).To(Succeed())
			Expect(setupBridge(params)).To(HaveField("Name", bridge.Name))
			Expect(rootBridgeQdisc(bridge)).To(BeAssignableToTypeOf(&netlink.FqCodel{}))
			wrong := &netlink.PfifoFast{QdiscAttrs: netlink.QdiscAttrs{
				LinkIndex: bridge.Index, Parent: netlink.HANDLE_ROOT, Handle: netlink.MakeHandle(2, 0),
			}}
			Expect(netlink.QdiscReplace(wrong)).To(Succeed())
			Expect(rootBridgeQdisc(bridge)).To(BeAssignableToTypeOf(&netlink.PfifoFast{}))
			Expect(netlink.LinkSetTxQLen(bridge, 0)).To(Succeed())
			Expect(setupBridge(params)).To(HaveField("Name", bridge.Name))
			Expect(rootBridgeQdisc(bridge)).To(BeAssignableToTypeOf(&netlink.FqCodel{}))
			current, err = netlink.LinkByIndex(bridge.Index)
			Expect(err).NotTo(HaveOccurred())
			Expect(current.Attrs().TxQLen).To(Equal(1000))
			changedHandle := netlink.NewFqCodel(netlink.QdiscAttrs{
				LinkIndex: bridge.Index, Parent: netlink.HANDLE_ROOT, Handle: netlink.MakeHandle(2, 0),
			})
			Expect(netlink.QdiscReplace(changedHandle)).To(Succeed())
			Expect(setupBridge(params)).To(HaveField("Name", bridge.Name))
			Expect(rootBridgeQdisc(bridge).Attrs().Handle).To(Equal(netlink.MakeHandle(1, 0)))
			return nil
		})).To(Succeed())
	})

	It("preserves unchanged bridge and qdisc state without netlink writes", func() {
		Expect(netnamespace.In(ns, func() error {
			params := VNIParams{VNI: 6002}
			bridge, err := setupBridge(params)
			Expect(err).NotTo(HaveOccurred())
			root := rootBridgeQdisc(bridge)
			Expect(root.Type()).To(Equal("fq_codel"))
			socket, err := nl.Subscribe(unix.NETLINK_ROUTE, unix.RTNLGRP_TC)
			Expect(err).NotTo(HaveOccurred())
			defer socket.Close()
			Expect(socket.SetReceiveTimeout(&unix.Timeval{Usec: 100000})).To(Succeed())
			links := make(chan netlink.LinkUpdate, 16)
			done := make(chan struct{})
			defer close(done)
			Expect(netlink.LinkSubscribe(links, done)).To(Succeed())
			repeated, err := setupBridge(params)
			Expect(err).NotTo(HaveOccurred())
			Expect(repeated.Index).To(Equal(bridge.Index))
			Expect(rootBridgeQdisc(bridge).Attrs().Handle).To(Equal(root.Attrs().Handle))
			Expect(bridgeQueueUpdated(socket, bridge.Index)).To(BeFalse())
			Consistently(func() bool {
				select {
				case update := <-links:
					return update.Attrs().Index != bridge.Index
				default:
					return true
				}
			}).WithTimeout(200 * time.Millisecond).WithPolling(10 * time.Millisecond).Should(BeTrue())
			return nil
		})).To(Succeed())
	})
})

func rootBridgeQdisc(link netlink.Link) netlink.Qdisc {
	GinkgoHelper()
	queues, err := netlink.QdiscList(link)
	Expect(err).NotTo(HaveOccurred())
	for _, queue := range queues {
		if queue.Attrs().Parent == netlink.HANDLE_ROOT {
			return queue
		}
	}
	Fail("bridge has no root qdisc")
	return nil
}

func bridgeQueueUpdated(socket *nl.NetlinkSocket, index int) bool {
	GinkgoHelper()
	for {
		messages, _, err := socket.Receive()
		if errors.Is(err, unix.EAGAIN) {
			return false
		}
		Expect(err).NotTo(HaveOccurred())
		for _, message := range messages {
			if message.Header.Type != unix.RTM_NEWQDISC && message.Header.Type != unix.RTM_DELQDISC {
				continue
			}
			Expect(len(message.Data)).To(BeNumerically(">=", nl.SizeofTcMsg))
			if int(nl.DeserializeTcMsg(message.Data).Ifindex) == index {
				return true
			}
		}
	}
}
