// SPDX-License-Identifier: Apache-2.0

package bpf

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -cc clang -cflags "-O2 -g -Wall" NicIngress c/nic_ingress.c
//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -cc clang -cflags "-O2 -g -Wall" UlHostIngress c/ul_host_ingress.c
