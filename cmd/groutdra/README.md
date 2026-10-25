# Grout DRA POC addressing

The POC catalog advertises eight synthetic DRA slots, `vhu-0` through `vhu-7`.
These are schedulable names; publishing them does not create Grout interfaces.
When a VM using a slot creates its vhost socket, the DRA driver creates the
corresponding Grout vhost interface and assigns that slot's gateway IP.

For example, two VMs could use these addresses:

| Link | VM address | Grout vhost address | VM gateway |
| --- | --- | --- | --- |
| VM A on `vhu-0` | `192.169.20.10/24` | `192.169.20.1/24` | `192.169.20.1` |
| VM B on `vhu-1` | `192.169.21.10/24` | `192.169.21.1/24` | `192.169.21.1` |

The VM addresses are examples; the DRA driver assigns only the Grout-side
addresses. To reach VM B, VM A sends traffic for `192.169.21.10` to its gateway,
`192.169.20.1`. Grout can forward it to VM B through the second vhost interface
when routing between the networks is configured. Kubernetes Pod IPs and VM
management IPs are separate from these links.

The `vhu-N` to `192.169.(20+N).1/24` mapping lives in
[`internal/groutdra/poc/catalog.go`](../../internal/groutdra/poc/catalog.go).
It is POC configuration, not a production IP allocation policy. The DRA port
setup and cleanup lifecycle can be reused with a production device catalog.
