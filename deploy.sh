#!/usr/bin/env bash
# Builds Haven and installs it on your server in one go.
#   ./deploy.sh user@SERVER_IP          (add --no-build to reuse the last build)
# Needs SSH access to the server. You'll be asked for the server's sudo
# password once, by the server itself. Your apps keep running during the upgrade.
set -euo pipefail
cd "$(dirname "$0")"

TARGET="${1:-}"
[ -n "$TARGET" ] || { echo "Usage: ./deploy.sh user@SERVER_IP [--no-build]"; exit 1; }
VERSION="${VERSION:-0.1.0}"

if [ "${2:-}" != "--no-build" ]; then
  VERSION="$VERSION" ./build.sh
fi
PKG="release/haven-$VERSION.tar.gz"
[ -f "$PKG" ] || { echo "Missing $PKG. Run ./build.sh first."; exit 1; }

echo "==> Copying to $TARGET"
scp "$PKG" "$TARGET:/tmp/haven-$VERSION.tar.gz"

echo "==> Installing on the server (it may ask for your sudo password)"
ssh -t "$TARGET" "set -e; cd /tmp && rm -rf haven-$VERSION && tar xzf haven-$VERSION.tar.gz && cd haven-$VERSION && sudo ./install.sh"

echo
echo "Done. Refresh Haven in your browser."
