#!/usr/bin/env bash
set -euo pipefail

write_output() {
    local output_file="$1"
    shift
    "$@" 2>&1 | tee -a "$benchmark_output/$output_file"
}

write_metadata() {
    local count="$1"
    local benchtime="$2"

    write_output metadata.txt printf 'base=%s\ncurrent=%s (including working tree)\ncount=%s\nbenchtime=%s\n' \
        "$benchmark_baseline_commit" "$(git rev-parse HEAD)" "$count" "$benchtime" > /dev/null
    write_output metadata.txt go version > /dev/null
    write_output metadata.txt uname -sr > /dev/null
}

benchmark_baseline() {
    # Reuse the binary so alternating samples do not rebuild or reset the baseline copy.
    if [[ ! -f "$benchmark_tmp/baseline.test" ]]; then
        local baseline_checkout="$benchmark_tmp/base/$benchmark_package"
        mkdir "$benchmark_tmp/base"
        git -C "$benchmark_root" archive "$benchmark_baseline_commit" | tar -x -C "$benchmark_tmp/base"

        # Replace the archived baseline's benchmark harness and fixtures with the current ones
        # so both production revisions run identical workloads. Remove old files first to avoid
        # retaining stale helpers or fixtures. These removals affect only the temporary archive,
        # not the working tree. This also supports baselines that predate the benchmark suite.
        rm -f "$baseline_checkout"/benchmark*_test.go \
            "$baseline_checkout/reconcile_benchmark_test.go"
        rm -rf "$baseline_checkout/testdata/benchmarks"

        cp "$benchmark_root/$benchmark_package/reconcile_benchmark_test.go" "$baseline_checkout/"
        mkdir -p "$baseline_checkout/testdata"
        cp -R "$benchmark_root/$benchmark_package/testdata/benchmarks" "$baseline_checkout/testdata/"

        # Compile the baseline production code with the current benchmark harness; -c does not run tests.
        go -C "$benchmark_tmp/base" test -c -o "$benchmark_tmp/baseline.test" "./$benchmark_package"
    fi

    local output_file="$1"
    local benchtime="$2"
    local -a benchmark_args=(
        -test.run '^$' -test.bench '^BenchmarkReconcile$' -test.benchmem -test.cpu=2 -test.count=1
    )

    write_output "$output_file" sudo -n "$benchmark_tmp/baseline.test" "${benchmark_args[@]}" \
        "-test.benchtime=$benchtime"
}

benchmark_current() {
    if [[ ! -f "$benchmark_tmp/current.test" ]]; then
        go -C "$benchmark_root" test -c -o "$benchmark_tmp/current.test" "./$benchmark_package"
    fi

    local output_file="$1"
    local benchtime="$2"
    local -a benchmark_args=(
        -test.run '^$' -test.bench '^BenchmarkReconcile$' -test.benchmem -test.cpu=2 -test.count=1
    )

    write_output "$output_file" sudo -n "$benchmark_tmp/current.test" "${benchmark_args[@]}" \
        "-test.benchtime=$benchtime"
}

collect_samples() {
    local count="$1"
    local benchtime="$2"
    local sample revision
    local -a revisions

    # baseline.test runs the archived baseline code; current.test runs the working tree code.
    # Alternate their order to reduce timing differences caused by changing worker load.
    for (( sample=1; sample<=count; sample++ )); do
        revisions=(baseline current)
        if (( sample % 2 == 0 )); then revisions=(current baseline); fi

        for revision in "${revisions[@]}"; do
            echo "Benchmark sample $sample/$count: $revision"
            "benchmark_$revision" "$revision.txt" "$benchtime"
        done
    done
}

benchmark_baseline_commit=${1:-HEAD}

benchmark_root=$(git rev-parse --show-toplevel)
cd "$benchmark_root"
benchmark_baseline_commit=$(git rev-parse --verify "$benchmark_baseline_commit^{commit}")

benchmark_tmp=$(mktemp -d "$benchmark_root/bin/benchmark-check.XXXXXX")
trap 'rm -rf "$benchmark_tmp"' EXIT

benchmark_package=internal/controller/routerconfiguration
sudo -n true


benchmark_output="$benchmark_root/bin/benchmark-results"
rm -rf "$benchmark_output"
mkdir -p "$benchmark_output"

write_metadata 10 20x

# Run both binaries from the current package directory so they load the same fixture files.
cd "$benchmark_root/$benchmark_package"

# Warm up the baseline and current binaries before collecting comparison samples.
benchmark_baseline baseline-warmup.txt 1x > /dev/null
benchmark_current current-warmup.txt 1x > /dev/null

collect_samples 10 20x

write_output comparison.txt "$benchmark_root/bin/benchmarkcheck" -max-regression 10 \
    "$benchmark_output/baseline.txt" "$benchmark_output/current.txt"
