#!/bin/sh
# Print this project's version: the highest fork release tag ("X.Y.Z") merged
# into HEAD, else upstream's cmd/server/VERSION when no git metadata is present.
# Only tags reachable from HEAD count, so tags on other branches are ignored.
set -eu

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
BACKEND_DIR="$(CDPATH= cd -- "$SCRIPT_DIR/.." && pwd)"
REPO_DIR="$(CDPATH= cd -- "$BACKEND_DIR/.." && pwd)"
VERSION_FILE="$BACKEND_DIR/cmd/server/VERSION"

if command -v git >/dev/null 2>&1; then
  # safe.directory: inside Docker the mounted .git belongs to another user.
  TAG="$(
    git -c safe.directory="$REPO_DIR" -C "$REPO_DIR" \
      tag --merged HEAD --list '[0-9]*' --sort=-v:refname 2>/dev/null | head -n 1
  )"
  if [ -n "$TAG" ]; then
    printf '%s\n' "$TAG"
    exit 0
  fi
fi

printf '%s\n' "$(tr -d '\r\n' < "$VERSION_FILE")"
