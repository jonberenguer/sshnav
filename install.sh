#!/usr/bin/env bash
set -euo pipefail

BINARY_NAME="sshnav"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"

err() { echo "ERROR: $*" >&2; exit 1; }
info() { echo "  >> $*"; }

command -v go >/dev/null 2>&1 || err "Go is not installed. Install from https://go.dev/dl/"
command -v git >/dev/null 2>&1 || err "git is required"

GO_VERSION=$(go version | awk '{print $3}' | sed 's/go//')
REQUIRED="1.22"
# Compare major.minor only
if printf '%s\n%s\n' "$REQUIRED" "$GO_VERSION" | sort -V -C 2>/dev/null; then
  : # ok
else
  err "Go >= $REQUIRED required (found $GO_VERSION)"
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

info "Fetching dependencies…"
go mod tidy

info "Building $BINARY_NAME…"
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o "$BINARY_NAME" .

if [[ "${1:-}" == "--install" ]]; then
  info "Installing to $INSTALL_DIR/$BINARY_NAME (may require sudo)…"
  if [[ -w "$INSTALL_DIR" ]]; then
    mv "$BINARY_NAME" "$INSTALL_DIR/$BINARY_NAME"
  else
    sudo mv "$BINARY_NAME" "$INSTALL_DIR/$BINARY_NAME"
  fi
  info "Installed: $INSTALL_DIR/$BINARY_NAME"
else
  info "Binary ready: $SCRIPT_DIR/$BINARY_NAME"
  info "Run with: ./$BINARY_NAME"
  info "Or re-run with: bash install.sh --install  (to install to $INSTALL_DIR)"
fi
