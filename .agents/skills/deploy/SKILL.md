---
name: deploy
description: Build and deploy openperouter to a kind cluster in Kubernetes, hostmode, or Grout mode
trigger: deploy, deploy hostmode, ship it, deploy the project
---

# Deploy openperouter

Run these steps in order:

1. Choose the build and deployment targets based on the user's request:

   | Mode | Build image | Deploy |
   | --- | --- | --- |
   | Kubernetes (default) | `make docker-build` | `make deploy` |
   | Hostmode | `make docker-build` | `make deploy-hostmode` |
   | Grout | `make grout-docker-build` | `make grout-deploy` |

2. Run the build target, followed by the deployment target. For example, to
   deploy Grout:

   ```shell
   make grout-docker-build
   make grout-deploy
   ```

Wait for each step to complete before proceeding to the next. Report any errors immediately.
