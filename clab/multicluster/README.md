# Multi-cluster test environment

## Topology

The multi-cluster topology extends the fabric from the
[single cluster topology](../singlecluster/README.md) with two separate Kind
clusters:

```bash
                                                   64612
                                               ┌───────────┐
                    ┌──────────────────────────┼           │
                    │                          │  Spine    ┼─────────────────────────────────────────────┐
                    │                   ┌──────┼           ┼──────────────┐                              │
                    │                   │      └───────────┘              │                              │
                    │                   │                                 │                              │
                    │                   │                                 │                              │
                    │                   │                                 │                              │
                    │                   │                                 │                              │
                    │                   │                                 │                              │
             ┌──────┴────┐        ┌─────┴─────┐                     ┌─────┴─────┐                  ┌─────┴─────┐
             │           │        │           │                     │           │                  │           │
64520        │  Leaf A   │        │  Leaf B   │               64512 │  Leaf     │                  │  Leaf     │ 64516
             │           │        │           │                     │  Kind A   │                  │  Kind B   │
             └──┬──────┬─┘        └─┬───────┬─┘                     ┌───────────┐                  ┌───────────┐
                │      │            │       │                       │   Switch  │                  │   Switch  │
           ┌────┴─┐  ┌─┴────┐  ┌────┴─┐  ┌──┴───┐                   └─┬───────┬─┘                  └─┬───────┬─┘
           │ Host │  │ Host │  │ Host │  │ Host │                     │       │                      │       │
           │ Red  │  │ Blue │  │ Red  │  │ Blue │           ┌─────────┴─┐   ┌─┴─────────┐  ┌─────────┴─┐   ┌─┴─────────┐
           └──────┘  └──────┘  └──────┘  └──────┘           │  Kind     │   │  Kind     │  │  Kind     │   │  Kind     │
                                                            │  Worker A │   │ControlP A │  │  Worker B │   │ControlP B │
                                                            └───────────┘   └───────────┘  └───────────┘   └───────────┘
                                                                 64514 (Open PE)                     64518 (Open PE)
```

In the multi-cluster setup:
- **Leaf Kind A** (AS 64512) connects to Kind cluster A nodes (AS 64514)
- **Leaf Kind B** (AS 64516) connects to Kind cluster B nodes (AS 64518)
- Both leaf switches connect to the same spine (AS 64612) for inter-cluster communication

## Interfaces and IPs

The interfaces are:

```
    - endpoints: ["leafA:eth1", "spine:eth1"]
    - endpoints: ["leafB:eth1", "spine:eth2"]
    - endpoints: ["leafkind-a:eth1", "spine:eth3"]
    - endpoints: ["leafkind-b:eth1", "spine:eth4"]
    - endpoints: ["leafA:ethred", "hostA_red:eth1"]
    - endpoints: ["leafA:ethdefault", "hostA_default:eth1"]
    - endpoints: ["leafA:ethblue", "hostA_blue:eth1"]
    - endpoints: ["leafB:ethred", "hostB_red:eth1"]
    - endpoints: ["leafB:ethblue", "hostB_blue:eth1"]
    - endpoints: ["leafkind-a:toswitch", "leafkind-sw-a:leaf2"]
    - endpoints: ["leafkind-b:toswitch", "leafkind-sw-b:leaf3"]
    - endpoints: ["pe-kind-a-control-plane:toswitch", "leafkind-sw-a:kindctrlpla"]
    - endpoints: ["pe-kind-b-control-plane:toswitch", "leafkind-sw-b:kindctrlplb"]
    - endpoints: ["pe-kind-a-worker:toswitch", "leafkind-sw-a:kindworkera"]
    - endpoints: ["pe-kind-b-worker:toswitch", "leafkind-sw-b:kindworkerb"]
```

The ips are:

```
clab-kind-spine,eth1,192.168.1.0/31
clab-kind-spine,eth2,192.168.1.2/31
clab-kind-spine,eth3,192.168.1.4/31
clab-kind-spine,eth4,192.168.1.6/31
clab-kind-leafA,eth1,192.168.1.1/31
clab-kind-leafB,eth1,192.168.1.3/31
clab-kind-leafkind-a,eth1,192.168.1.5/31
clab-kind-leafkind-a,toswitch,192.168.11.2/24
clab-kind-leafkind-b,eth1,192.168.1.7/31
clab-kind-leafkind-b,toswitch,192.168.12.2/24
pe-kind-a-control-plane,toswitch,192.168.11.3/24
pe-kind-a-worker,toswitch,192.168.11.4/24
pe-kind-b-control-plane,toswitch,192.168.12.3/24
pe-kind-b-worker,toswitch,192.168.12.4/24
clab-kind-leafA,ethred,192.168.20.1/24
clab-kind-leafA,ethred,2001:db8:20::1/64
clab-kind-hostA_red,eth1,192.168.20.2/24
clab-kind-hostA_red,eth1,2001:db8:20::2/64
clab-kind-leafA,ethblue,192.168.21.1/24
clab-kind-leafA,ethblue,2001:db8:21::1/64
clab-kind-hostA_blue,eth1,192.168.21.2/24
clab-kind-hostA_blue,eth1,2001:db8:21::2/64
clab-kind-leafA,ethdefault,192.168.22.1/24
clab-kind-hostA_default,eth1,192.168.22.2/24
clab-kind-leafB,ethred,192.169.20.1/24
clab-kind-leafB,ethred,2001:db8:169:20::1/64
clab-kind-hostB_red,eth1,192.169.20.2/24
clab-kind-hostB_red,eth1,2001:db8:169:20::2/64
clab-kind-leafB,ethblue,192.169.21.1/24
clab-kind-leafB,ethblue,2001:db8:169:21::1/64
clab-kind-hostB_blue,eth1,192.169.21.2/24
clab-kind-hostB_blue,eth1,2001:db8:169:21::2/64
```
