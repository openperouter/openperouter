---
weight: 50
title: "Examples"
description: "Integration examples with BGP-speaking components"
icon: "article"
date: "2025-06-15T15:03:22+02:00"
lastmod: "2025-06-15T15:03:22+02:00"
toc: true
---

This section provides practical examples of integrating OpenPERouter with various BGP-speaking components commonly used in Kubernetes environments.

## Overview

OpenPERouter behaves exactly like a physical Provider Edge (PE) router, enabling seamless integration with any BGP-speaking component. This router-like behavior ensures that integration is straightforward and follows standard BGP peering practices.

## Prerequisites

All examples in this section assume you have:

- OpenPERouter installed and configured (see [Installation]({{< ref "installation" >}}))
- A container lab based development environment with the relevant VPN endpoints configured
- Basic understanding of BGP and the VPN of your choice (currently EVPN only) concepts

## Development Environment

The examples require a container lab based environment that provides:

- Leaf switches with BGP peering capabilities
- A Kubernetes cluster for testing OpenPERouter integration

## Available Examples

### EVPN Examples

For comprehensive EVPN integration examples, including detailed configurations and step-by-step guides, see the dedicated EVPN examples section:

[View EVPN Examples →]({{< ref "evpnexamples" >}})

This section contains practical examples of integrating OpenPERouter with various BGP-speaking components in EVPN environments.
