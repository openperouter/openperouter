// SPDX-License-Identifier: Apache-2.0

// TC ingress program attached to ul-host veth.
// Forwards all traffic from the router pod (via ul-pe -> ul-host) to the
// physical NIC egress (wire).

#include <linux/bpf.h>
#include <linux/pkt_cls.h>

#include <bpf/bpf_helpers.h>

// config_map: array for runtime config.
// key 0 = physical NIC ifindex.
struct {
	__uint(type, BPF_MAP_TYPE_ARRAY);
	__type(key, __u32);
	__type(value, __u32);
	__uint(max_entries, 1);
} config_map SEC(".maps");

SEC("tc")
int ul_host_ingress(struct __sk_buff *skb) {
	__u32 key = 0;
	__u32 *nic_ifindex = bpf_map_lookup_elem(&config_map, &key);
	if (!nic_ifindex)
		return TC_ACT_OK;

	return bpf_redirect(*nic_ifindex, 0);
}

char _license[] SEC("license") = "Apache-2.0";
