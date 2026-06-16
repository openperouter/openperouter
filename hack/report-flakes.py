#!/usr/bin/env python3

import json
import os
import subprocess
import sys
from datetime import datetime, timezone
from pathlib import Path

GROUPED_LANES = {"manifests", "helm", "operator"}

def env_required(name):
    val = os.environ.get(name)
    if not val:
        print(f"Error: {name} must be set", file=sys.stderr)
        sys.exit(1)
    return val

def run_gh(*args):
    result = subprocess.run(
        ["gh"] + list(args),
        capture_output=True, text=True,
    )
    if result.returncode != 0:
        cmd = " ".join(["gh"] + list(args))
        raise RuntimeError(f"{cmd} failed:\n{result.stderr.strip()}")
    return result.stdout.strip()

def collect_failures(reports_dir):
    failures = {}
    report_files_seen = 0
    for artifact_dir in sorted(reports_dir.glob("e2e-reports-*/")):
        deployment = artifact_dir.name.removeprefix("e2e-reports-")

        for report_path in sorted(artifact_dir.glob("e2e-report*.json")):
            report_files_seen += 1
            try:
                data = json.loads(report_path.read_text())
            except (json.JSONDecodeError, OSError) as e:
                print(f"Warning: {report_path} is invalid JSON, skipping: {e}", file=sys.stderr)
                continue

            if isinstance(data, dict):
                suites = [data]
            elif isinstance(data, list):
                suites = data
            else:
                print(f"Warning: {report_path} has unexpected JSON shape, skipping", file=sys.stderr)
                continue

            for suite in suites:
                if not isinstance(suite, dict):
                    continue
                for spec in suite.get("SpecReports", []):
                    if not isinstance(spec, dict):
                        continue
                    if spec.get("State") != "failed":
                        continue

                    hierarchy = spec.get("ContainerHierarchyTexts") or []
                    leaf = spec.get("LeafNodeText", "")
                    parts = hierarchy + [leaf]
                    full_path = " ".join(parts).strip()
                    if not full_path:
                        continue

                    if len(hierarchy) > 0:
                        short_title = f"{hierarchy[-1]} / {leaf}"
                    else:
                        short_title = leaf

                    if deployment in GROUPED_LANES:
                        flake_id = f"flake-id: {full_path}"
                    else:
                        flake_id = f"flake-id: {deployment} {full_path}"

                    if flake_id not in failures:
                        failures[flake_id] = {
                            "full_path": full_path,
                            "short_title": short_title,
                            "deployments": set(),
                        }
                    failures[flake_id]["deployments"].add(deployment)

    if report_files_seen == 0:
        print(f"Warning: no e2e-report*.json files found under {reports_dir}", file=sys.stderr)

    return failures

def fetch_existing_issues(repo):
    output = run_gh(
        "api", "--paginate",
        "--method", "GET",
        f"repos/{repo}/issues",
        "-f", "labels=kind/flake",
        "-f", "state=open",
        "-f", "per_page=100",
    )
    if not output:
        return []
    issues = []
    for line in output.splitlines():
        line = line.strip()
        if not line:
            continue
        page = json.loads(line)
        if not isinstance(page, list):
            raise RuntimeError(f"unexpected GitHub issues API response: {line[:200]}")
        issues.extend(issue for issue in page if "pull_request" not in issue)
    return issues

def find_existing_issue(issues, flake_id):
    for issue in issues:
        body = (issue.get("body") or "").replace("\r", "")
        if flake_id in body.split("\n"):
            return issue["number"]
    return None

def comment_on_issue(repo, issue_number, deployments, full_path, run_url, date):
    body = (
        f"Nightly flake recurrence ({date})\n\n"
        f"| Field | Value |\n"
        f"|-------|-------|\n"
        f"| Run | {run_url} |\n"
        f"| Deployment(s) | {deployments} |\n\n"
        f"**Test path:**\n"
        f"```\n{full_path}\n```"
    )
    run_gh("issue", "comment", str(issue_number), "--repo", repo, "--body", body)
    print(f"Commented on issue #{issue_number} for [{deployments}]: {full_path}")

def create_issue(repo, title, flake_id, full_path, deployments, run_url, date):
    body = (
        f"Flaky test detected by nightly CI.\n\n"
        f"**Test path:**\n"
        f"```\n{full_path}\n```\n\n"
        f"**Deployment(s):** {deployments}\n"
        f"**First seen:** {date}\n"
        f"**Run:** {run_url}\n\n"
        f"---\n"
        f"{flake_id}"
    )
    run_gh("issue", "create", "--repo", repo, "--title", title, "--label", "kind/flake", "--body", body)
    print(f"Created issue for [{deployments}]: {full_path}")

def main():
    if len(sys.argv) != 2:
        print("Usage: report-flakes.py <reports-dir>", file=sys.stderr)
        sys.exit(1)

    reports_dir = Path(sys.argv[1])
    if not reports_dir.is_dir():
        print(f"Error: reports dir does not exist: {reports_dir}", file=sys.stderr)
        sys.exit(1)
    repo = env_required("GITHUB_REPOSITORY")
    server_url = env_required("GITHUB_SERVER_URL")
    run_id = env_required("GITHUB_RUN_ID")
    run_url = f"{server_url}/{repo}/actions/runs/{run_id}"
    date = datetime.now(timezone.utc).strftime("%Y-%m-%d")

    failures = collect_failures(reports_dir)
    if not failures:
        print("No test failures found.")
        return

    existing_issues = fetch_existing_issues(repo)

    for flake_id, info in sorted(failures.items()):
        deployments = ", ".join(sorted(info["deployments"]))
        title = f"Flake: {info['short_title']}"
        if len(title) > 80:
            title = title[:77] + "..."

        issue_number = find_existing_issue(existing_issues, flake_id)
        if issue_number:
            comment_on_issue(repo, issue_number, deployments, info["full_path"], run_url, date)
        else:
            create_issue(repo, title, flake_id, info["full_path"], deployments, run_url, date)

if __name__ == "__main__":
    main()
