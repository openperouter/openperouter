#!/usr/bin/env python3
"""Parse scale test JSON report and extract graph-ready data points.

Reads the Ginkgo JSON report produced by `make scale-tests` and prints
reconcile time and memory data grouped by scenario, suitable for plotting:
  - X axis: number of VNIs
  - Y axis: reconcile time (seconds) or memory (MB)

Usage:
    python3 hack/parse-scale-report.py [path/to/scale-report.json]

Default path: /tmp/kind_logs/e2etests_suite_scale-report.json
"""

import argparse
import json
import re
import sys

DEFAULT_REPORT = "/tmp/kind_logs/e2etests_suite_scale-report.json"

RECONCILE_RE = re.compile(
    r"^VNI Scale Tests (?P<scenario>.+?) VNI Scale Measurements (?P<vnis>\d+) (?:VNIs|pairs) reconcile$"
)
MEMORY_RE = re.compile(
    r"^VNI Scale Tests (?P<scenario>.+?) VNI Scale Measurements (?P<vnis>\d+) (?:VNIs|pairs) (?P<component>router|controller) memory$"
)


def load_measurements(path: str) -> list[dict]:
    with open(path) as f:
        suites = json.load(f)

    measurements = []
    for suite in suites:
        for spec in suite.get("SpecReports", []):
            for entry in spec.get("ReportEntries", []):
                value = entry.get("Value", {})
                as_json = value.get("AsJSON")
                if not as_json:
                    continue
                experiment = json.loads(as_json)
                measurements.extend(experiment.get("Measurements", []))
    return measurements


def extract_data(measurements: list[dict]):
    reconcile: dict[str, list[tuple[int, float]]] = {}
    memory: dict[str, list[tuple[int, float]]] = {}

    for m in measurements:
        name = m["Name"]

        match = RECONCILE_RE.match(name)
        if match:
            scenario = match.group("scenario")
            vnis = int(match.group("vnis"))
            duration_ns = m["Durations"][0]
            duration_s = duration_ns / 1e9
            reconcile.setdefault(scenario, []).append((vnis, duration_s))
            continue

        match = MEMORY_RE.match(name)
        if match:
            scenario = match.group("scenario")
            component = match.group("component")
            vnis = int(match.group("vnis"))
            value_mb = m["Values"][0]
            key = f"{scenario} - {component}"
            memory.setdefault(key, []).append((vnis, value_mb))

    for points in reconcile.values():
        points.sort()
    for points in memory.values():
        points.sort()

    return reconcile, memory


def print_table(title: str, headers: tuple[str, str], rows: list[tuple[int, float]]):
    print(f"\n{title}")
    print("-" * len(title))
    col1, col2 = headers
    print(f"  {col1:>8s}  {col2:>12s}")
    for vnis, value in rows:
        print(f"  {vnis:>8d}  {value:>12.2f}")


def main():
    parser = argparse.ArgumentParser(
        description="Parse scale test JSON report and extract graph-ready data points.\n\n"
        "Reads the Ginkgo JSON report produced by `make scale-tests` and prints\n"
        "reconcile time and memory tables grouped by scenario, plus CSV output\n"
        "suitable for plotting:\n"
        "  - VNIs (X) vs reconcile time in seconds (Y)\n"
        "  - VNIs (X) vs memory in MB (Y)",
        formatter_class=argparse.RawDescriptionHelpFormatter,
    )
    parser.add_argument(
        "report",
        nargs="?",
        default=DEFAULT_REPORT,
        help=f"path to the scale-report.json file (default: {DEFAULT_REPORT})",
    )
    args = parser.parse_args()
    path = args.report

    try:
        measurements = load_measurements(path)
    except FileNotFoundError:
        print(f"error: report not found: {path}", file=sys.stderr)
        sys.exit(1)

    if not measurements:
        print(f"error: no measurements found in {path}", file=sys.stderr)
        sys.exit(1)

    reconcile, memory = extract_data(measurements)

    print("=" * 60)
    print("RECONCILE TIME")
    print("=" * 60)
    for scenario, rows in sorted(reconcile.items()):
        print_table(scenario, ("VNIs", "Time (s)"), rows)

    print()
    print("=" * 60)
    print("MEMORY USAGE")
    print("=" * 60)
    for key, rows in sorted(memory.items()):
        print_table(key, ("VNIs", "Memory (MB)"), rows)

    print()
    print("=" * 60)
    print("CSV (for plotting)")
    print("=" * 60)

    print("\n# Reconcile time")
    scenarios = sorted(reconcile.keys())
    print("VNIs," + ",".join(f"{s} (s)" for s in scenarios))
    all_vnis = sorted({v for rows in reconcile.values() for v, _ in rows})
    for vni in all_vnis:
        vals = []
        for s in scenarios:
            lookup = {v: t for v, t in reconcile[s]}
            vals.append(f"{lookup[vni]:.2f}" if vni in lookup else "")
        print(f"{vni},{','.join(vals)}")

    print("\n# Memory")
    mem_keys = sorted(memory.keys())
    print("VNIs," + ",".join(f"{k} (MB)" for k in mem_keys))
    all_vnis = sorted({v for rows in memory.values() for v, _ in rows})
    for vni in all_vnis:
        vals = []
        for k in mem_keys:
            lookup = {v: m for v, m in memory[k]}
            vals.append(f"{lookup[vni]:.2f}" if vni in lookup else "")
        print(f"{vni},{','.join(vals)}")


if __name__ == "__main__":
    main()
