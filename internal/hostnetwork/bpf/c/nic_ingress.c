// SPDX-License-Identifier: Apache-2.0

// TC ingress program attached to the physical NIC.
// Steers ARP, BGP/BFD, and VXLAN traffic from the NIC to the router pod
// via the ul-host veth.

#include <linux/bpf.h>
#include <linux/pkt_cls.h>
#include <linux/if_ether.h>
#include <linux/in.h>
#include <linux/ip.h>
#include <linux/tcp.h>
#include <linux/udp.h>

#include <bpf/bpf_helpers.h>
#include <bpf/bpf_endian.h>

// Minimal ARP header definition to avoid pulling in linux/if_arp.h
// which drags in system headers incompatible with BPF compilation.
struct arp_hdr {
	__be16 ar_hrd;  // format of hardware address
	__be16 ar_pro;  // format of protocol address
	__u8   ar_hln;  // length of hardware address
	__u8   ar_pln;  // length of protocol address
	__be16 ar_op;   // ARP opcode
} __attribute__((packed));

// neighbor_map: hash map of neighbor IPv4 addresses we care about.
// key = __be32 (network-byte-order IPv4), value = __u8 (just a flag).
struct {
	__uint(type, BPF_MAP_TYPE_HASH);
	__type(key, __be32);
	__type(value, __u8);
	__uint(max_entries, 64);
} neighbor_map SEC(".maps");

// vni_map: hash map of allowed VXLAN VNIs.
// key = __u32 (host-byte-order VNI), value = __u8 (flag).
struct {
	__uint(type, BPF_MAP_TYPE_HASH);
	__type(key, __u32);
	__type(value, __u8);
	__uint(max_entries, 1024);
} vni_map SEC(".maps");

// config_map: array for runtime config.
// key 0 = ul-host ifindex.
struct {
	__uint(type, BPF_MAP_TYPE_ARRAY);
	__type(key, __u32);
	__type(value, __u32);
	__uint(max_entries, 1);
} config_map SEC(".maps");

#define BGP_PORT 179
#define BFD_CTRL_PORT 3784
#define BFD_ECHO_PORT 4784
#define VXLAN_PORT 4789

SEC("tc")
int nic_ingress(struct __sk_buff *skb) {
	__u32 key = 0;
	__u32 *ul_host_ifindex = bpf_map_lookup_elem(&config_map, &key);
	if (!ul_host_ifindex)
		return TC_ACT_OK;

	void *data = (void *)(long)skb->data;
	void *data_end = (void *)(long)skb->data_end;

	// Parse Ethernet header
	struct ethhdr *eth = data;
	if ((void *)(eth + 1) > data_end)
		return TC_ACT_OK;

	__u16 eth_type = bpf_ntohs(eth->h_proto);

	// ARP (0x0806): clone to ul-host so both host and router pod see it.
	// All ARPs are cloned unconditionally because the router pod needs to
	// resolve MACs for remote VTEPs (not just BGP neighbors).
	if (eth_type == ETH_P_ARP) {
		bpf_clone_redirect(skb, *ul_host_ifindex, 0);
		return TC_ACT_OK;
	}

	// Not IPv4 -> pass
	if (eth_type != ETH_P_IP)
		return TC_ACT_OK;

	// Parse IPv4
	struct iphdr *ip = (void *)(eth + 1);
	if ((void *)(ip + 1) > data_end)
		return TC_ACT_OK;

	// Validate IP header length
	__u32 ip_hdr_len = ip->ihl * 4;
	if (ip_hdr_len < sizeof(struct iphdr))
		return TC_ACT_OK;
	if ((void *)ip + ip_hdr_len > data_end)
		return TC_ACT_OK;

	__be32 src_ip = ip->saddr;
	__u8 protocol = ip->protocol;

	// TCP: BGP (port 179)
	if (protocol == IPPROTO_TCP) {
		struct tcphdr *tcp = (void *)ip + ip_hdr_len;
		if ((void *)(tcp + 1) > data_end)
			return TC_ACT_OK;

		__u16 sport = bpf_ntohs(tcp->source);
		__u16 dport = bpf_ntohs(tcp->dest);

		if ((sport == BGP_PORT || dport == BGP_PORT) &&
		    bpf_map_lookup_elem(&neighbor_map, &src_ip)) {
			return bpf_redirect(*ul_host_ifindex, 0);
		}
		return TC_ACT_OK;
	}

	// UDP: BFD or VXLAN
	if (protocol == IPPROTO_UDP) {
		struct udphdr *udp = (void *)ip + ip_hdr_len;
		if ((void *)(udp + 1) > data_end)
			return TC_ACT_OK;

		__u16 dport = bpf_ntohs(udp->dest);

		// BFD control or echo
		if (dport == BFD_CTRL_PORT || dport == BFD_ECHO_PORT) {
			if (bpf_map_lookup_elem(&neighbor_map, &src_ip))
				return bpf_redirect(*ul_host_ifindex, 0);
			return TC_ACT_OK;
		}

		// VXLAN
		if (dport == VXLAN_PORT) {
			// Parse VXLAN header (8 bytes after UDP header)
			void *vxlan_ptr = (void *)(udp + 1);
			if (vxlan_ptr + 8 > data_end)
				return TC_ACT_OK;

			// VXLAN bytes 4-6 contain VNI, byte 7 is reserved
			// Read bytes 4-7 as __be32, VNI = upper 24 bits
			__be32 vni_word;
			__builtin_memcpy(&vni_word, vxlan_ptr + 4, 4);
			__u32 vni = bpf_ntohl(vni_word) >> 8;

			if (bpf_map_lookup_elem(&vni_map, &vni))
				return bpf_redirect(*ul_host_ifindex, 0);
			return TC_ACT_OK;
		}
	}

	return TC_ACT_OK;
}

char _license[] SEC("license") = "Apache-2.0";
