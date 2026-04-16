#!/usr/bin/env bash
# Builds the sshnav binary via Docker and copies it to the host.
# Usage:
#   bash build.sh                   # extracts binary to ./sshnav
#   INSTALL_DIR=/usr/local/bin bash build.sh   # also installs it
set -euo pipefail

IMAGE="sshnav-builder"
BINARY="sshnav"
INSTALL_DIR="${INSTALL_DIR:-}"

err()  { echo "ERROR: $*" >&2; exit 1; }
info() { echo "  >> $*"; }

command -v docker >/dev/null 2>&1 || err "Docker is not installed."

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

info "Building Docker image…"
docker build --tag "$IMAGE" .

info "Extracting binary…"
# BuildKit --output is cleaner but requires BuildKit; fall back to docker cp
if docker build --help 2>&1 | grep -q '\-\-output'; then
  docker build \
    --output "type=local,dest=${SCRIPT_DIR}" \
    --tag "$IMAGE" \
    . 2>/dev/null \
  && mv -f "${SCRIPT_DIR}/sshnav" "${SCRIPT_DIR}/${BINARY}" 2>/dev/null || true
fi

# Always-reliable fallback: create a throwaway container and cp from it
if [[ ! -x "${SCRIPT_DIR}/${BINARY}" ]]; then
  CID=$(docker create "$IMAGE")
  docker cp "${CID}:/sshnav" "${SCRIPT_DIR}/${BINARY}"
  docker rm -f "$CID" >/dev/null
fi

chmod +x "${SCRIPT_DIR}/${BINARY}"
info "Binary ready: ${SCRIPT_DIR}/${BINARY}"

if [[ -n "$INSTALL_DIR" ]]; then
  info "Installing to ${INSTALL_DIR}/${BINARY}…"
  if [[ -w "$INSTALL_DIR" ]]; then
    mv "${SCRIPT_DIR}/${BINARY}" "${INSTALL_DIR}/${BINARY}"
  else
    sudo mv "${SCRIPT_DIR}/${BINARY}" "${INSTALL_DIR}/${BINARY}"
  fi
  info "Installed: ${INSTALL_DIR}/${BINARY}"
fi
