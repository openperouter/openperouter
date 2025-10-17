---
weight: 30
title: "How to release"
description: "How to release"
icon: "article"
date: "2025-06-15T15:03:22+02:00"
lastmod: "2025-06-15T15:03:22+02:00"
toc: true
---
# Release process

## Preparing the branch

Checkout the release branch and merge main or cherry pick the relevant commits:

```bash
git checkout v0.9
git merge main
git push
```

### Using cherry picks

In case only a subset of the changes are brought to the new release, cherry-pick
must be used.

```bash
git checkout v0.9
git cherry-pick -x f1f86ed658c1e8a6f90f967ed94881d61476b4c0
git push
```

## Clean the working directory

The release script only works if the Git working directory is completely clean: no pending modifications, no untracked files, nothing. Make sure everything is clean, or run the release from a fresh checkout.

The release script will abort if the working directory isn’t right.

## Run the release script
Run `OPENPE_VERSION="X.Y.Z" make cutrelease` from the main branch. This will create the appropriate branches, commits and tags in your local repository.

Where branch is the branch being released, first and last commit is the interval
we want to generate the release notes for.

In order to prepare the release notes, the `GITHUB_TOKEN` environment variable must be set with a github token which has the following permissions:

Read access to:

- Contents
- Pull requests
- Commit statuses


## Push the new artifacts
Run git push origin main `vX.Y` --tags. This will push all pending changes both in main and the release branch, as well as the new tag for the release.

## Wait for the image repositories to update
When you pushed, GitHub actions kicked off a set of image builds for the new tag. You need to wait for these images to be pushed live before creating a new release. Check on quay.io that the tagget version exists.

## Create a new release on github
By default, new tags show up de-emphasized in the list of releases. Create a new release attached to the tag you just pushed. Make the description point to the release notes on the website.

