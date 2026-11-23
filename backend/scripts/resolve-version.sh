#!/bin/sh
set -eu

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
BACKEND_DIR="$(CDPATH= cd -- "$SCRIPT_DIR/.." && pwd)"
REPO_DIR="$(CDPATH= cd -- "$BACKEND_DIR/.." && pwd)"
PROJECT_VERSION_FILE="$BACKEND_DIR/cmd/server/PROJECT_VERSION"
VERSION_FILE="$BACKEND_DIR/cmd/server/VERSION"

# Prefer the exact release tag when building from a tagged checkout so source
# builds don't inherit a stale version file. Fork "X.Y.Z" tags win over
# upstream "vX.Y.Z" tags.
if command -v git >/dev/null 2>&1; then
  TAG="$(
    git -C "$REPO_DIR" describe --tags --exact-match --match '[0-9]*' 2>/dev/null || \
    git -C "$REPO_DIR" describe --tags --exact-match --match 'v[0-9]*' 2>/dev/null || \
    true
  )"
  if [ -n "$TAG" ]; then
    printf '%s\n' "${TAG#v}"
    exit 0
  fi
fi

# Fork-owned PROJECT_VERSION wins over upstream's VERSION file, which tracks
# Wei-Shaw/sub2api releases rather than this fork's tags.
if [ -f "$PROJECT_VERSION_FILE" ]; then
  PROJECT_VERSION="$(tr -d '\r\n' < "$PROJECT_VERSION_FILE")"
  if [ -n "$PROJECT_VERSION" ]; then
    printf '%s\n' "$PROJECT_VERSION"
    exit 0
  fi
fi

printf '%s\n' "$(tr -d '\r\n' < "$VERSION_FILE")"
