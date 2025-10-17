# Test environment

The test environment supports two different topologies:
1. [Single Cluster Topology](singlecluster/README.md) - Uses one Kind cluster connected to the fabric
2. [Multi Cluster Topology](multicluster/README.md) - Uses two Kind clusters (Leaf Kind A and Leaf Kind B) connected to separate leaf switches

## FRR-K8s

FRR-K8s is a Kubernetes controller that allows you to run FRR in a Kubernetes cluster. Given the low level nature of its API, it's deployed on the test environent to validate the interaction between the Open PE router and a BGP speaking component running on the host.

## Setting up the environment

Either running `make deploy` from the project root, or the local [./setup](./setup.sh) script, will deploy the test environment.
The script will setup a clab instance together with a kind cluster.

## Interfaces and IPs

Each variant has different interfaces and IPs. You can find them listed in each
variant's README:
- [singlecluster](singlecluster/README.md#interfaces-and-ips) interfaces and IPs list
- [multicluster](multicluster/README.md#interfaces-and-ips) interfaces and IPs list
