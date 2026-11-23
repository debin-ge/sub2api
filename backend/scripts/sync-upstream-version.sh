#!/bin/sh
# Refresh cmd/server/UPSTREAM_VERSION after merging an upstream (Wei-Shaw/sub2api)
# release. Upstream tags are "vX.Y.Z"; this fork's own tags are bare "X.Y.Z",
# so the nearest reachable v-tag is the upstream release merged into HEAD.
#
# Usage: backend/scripts/sync-upstream-version.sh [ref]   (default: HEAD)
# Needs upstream tags locally: git fetch upstream --tags
set -eu

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
BACKEND_DIR="$(CDPATH= cd -- "$SCRIPT_DIR/.." && pwd)"
REPO_DIR="$(CDPATH= cd -- "$BACKEND_DIR/.." && pwd)"
VERSION_FILE="$BACKEND_DIR/cmd/server/UPSTREAM_VERSION"
REF="${1:-HEAD}"

TAG="$(git -C "$REPO_DIR" describe --tags --abbrev=0 --match 'v[0-9]*' "$REF")"
VERSION="${TAG#v}"
printf '%s\n' "$VERSION" > "$VERSION_FILE"
printf 'UPSTREAM_VERSION -> %s\n' "$VERSION"
