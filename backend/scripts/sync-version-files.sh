#!/bin/sh
# Refresh the fork-owned version files embedded into the binary:
#   cmd/server/PROJECT_VERSION   this fork's release tag (bare "X.Y.Z" tags)
#   cmd/server/UPSTREAM_VERSION  Wei-Shaw/sub2api release merged in ("vX.Y.Z" tags)
# cmd/server/VERSION is owned by upstream and is left alone.
#
# Run after tagging a fork release or merging an upstream release, then commit.
# Builds without git metadata (Docker, .git is in .dockerignore) read these files.
#
# Usage: backend/scripts/sync-version-files.sh [ref]   (default: HEAD)
# Needs upstream tags locally: git fetch upstream --tags
set -eu

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
BACKEND_DIR="$(CDPATH= cd -- "$SCRIPT_DIR/.." && pwd)"
REPO_DIR="$(CDPATH= cd -- "$BACKEND_DIR/.." && pwd)"
SERVER_DIR="$BACKEND_DIR/cmd/server"
REF="${1:-HEAD}"

nearest_tag() {
  git -C "$REPO_DIR" describe --tags --abbrev=0 --match "$1" "$REF"
}

PROJECT="$(nearest_tag '[0-9]*')"
UPSTREAM="$(nearest_tag 'v[0-9]*')"
UPSTREAM="${UPSTREAM#v}"

printf '%s\n' "$PROJECT" > "$SERVER_DIR/PROJECT_VERSION"
printf '%s\n' "$UPSTREAM" > "$SERVER_DIR/UPSTREAM_VERSION"
printf 'PROJECT_VERSION  -> %s\nUPSTREAM_VERSION -> %s\n' "$PROJECT" "$UPSTREAM"
