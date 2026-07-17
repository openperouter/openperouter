---
name: deploy
description: Build and deploy openperouter to a kind cluster, optionally in hostmode
trigger: deploy, deploy hostmode, ship it, deploy the project
---

# Deploy openperouter

Run these steps in order:

1. Build the docker image:
   ```
   make docker-build
   ```

2. Deploy to the kind cluster. Choose based on the user's request:

   - **Standard mode** (default): run `make deploy`
   - **Hostmode**: if the user asks for hostmode, run `make deploy-hostmode` instead

Wait for each step to complete before proceeding to the next. Report any errors immediately.
