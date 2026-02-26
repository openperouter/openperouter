#!/usr/bin/env python3
"""Generate scale test metric plots from the JSON report.

Reads the scale-test-report.json produced by VNI scale tests and generates
4 PNG plots showing CPU and memory usage vs VNI count for router and
controller pods.

Usage:
    python3 hack/plot_scale_metrics.py \
        --input /tmp/kind_logs/scale-test-report.json \
        --output-dir /tmp/kind_logs/plots
"""

import argparse
import json
import os
import sys

try:
    import matplotlib
    matplotlib.use("Agg")
    import matplotlib.pyplot as plt
except ImportError:
    print("ERROR: matplotlib is required. Install with: pip install matplotlib", file=sys.stderr)
    sys.exit(1)

# Series configuration: test_type -> (label, color, linestyle)
SERIES_CONFIG = {
    "l2vni_only_linux-bridge": ("L2VNI linux-bridge", "#1f77b4", "solid"),
    "l2vni_only_ovs-bridge": ("L2VNI ovs-bridge", "#1f77b4", "dashed"),
    "l3vni_with_l2vni_linux-bridge": ("L3VNI+L2VNI linux-bridge", "#d62728", "solid"),
    "l3vni_with_l2vni_ovs-bridge": ("L3VNI+L2VNI ovs-bridge", "#d62728", "dashed"),
}

# Plot definitions: (filename, title, component, metric_key, y_label)
PLOT_DEFS = [
    ("router-cpu-vs-vni.png", "Router CPU vs VNI Count", "router", "total_cpu_millicores", "CPU (millicores)"),
    ("router-memory-vs-vni.png", "Router Memory vs VNI Count", "router", "total_memory_mb", "Memory (MB)"),
    ("controller-cpu-vs-vni.png", "Controller CPU vs VNI Count", "controller", "total_cpu_millicores", "CPU (millicores)"),
    ("controller-memory-vs-vni.png", "Controller Memory vs VNI Count", "controller", "total_memory_mb", "Memory (MB)"),
]


def load_report(path):
    """Load and return the JSON report."""
    if not os.path.isfile(path):
        print(f"ERROR: Report file not found: {path}", file=sys.stderr)
        sys.exit(1)
    with open(path) as f:
        return json.load(f)


def extract_series(data_points, component, metric_key):
    """Extract per-test-type series data from data points.

    Returns dict: test_type -> sorted list of (vni_count, metric_value).
    """
    series = {}
    for dp in data_points:
        test_type = dp["test_type"]
        vni_count = dp["vni_count"]
        scaled = dp.get("scaled", {})
        comp = scaled.get(component, {})
        value = comp.get(metric_key)
        if value is None:
            continue
        series.setdefault(test_type, []).append((vni_count, value))

    # Sort each series by VNI count
    for test_type in series:
        series[test_type].sort(key=lambda x: x[0])

    return series


def generate_plot(series, title, y_label, output_path, metrics_available):
    """Generate and save a single plot."""
    fig, ax = plt.subplots(figsize=(10, 6))

    for test_type, config in SERIES_CONFIG.items():
        if test_type not in series:
            continue
        label, color, linestyle = config
        points = series[test_type]
        x = [p[0] for p in points]
        y = [p[1] for p in points]
        ax.plot(x, y, label=label, color=color, linestyle=linestyle,
                marker="o", markersize=4, linewidth=1.5)

    ax.set_xlabel("VNI Count")
    ax.set_ylabel(y_label)
    ax.set_title(title)
    ax.legend(loc="upper left")
    ax.grid(True, alpha=0.3)

    if not metrics_available:
        ax.text(0.5, 0.5, "WARNING: metrics-server was unavailable\nValues may be zeros",
                transform=ax.transAxes, fontsize=14, color="red", alpha=0.4,
                ha="center", va="center", fontweight="bold")

    fig.tight_layout()
    fig.savefig(output_path, dpi=150)
    plt.close(fig)
    print(f"  Saved: {output_path}")


def main():
    parser = argparse.ArgumentParser(description="Generate scale test metric plots")
    parser.add_argument("--input", default="/tmp/kind_logs/scale-test-report.json",
                        help="Path to scale-test-report.json")
    parser.add_argument("--output-dir", default="/tmp/kind_logs/plots",
                        help="Directory for output PNG files")
    args = parser.parse_args()

    report = load_report(args.input)

    data_points = report.get("data_points", [])
    if not data_points:
        print("WARNING: No data points found in report, skipping plot generation", file=sys.stderr)
        sys.exit(0)

    metrics_available = report.get("environment", {}).get("metrics_server_available", True)

    os.makedirs(args.output_dir, exist_ok=True)

    generated = 0
    for filename, title, component, metric_key, y_label in PLOT_DEFS:
        series = extract_series(data_points, component, metric_key)
        if not series:
            print(f"  WARNING: No data for {title}, skipping")
            continue
        output_path = os.path.join(args.output_dir, filename)
        generate_plot(series, title, y_label, output_path, metrics_available)
        generated += 1

    print(f"Generated {generated} plot(s) in {args.output_dir}")


if __name__ == "__main__":
    main()
