#!/bin/bash -e

# Script to automate generation and committing of release notes
# Usage:
#   ./hack/gen_relnotes.sh           # Automates entire release notes workflow (auto-increments version)
#   ./hack/gen_relnotes.sh <version> # Automates workflow with custom version (e.g., 1.2.3)

set -e

if [[ ! -v GITHUB_TOKEN ]]; then
    echo "GITHUB_TOKEN is not set, please set it with a token with read permissions on commits and PRs"
    exit 1
fi

script_dir=$(dirname "$(readlink -f "$0")")
repo_root=$(cd "$script_dir/../.." && pwd)

get_latest_version() {
    grep -m 1 "^## Release v" "$repo_root/RELEASE_NOTES.md" | sed 's/## Release v//'
}

increment_version() {
    local version=$1
    # Split version into parts
    local major=$(echo "$version" | cut -d. -f1)
    local minor=$(echo "$version" | cut -d. -f2)
    local patch=$(echo "$version" | cut -d. -f3)

    # Increment patch version
    patch=$((patch + 1))

    echo "${major}.${minor}.${patch}"
}

if [ $# -eq 0 ] || [ $# -eq 1 ]; then
    # Automated mode
    echo "Running in automated mode..."

    branch=${BRANCH:-main}

    # Find the SHA of the most recent "Prepare the v*" commit
    from=$(git log --all --grep="^Release notes for" --format="%H" | head -1)

    if [ -z "$from" ]; then
        echo "Error: Could not find a previous 'Prepare the v*' commit"
        echo "This might be the first release."
        exit 1
    fi

    to=$(git ls-remote https://github.com/openperouter/openperouter.git refs/heads/main | cut -f1)

    current_version=$(get_latest_version)

    if [ $# -eq 1 ]; then
        new_version=$1
        echo "Found previous release commit: $from"
        echo "Current HEAD: $to"
        echo "Current version: v$current_version"
        echo "Using provided version: v$new_version"
    else
        new_version=$(increment_version "$current_version")
        echo "Found previous release commit: $from"
        echo "Current HEAD: $to"
        echo "Current version: v$current_version"
        echo "Auto-incrementing to version: v$new_version"
    fi
else
    echo "Usage: $0 [version]"
    echo "  No arguments: Automated mode (auto-increments patch version)"
    echo "  1 argument: Automated mode with custom version (e.g., $0 1.2.3)"
    exit 1
fi

release_notes=$(mktemp)

end() {
    rm -f "$release_notes"
}

trap end EXIT SIGINT SIGTERM SIGSTOP

echo "Generating release notes..."
GOFLAGS=-mod=mod go run k8s.io/release/cmd/release-notes@v0.16.5 \
    --branch "$branch" \
    --required-author "" \
    --org openperouter \
    --dependencies=false \
    --repo openperouter \
    --start-sha "$from" \
    --end-sha "$to" \
    --go-template "go-template:$script_dir/release-notes-template.md" \
    --output "$release_notes"

# Update RELEASE_NOTES.md
temp_notes=$(mktemp)

echo "## Release v$new_version" > "$temp_notes"
echo "" >> "$temp_notes"

cat "$release_notes" >> "$temp_notes"
echo "" >> "$temp_notes"
echo "" >> "$temp_notes"

tail -n +2 "$repo_root/RELEASE_NOTES.md" >> "$temp_notes"

mv "$temp_notes" "$repo_root/RELEASE_NOTES.md"

echo ""
echo "Release notes have been updated in RELEASE_NOTES.md"
echo ""

# Update website release notes by combining frontmatter with RELEASE_NOTES.md
website_release_notes="$repo_root/website/content/docs/release-notes.md"
website_frontmatter="$repo_root/website/content/docs/.release-notes-frontmatter.md"
temp_website_notes=$(mktemp)

# Combine frontmatter with RELEASE_NOTES.md content
cat "$website_frontmatter" > "$temp_website_notes"
echo "" >> "$temp_website_notes"
cat "$repo_root/RELEASE_NOTES.md" >> "$temp_website_notes"

mv "$temp_website_notes" "$website_release_notes"

echo "Release notes have been updated in website/content/docs/release-notes.md"
echo ""

echo "Changes to be committed:"
git diff "$repo_root/RELEASE_NOTES.md"
echo ""
git diff "$website_release_notes"

read -p "Do you want to commit these changes? (y/n) " -n 1 -r
echo
if [[ $REPLY =~ ^[Yy]$ ]]; then
    git add "$repo_root/RELEASE_NOTES.md" "$website_release_notes"
    git commit -m "Prepare the v$new_version release notes"
    echo ""
    echo "Committed: Prepare the v$new_version release notes"
else
    echo "Commit cancelled. Changes are staged but not committed."
fi
