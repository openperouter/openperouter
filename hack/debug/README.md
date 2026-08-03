# DNM debugging helpers

Tooling to investigate the zebra `SIGABRT` in `dplane_thread_loop` that shows
up in the `ListenRange` e2e specs. None of this is meant to be merged as is.

## What the branch changes

* `Dockerfile` grows a `FRR_VARIANT` build argument:
  * `release` (default when building by hand) keeps the stock, stripped FRR
    from `quay.io/frrouting/frr:10.6.0`.
  * `debug` rebuilds FRR from source with `-g3 -Og -fno-omit-frame-pointer`
    and `--enable-libunwind`, and adds `gdb`. libunwind is what makes the FRR
    crash handler print frames at all: on musl, without it, the log only shows
    `Received signal 6 ... aborting...`, which is exactly what CI collected so
    far.
* CI builds the router image with `FRR_VARIANT=debug` by default on this
  branch.
* The log, core and artifact steps run on `always()` instead of `failure()`,
  because watchfrr sometimes restarts the crashed daemon and the suite goes on
  to pass, hiding the crash entirely.
* `hack/debug/symbolize-cores.sh` turns every collected core into a backtrace.
* `hack/debug/detect-frr-crashes.sh` fails the job when a daemon crashed.
* The e2e lanes default to `--focus='ListenRange' --repeat=20`, which gives 20
  attempts at the race per run instead of one.

## Building the debug image locally

```bash
make docker-build FRR_VARIANT=debug
```

Verify it is the debug build:

```bash
docker run --rm --entrypoint sh quay.io/openperouter/router:main -c \
  '/usr/lib/frr/zebra --version | head -1; ldd /usr/lib/frr/zebra | grep unwind'
```

The stock image reports `10.6.0_git` and links no libunwind, the debug one
reports `10.6.0` and links `libunwind.so.8`.

## Symbolizing cores by hand

Core dumps are written by the `kernel.core_pattern` pipe installed in
`clab/setup.sh` when `COREDUMP=true`, and land in
`${KIND_EXPORT_LOGS}/core_dumps`.

```bash
./hack/debug/symbolize-cores.sh /tmp/kind_logs quay.io/openperouter/router:main
```

Backtraces are written to `${KIND_EXPORT_LOGS}/core_backtraces/*.bt` and echoed
to stdout. If no frame resolves to a symbol, the core came from a stock FRR
build and the image has to be rebuilt with `FRR_VARIANT=debug`.

## Running a soak by hand

```bash
make e2etests GINKGO_ARGS="--focus='ListenRange' --repeat=20"
```

`--repeat` stops at the first failure, so the run ends as soon as the crash is
reproduced.

## Triggering a parameterized CI run

The CI workflow accepts `workflow_dispatch` inputs:

| input | default | meaning |
| --- | --- | --- |
| `frr_variant` | `debug` | FRR runtime baked into the router image |
| `ginkgo_args` | `--focus='ListenRange' --repeat=20` | extra ginkgo arguments |
| `deployments` | `["manifests","helm"]` | JSON array of e2e lanes |

```bash
gh workflow run CI --ref rr-dnm-nodebugkernel \
  -f ginkgo_args="--focus='ListenRange' --repeat=40" \
  -f deployments='["manifests"]'
```

## Catching the corruption instead of the assert

The abort itself is a symptom: `_zlog_assert_failed()` re-firing while
formatting an already corrupted `struct ipaddr`. To find where the bad
`ipa_type` comes from, run zebra under valgrind. The debug image ships it, and
FRR's daemons file has a wrap hook for exactly this:

```
zebra_wrap="/usr/bin/valgrind --tool=memcheck --track-origins=yes --log-file=/var/run/frr/valgrind-zebra.%p.log"
```

Set it in the daemons ConfigMap of the lane under test, `config/pods/frr-cm.yaml`
for `manifests` or `charts/openperouter/templates/router.yaml` for `helm`, then
collect `/var/run/frr/valgrind-zebra.*.log`. Expect the suite to be much slower.

AddressSanitizer is not available: Alpine ships no `libasan` for musl, so an
ASAN build would need the router image moved to a glibc base.

## Notes

* `debug zebra kernel` must stay in `internal/frr/templates/frr.tmpl`. Dropping
  it makes the abort go away, but it also hides the corruption: the crash has
  to keep firing for a core to be worth dissecting.

* `concurrency.cancel-in-progress` is left at the upstream default. Turning it
  off to protect a soak run backfires: every later push then queues behind the
  running soak instead of superseding it, and the branch stalls for an hour.
  Just avoid pushing while a soak is running, or launch it with
  `workflow_dispatch` from a ref nobody is pushing to.
* The `manifests` lane runs zebra without `-K 60`, the `helm` and `operator`
  lanes with it (`config/pods/frr-cm.yaml` vs
  `charts/openperouter/templates/router.yaml`). Both lanes have reproduced the
  crash, but it is worth keeping in mind when comparing them.
