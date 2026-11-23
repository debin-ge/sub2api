#!/bin/sh
# Print the upstream (Wei-Shaw/sub2api) release merged into HEAD, without "v":
# the highest "vX.Y.Z" tag reachable from HEAD. Prints an empty line when no
# git metadata or upstream tags are available.
# Upstream tags that exist locally but are not merged are ignored, so fetching
# the latest upstream tags before a build is safe.
#   git fetch --no-tags https://github.com/Wei-Shaw/sub2api.git '+refs/tags/v*:refs/tags/v*'
set -eu

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
BACKEND_DIR="$(CDPATH= cd -- "$SCRIPT_DIR/.." && pwd)"
REPO_DIR="$(CDPATH= cd -- "$BACKEND_DIR/.." && pwd)"

if command -v git >/dev/null 2>&1; then
  TAG="$(
    git -c safe.directory="$REPO_DIR" -C "$REPO_DIR" \
      tag --merged HEAD --list 'v[0-9]*' --sort=-v:refname 2>/dev/null | head -n 1
  )"
  if [ -n "$TAG" ]; then
    printf '%s\n' "${TAG#v}"
    exit 0
  fi
fi

printf '\n'
