#!/usr/bin/env bash
set -euo pipefail

REPORTS_DIR="${1:?Usage: report-flakes.sh <reports-dir>}"
REPO="${GITHUB_REPOSITORY:?GITHUB_REPOSITORY must be set}"
RUN_URL="${GITHUB_SERVER_URL:?GITHUB_SERVER_URL must be set}/${REPO}/actions/runs/${GITHUB_RUN_ID:?GITHUB_RUN_ID must be set}"
DATE="$(date -u +%Y-%m-%d)"

GROUPED_LANES="manifests helm operator"

FAILURES="$(mktemp)"
EXISTING_ISSUES="$(mktemp)"
trap 'rm -f "$FAILURES" "$EXISTING_ISSUES"' EXIT

is_grouped_lane() {
    local lane="$1"
    for g in $GROUPED_LANES; do
        [ "$g" = "$lane" ] && return 0
    done
    return 1
}

collect_failures() {
    for artifact_dir in "$REPORTS_DIR"/e2e-reports-*/; do
        [ -d "$artifact_dir" ] || continue

        deployment="${artifact_dir%/}"
        deployment="${deployment##*e2e-reports-}"

        for report in "$artifact_dir"/e2e-report*.json; do
            [ -f "$report" ] || continue
            if ! jq empty "$report" 2>/dev/null; then
                echo "Warning: $report is invalid JSON, skipping." >&2
                continue
            fi

            jq -r '
                .[] |
                .SpecReports[]? |
                select(.State == "failed") |
                {
                    full_path: (((.ContainerHierarchyTexts // []) + [.LeafNodeText]) | join(" ")),
                    short_title: (
                        if ((.ContainerHierarchyTexts // []) | length) > 0 then
                            (.ContainerHierarchyTexts[-1] + " / " + .LeafNodeText)
                        else
                            .LeafNodeText
                        end
                    )
                } |
                select(.full_path != "") |
                [.full_path, .short_title] |
                @tsv
            ' "$report" | while IFS=$'\t' read -r full_path short_title; do
                if is_grouped_lane "$deployment"; then
                    flake_id="flake-id: ${full_path}"
                else
                    flake_id="flake-id: ${deployment} ${full_path}"
                fi
                printf '%s\t%s\t%s\t%s\n' "$flake_id" "$deployment" "$full_path" "$short_title" >> "$FAILURES"
            done
        done
    done
}

fetch_existing_issues() {
    gh issue list \
        --repo "$REPO" \
        --label "kind/flake" \
        --state open \
        --json number,body \
        --limit 200 > "$EXISTING_ISSUES" 2>/dev/null || echo '[]' > "$EXISTING_ISSUES"
}

find_existing_issue() {
    local flake_id="$1"
    jq -r --arg fid "$flake_id" '
        [ .[] | select((.body // "") | gsub("\r";"") | split("\n") | any(. == $fid)) ] | .[0].number // empty
    ' "$EXISTING_ISSUES"
}

comment_on_issue() {
    local issue_number="$1" deployments="$2" full_path="$3"
    gh issue comment "$issue_number" \
        --repo "$REPO" \
        --body "$(cat <<COMMENT
Nightly flake recurrence (${DATE})

| Field | Value |
|-------|-------|
| Run | ${RUN_URL} |
| Deployment(s) | ${deployments} |
COMMENT
)"
    echo "Commented on issue #${issue_number} for [${deployments}]: ${full_path}"
}

create_issue() {
    local title="$1" flake_id="$2" full_path="$3" deployments="$4"
    gh issue create \
        --repo "$REPO" \
        --title "$title" \
        --label "kind/flake" \
        --body "$(cat <<BODY
Flaky test detected by nightly CI.

**Test path:**
\`\`\`
${full_path}
\`\`\`

**Deployment(s):** ${deployments}
**First seen:** ${DATE}
**Run:** ${RUN_URL}

---
${flake_id}
BODY
)"
    echo "Created issue for [${deployments}]: ${full_path}"
}

report_flakes() {
    fetch_existing_issues

    while IFS= read -r flake_id; do
        full_path="$(FID="$flake_id" awk -F'\t' '$1 == ENVIRON["FID"] { print $3; exit }' "$FAILURES")"
        short_title="$(FID="$flake_id" awk -F'\t' '$1 == ENVIRON["FID"] { print $4; exit }' "$FAILURES")"
        deployments="$(FID="$flake_id" awk -F'\t' '$1 == ENVIRON["FID"] { print $2 }' "$FAILURES" | sort -u | paste -sd, | sed 's/,/, /g')"

        title="Flake: ${short_title}"
        if [ "${#title}" -gt 80 ]; then
            title="${title:0:77}..."
        fi

        existing="$(find_existing_issue "$flake_id")"

        if [ -n "$existing" ]; then
            comment_on_issue "$existing" "$deployments" "$full_path"
        else
            create_issue "$title" "$flake_id" "$full_path" "$deployments"
        fi
    done < <(cut -f1 "$FAILURES" | sort -u)
}

collect_failures

if [ ! -s "$FAILURES" ]; then
    echo "No test failures found."
    exit 0
fi

report_flakes
