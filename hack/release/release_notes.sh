#!/bin/bash

# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

# Function to generate release notes
# It takes two arguments:
# $1: start commit
# $2: end commit (or branch)
generate_release_notes() {
    local start_commit=$1
    local end_commit=$2
    local branch=$2

    local release_notes
    release_notes=$(mktemp)

    trap 'rm -f "$release_notes"' RETURN

    GOFLAGS=-mod=mod go run k8s.io/release/cmd/release-notes@v0.16.5 \
        --branch "$branch" \
        --required-author "" \
        --org metallb \
        --dependencies=false \
        --repo frr-k8s \
        --start-sha "$start_commit" \
        --end-sha "$end_commit" \
        --output "$release_notes"

    cat "$release_notes"
}

# Function to compare semantic versions
version_gt() {
    test "$(printf '%s\n' "$1" "$2" | sort -V | head -n 1)" != "$1" || [ "$1" == "$2" ]
}

if [ -z "$1" ]; then
    echo "Usage: $0 <version>"
    echo "Example: $0 1.2.3"
    exit 1
fi

NEW_VERSION=$1

# Check for GITHUB_TOKEN
if [ -z "$GITHUB_TOKEN" ]; then
    echo "Error: GITHUB_TOKEN environment variable is not set."
    exit 1
fi

echo "Preparing release for version $NEW_VERSION"

# Find the commit of the previous release notes generation
LAST_RELEASE_COMMIT_INFO=$(git log --grep="^Generate release for version" --pretty=format:"%H %s" -n 1)

if [ -z "$LAST_RELEASE_COMMIT_INFO" ]; then
    echo "No previous release found. Using the first commit as the start of the range."
    START_COMMIT=$(git rev-list --max-parents=0 HEAD)
else
    LAST_RELEASE_COMMIT=$(echo "$LAST_RELEASE_COMMIT_INFO" | cut -d' ' -f1)
    LAST_RELEASE_MSG=$(echo "$LAST_RELEASE_COMMIT_INFO" | cut -d' ' -f2-)
    PREVIOUS_VERSION=$(echo "$LAST_RELEASE_MSG" | awk '{print $5}')
    
    echo "Previous release version: $PREVIOUS_VERSION"

    # Compare versions
    if ! version_gt "$NEW_VERSION" "$PREVIOUS_VERSION"; then
        echo "Error: New version ($NEW_VERSION) must be greater than the previous version ($PREVIOUS_VERSION)."
        exit 1
    fi
    START_COMMIT=$LAST_RELEASE_COMMIT
fi

echo "Generating release notes from commit $START_COMMIT to main..."

# Generate release notes content
NOTES_CONTENT=$(generate_release_notes "$START_COMMIT" "main")

if [ -z "$NOTES_CONTENT" ]; then
    echo "No new pull requests found since the last release. No release notes to generate."
    exit 0
fi

# Prepare the new release notes section
NEW_RELEASE_SECTION="## Release $NEW_VERSION\n\n$NOTES_CONTENT"

RELEASE_NOTES_FILE="website/content/docs/release-notes.md"
TEMP_FILE=$(mktemp)

awk -v new_section="$(echo -e "\n$NEW_RELEASE_SECTION")" '1;/^# Release Notes/ {printf "%s", new_section}' "$RELEASE_NOTES_FILE" > "$TEMP_FILE"
mv "$TEMP_FILE" "$RELEASE_NOTES_FILE"

echo "Updated $RELEASE_NOTES_FILE"

# Commit the changes
git add "$RELEASE_NOTES_FILE"
git commit -m "Generate release for version $NEW_VERSION"

echo "Committed release notes for version $NEW_VERSION"
