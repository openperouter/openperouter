# Singlecluster test environment

## Topology

The single cluster topology is built as follows:

```bash
                                                   64612
                                               ┌───────────┐
                    ┌──────────────────────────┼           │
                    │                          │  Spine    ┼──────────────┐
                    │                   ┌──────┼           │              │
                    │                   │      └───────────┘              │
                    │                   │                                 │
                    │                   │                                 │
                    │                   │                                 │
                    │                   │                                 │
                    │                   │                                 │
             ┌──────┴────┐        ┌─────┴─────┐                     ┌─────┴─────┐
             │           │        │           │                     │           │
64520        │  Leaf A   │        │  Leaf B   │                     │  Leaf     │      64512
             │           │        │           │                     │  Kind     │
             └──┬──────┬─┘        └─┬───────┬─┘                     ┌───────────┐
                │      │            │       │                       │   Switch  │
           ┌────┴─┐  ┌─┴────┐  ┌────┴─┐  ┌──┴───┐                   └─┬───────┬─┘
           │ Host │  │ Host │  │ Host │  │ Host │                     │       │
           │ Red  │  │ Blue │  │ Red  │  │ Blue │                     │       │
           └──────┘  └──────┘  └──────┘  └──────┘           ┌─────────┴─┐   ┌─┴─────────┐
                                                            │           │   │           │
                                                            │  Kind     │   │  Kind     │   64514 (Open PE)
                                                            │  Worker   │   │  ControlP │
                                                            └───────────┘   └───────────┘
```

Both LeafA and LeafB are connected to two hosts, on red and blue networks corresponding to VNI 100 and 200 respectively. The LeafA and LeafB are connected to the Spine switch, which relays BGP and EVPN routes.

The Kind nodes are connected to a Leaf too, with the difference that both nodes and the leaf are connected to a switch (leaf-switch). This makes it
possible to have both nodes on the same subnet, emulating a real router.

The topology is able to simulate traffic across the fabric with different overlays.

## Interfaces and IPs

The interfaces are:

```
    - endpoints: ["leafA:eth1", "spine:eth1"]
    - endpoints: ["leafB:eth1", "spine:eth2"]
    - endpoints: ["leafkind:eth1", "spine:eth3"]
    - endpoints: ["leafA:ethred", "hostA_red:eth1"]
    - endpoints: ["leafA:ethblue", "hostA_blue:eth1"]
    - endpoints: ["leafB:ethred", "hostB_red:eth1"]
    - endpoints: ["leafB:ethblue", "hostB_blue:eth1"]
    - endpoints: ["leafkind:toswitch", "leafkind-switch:leaf2"]
    - endpoints: ["pe-kind-control-plane:toswitch", "leafkind-switch:kindctrlpl"]
    - endpoints: ["pe-kind-worker:toswitch", "leafkind-switch:kindworker"]
```

The ips are:

```
clab-kind-spine,eth1,192.168.1.0/31
clab-kind-spine,eth2,192.168.1.2/31
clab-kind-spine,eth3,192.168.1.4/31
clab-kind-leafA,eth1,192.168.1.1/31
clab-kind-leafB,eth1,192.168.1.3/31
clab-kind-leafkind,eth1,192.168.1.5/31
clab-kind-leafkind,toswitch,192.168.11.2/24
pe-kind-control-plane,toswitch,192.168.11.3/24
pe-kind-worker,toswitch,192.168.11.4/24
clab-kind-leafA,ethred,192.168.20.1/24
clab-kind-hostA_red,eth1,192.168.20.2/24
clab-kind-leafA,ethblue,192.168.21.1/24
clab-kind-hostA_blue,eth1,192.168.21.2/24
clab-kind-leafB,ethred,192.169.20.1/24
clab-kind-hostB_red,eth1,192.169.20.2/24
clab-kind-leafB,ethblue,192.169.21.1/24
clab-kind-hostB_blue,eth1,192.169.21.2/24
```
