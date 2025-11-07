# Running on the host

## Summary

This enhancement proposes an operating mode of OpenPERouter where the router is able to start at boot time and to provide connectivity to the VPN network before the Kubernetes machinery starts.

## Motivation

Running the nodes completely out of VPN does not work with the current design. In order to configure the router, we need to fetch the configuration from the API server, but in order to reach the API server we need to configure the router.

To solve this chicken-egg problem we need an operating mode where the router is allowed to start as soon as possible and to be fed with a static configuration used to provide the basic connectivity required to bootstrap kubernetes.

## Goals

- Running the router before the kubelet and the cluster's primary CNI
- Providing a way to configure the router statically
- Providing a way to extend the staic configuration with a configuration provided via the current kubernetes API

## Non Goals

- Having an alternative way to provide the static configuration that involves distritbuting the configuration to the routers

## Proposal

### User stories

- As a cluster admin, I want to deploy the router to the nodes of the cluster with enough configuration to allow it to provide the basic connectivity.

- As a cluster admin, I want to use Kubernetes CRDs to provision additional networks to reach / expose different classes of services or implement multitenancy.

### Design Details

The general idea is to run the same set of containers we have in pods today, but on the host. This includes:

- running the router pod, in charge of providing the network control plane with FRR and the data plane with veths and the underlay interface
- running the controller pod, in charge of reading the static configuration and receiving the additional configuration when the kubernete api is available

#### Handling the lifecycle of the components

The router and the contrller need to run as systemd units. This allows us to run them before the kubelet and the CNI start.
Once the machinery is implemented, alternative ways to run them can be added for those environments where systemd is not enabled.

#### Running on the host

Podman provides a way to maintain the current pod structure while running the pods as systemd units. This allow us to keep reasoning of pods and
to have a good parallel between the "Kubernetes mode" and the "Host mode".

Because of this, the initial implementation will be based on Podman. As per systemd above, when we have a way of running the pods on the host, it should be
relatively easy to adapt to other container runtimes.

#### Interactions

There are some interactions between the controller process and the router (and the surrounding system) that need to be adapted to the new scenario:

##### Retrieving the router's network namespace

We are using the container runtime api to retrieve the network namespace of the pod.
When running on the host, the target namespace can be reitrieved by telling podman to fill a pid file, and by using the pid to retrieve the network namespace of the process.

##### Sending a signal to the reloader container to reload the FRR configuration

Currently the contrller retrieves the target pod's IP using the kubernetes API. When running on the host, a unix socket can be shared between the containers.

##### Restarting the router pod

The controller pod currently deletes the router pod to restart the underlay creation machinery. When running on the host, the controller pod can interact either with Podman or with Systemd to restart the pod.

#### Consuming the static configuration

The static configuration must be provided to the OpenPERouter as files in a known location. The configuration is composed by:

- A static configuration with the same structure of the CRDs (ie underlays, vnis) to be used for bringing up the basic connectivity
- An extra configuration to fill the requirements of running on the host. At the time of writing, the only extra configuration required seem to be a node index, to be associated manually (or by automation) to be different per each node.

#### Consuming the Kubernetes API

Running the controller on the host while it's still be able to access the kubernetes API is a challenge. One possible way to solve this is to have a proxy pod running on the cluster and translating its credentials to the controller running on the host.

#### Merging the two configurations

The static configuration must be treated exactly as configuration coming from the CRs. In this sense it must be validated, and the static configuration must be merged with the configuration read by CRs.

This would allow a scenario where the UNDERLAY and a VNI is provided via static configuration, and extra VNIs are provided as day 2 configuration.

### Challenges

#### Troubleshooting and triaging

When the components are running on the host, it's harder to inspect logs (as it requires ssh-ing to the node and checking the logs of the systemd unit / podman containers).

#### Handling the lifecycle

Updating the containers is not as straightforward as changing the manifests in the cluster using helm or the operator.

#### Deploying on the hosts

This will require automation for installing the systemd units, for copying the static configuration and also for generating a different configuration per node (to provide a different node index).

#### Changing the underlay configuration

Given that the base configuration is static, changing it comes with its own challenges

### Possible alternatives

Instead of consuming the kubernetes API, the controller could expose a kubernetes independent configuration (like grpc).

This could make the controller (and the router in general) independent from kubernets, and in this scenario a pod running on the cluster would translate the 
configuration from CRs to grpc. However, this would bring extra complexity by splitting the controller in two pieces and relaying on the grpc interface.


## Implementation

### Host mode in the controller

The controller must expose a parameter that enables the differences in the interaction with the router as mentioned above.

When running in host mode it will:

- Load and process the static configuration
- Wait until the API server is available
- Start the reconciler logic as it does today

### Host proxy pod

When running in "host mode" we won't deploy the router and the controller pods via the manifests (because they are running already on the host), but a new type of pod ("host proxy") is needed to provide the credentials to the components running on the host.

A new kustomize overlay / helm parameter will be provided to support this deployment mode.

### Host mode in the reloader

The reloader must be able to be notified via a unix socket instead of TCP.

### Systemd unit generator

A convenience systemd unit generator must be provided, so the `.service` files can be provided to users.

### The podman pods

The structure of the podman pods will mimic what we have today with kubernetes pods. The differences are:

- a shared volume for the frr reloader socket
- the router exposing its pid as a pid file
- the pidfile mounted to the controller pod

## Observability

- The status CRDs that are discussed in the [status enhancement](https://github.com/openperouter/openperouter/blob/main/enhancements/status-crd.md)
must keep working
- Prometheus metrics added to the pods must be available from the Kubernetes cluster.
- The pod responsible of providing the kubernetes credentials to the process running on the host will mirror the statically provided configuration to
CRs with an appropriate name. This will make it easier to understand the whole configuration of the cluster.

## Testing

When running in host mode, the same test suite we have today must work and the tests must pass.

### Changing the test suite

The test suite must be changed to accomodate the host mode. Specifically:

- knowing when the router pod is restarted
- running commands in the router pod

### Testing the static configuration

A new lane and possibly a new (small) set of tests must validate that the OpenPERouter works when providing a static configuration that is then mixed with the one coming from CRs.

