---
name: e2e-tests
description: Run e2e tests, auto-detecting hostmode from the cluster and supporting test filtering
trigger: run e2e tests, run tests, e2e, run the e2e suite, test e2e
---

# Run e2e tests

## Step 1: Detect if the cluster runs in hostmode (systemd mode)

Use the kubeconfig at `bin/kubeconfig` to check for hostbridge pods:

```
KUBECONFIG=bin/kubeconfig kubectl get daemonset hostbridge -n kube-system --no-headers 2>/dev/null
```

If the command returns a result (exit code 0), the cluster is running in **systemd/hostmode**.
If it fails or returns nothing, the cluster is running in **standard mode**.

Tell the user which mode was detected before proceeding.

## Step 2: Build the test arguments

### TEST_ARGS (passed after `--` to the test binary)

- **Standard mode**: leave `TEST_ARGS` empty.
- **Systemd/hostmode**: set `TEST_ARGS="--systemdmode"`.

### GINKGO_ARGS (Ginkgo runner flags, passed before `--`)

- **Standard mode** (no hostbridge): set `GINKGO_ARGS="--label-filter='!systemdmode'"` to skip systemd-only tests.
- **Systemd/hostmode**: set `GINKGO_ARGS="--skip='Router Host configuration' --skip='North/south traffic' --skip='Alpha: Named netns' --skip='Beta: Named netns'"` to skip tests that don't work in hostmode.

### User-requested test filtering

If the user wants to run only specific tests or skip some, adjust `GINKGO_ARGS`:

- To **focus** on tests matching a pattern: add `--focus="<pattern>"` to GINKGO_ARGS.
- To **skip** tests matching a pattern: add `--skip="<pattern>"` to GINKGO_ARGS.
- To filter by **label**: add `--label-filter="<expression>"` to GINKGO_ARGS.

Merge user-requested filters with the mode-specific defaults above (don't drop the defaults unless the user explicitly asks to override them).

## Step 3: Run the tests

```
make e2etests TEST_ARGS="<test_args>" GINKGO_ARGS="<ginkgo_args>"
```

Run this with a long timeout (the Makefile sets `--timeout=3h` internally). Report progress and any failures to the user.
