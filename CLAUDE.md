# Project: OpenPERouter

## Overview
OpenPERouter is a Kubernetes-native router component that runs on cluster nodes and exposes VPN entry points. For detailed architecture and design information, see website/content.

## Tech Stack
- **Language:** Go
- **BGP Implementation:** FRR (Free Range Routing) - https://frrouting.org/
- **Platform:** Kubernetes

## Development Workflow

### Project Structure

Project layout, build commands, and test instructions are documented in @website/content/docs/contributing/_index.md

### Coding style

Follow the coding guidelines in @CODING_STYLE.md — read it before writing or reviewing code.

### Testing Strategy

**Unit Tests:**
Run `make test` to execute unit tests.

**Linting**
Run `make lint` to lint the code.

**End-to-End Tests:**

- To deploy the dev environment, use the /deploy skill. Make sure you deploy the hostmode if you want to test hostmode
specific behavior. If there is nothing specific, just use deploy
- To run e2e tests, use the /e2e-tests skill.
- When triaging e2e test failures, use the /e2etriage skill.

