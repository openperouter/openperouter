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
   | Hostmode | `make silentdeploy-hostmode` |
   | Grout | `make grout-silentdeploy` |

2. Run the selected target. For example, to deploy Grout:

   ```shell
   make grout-silentdeploy
   ```

All silent deployment targets build the appropriate image, deploy it, and print
only errors. Do not follow the log continuously. Check the live log once every
10 minutes to confirm the deployment is still progressing:

```shell
tail -n 50 .silentdeploy.log
```

Wait for each target to complete before proceeding. Report any errors immediately.
