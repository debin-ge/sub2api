#!/bin/sh
set -eu

PROMETHEUS_VERSION="3.4.2"
SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)

if [ -n "${PROMTOOL_BIN:-}" ]; then
  PROMTOOL=$PROMTOOL_BIN
else
  OS=$(uname -s | tr '[:upper:]' '[:lower:]')
  ARCH=$(uname -m)
  case "$ARCH" in
    x86_64) ARCH=amd64 ;;
    aarch64|arm64) ARCH=arm64 ;;
    *) echo "unsupported architecture: $ARCH" >&2; exit 1 ;;
  esac
  case "$OS" in
    linux|darwin) ;;
    *) echo "unsupported operating system: $OS" >&2; exit 1 ;;
  esac
  # Official v3.4.2 archive digests from:
  # https://github.com/prometheus/prometheus/releases/download/v3.4.2/sha256sums.txt
  case "$OS-$ARCH" in
    darwin-amd64) EXPECTED_SHA256="0c046a68e51c0e7245b7cc37a83c3db69cc0af8224de9947b24c48512f120462" ;;
    darwin-arm64) EXPECTED_SHA256="194e57f02dd2d1e3691eafc6f14b11cdc2c569d64f9cdefd0bf18b561843e097" ;;
    linux-amd64) EXPECTED_SHA256="630177c6ad011193987904f09ffafec29d531abfeb5e43fa3714e376e5f28ddc" ;;
    linux-arm64) EXPECTED_SHA256="6c4ba48d2efe582bd70c296a2184fbb1adf03c1cb3ef8e8b61bb009ed3d73c85" ;;
    *) echo "unsupported promtool platform: $OS-$ARCH" >&2; exit 1 ;;
  esac
  TMP_DIR=$(mktemp -d)
  trap 'rm -rf "$TMP_DIR"' EXIT INT TERM
  ARCHIVE="prometheus-${PROMETHEUS_VERSION}.${OS}-${ARCH}.tar.gz"
  curl -fsSL "https://github.com/prometheus/prometheus/releases/download/v${PROMETHEUS_VERSION}/${ARCHIVE}" -o "$TMP_DIR/$ARCHIVE"
  if command -v sha256sum >/dev/null 2>&1; then
    ACTUAL_SHA256=$(sha256sum "$TMP_DIR/$ARCHIVE" | awk '{print $1}')
  elif command -v shasum >/dev/null 2>&1; then
    ACTUAL_SHA256=$(shasum -a 256 "$TMP_DIR/$ARCHIVE" | awk '{print $1}')
  else
    echo "sha256sum or shasum is required to verify promtool" >&2
    exit 1
  fi
  if [ "$ACTUAL_SHA256" != "$EXPECTED_SHA256" ]; then
    echo "promtool archive SHA-256 mismatch" >&2
    exit 1
  fi
  tar -xzf "$TMP_DIR/$ARCHIVE" -C "$TMP_DIR"
  PROMTOOL="$TMP_DIR/prometheus-${PROMETHEUS_VERSION}.${OS}-${ARCH}/promtool"
fi

cd "$SCRIPT_DIR"
"$PROMTOOL" check rules radar-alerts.yml
"$PROMTOOL" check rules video-alerts.yml
"$PROMTOOL" check config --syntax-only prometheus-radar.yml
"$PROMTOOL" test rules radar-rules.test.yml
"$PROMTOOL" test rules video-rules.test.yml
