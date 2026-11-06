---
name: deploy
description: Build and deploy openperouter to a kind cluster in Kubernetes, hostmode, or Grout mode
trigger: deploy, deploy hostmode, ship it, deploy the project
---

# Deploy openperouter

Run these steps in order:

1. Choose the build and deployment targets based on the user's request:

   | Mode | Build and deploy |
   | --- | --- | --- |
   | Kubernetes (default) | `make silentdeploy` |
   | Hostmode | `make docker-build`, then `make deploy-hostmode` |
   | Grout | `make grout-docker-build`, then `make grout-deploy` |

2. Run the selected target(s). For example, to deploy Grout:

   ```shell
   make grout-docker-build && make grout-deploy
   ```

For the default Kubernetes deployment, use `make silentdeploy`; it builds the
router image, deploys it, and prints only errors. Do not follow the log
continuously. Check the live log once every 10 minutes to confirm the deployment
is still progressing:

```shell
tail -n 50 .silentdeploy.log
```

Wait for each step to complete before proceeding to the next. Report any errors
immediately.
