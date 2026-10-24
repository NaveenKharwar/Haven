#!/usr/bin/env bash
# Builds Haven for Ubuntu servers (Intel/AMD and ARM) into ./release
set -euo pipefail
cd "$(dirname "$0")"

# The version comes from the newest git tag (v0.1.7 -> 0.1.7), so a local build
# says the same as the release it is based on. Set VERSION to override.
if [ -z "${VERSION:-}" ]; then
  VERSION="$(git describe --tags --abbrev=0 2>/dev/null | sed 's/^v//' || true)"
  VERSION="${VERSION:-0.1.0}"
fi
OUT=release

# Where this build looks for new releases ("owner/name"). By default it is the
# GitHub repository this folder is pushed to, so a new repository needs no edit.
if [ -z "${UPDATE_REPO:-}" ]; then
  url="$(git config --get remote.origin.url 2>/dev/null || true)"
  UPDATE_REPO="$(printf '%s' "$url" | sed -E 's#^(https://github.com/|git@github.com:)##; s#\.git$##')"
  case "$UPDATE_REPO" in */*) ;; *) UPDATE_REPO="" ;; esac
fi
echo "==> Update source: ${UPDATE_REPO:-none}"

echo "==> Building web UI"
(cd web && { [ -d node_modules ] || npm ci --no-fund --no-audit; } && npm run build)

echo "==> Running tests"
go vet ./...
go test ./...

rm -rf "$OUT"
mkdir -p "$OUT/haven-$VERSION"
for arch in amd64 arm64; do
  echo "==> Building linux/$arch"
  CGO_ENABLED=0 GOOS=linux GOARCH=$arch go build -trimpath \
    -ldflags "-s -w -X main.Version=$VERSION -X main.UpdateRepo=$UPDATE_REPO" \
    -o "$OUT/haven-$VERSION/haven-linux-$arch" .
done
cp install.sh uninstall.sh "$OUT/haven-$VERSION/"
cp scripts/e2e.sh "$OUT/haven-$VERSION/e2e.sh"
chmod +x "$OUT/haven-$VERSION/"*.sh
tar -C "$OUT" -czf "$OUT/haven-$VERSION.tar.gz" "haven-$VERSION"

echo
echo "Done: $OUT/haven-$VERSION.tar.gz"
echo "Copy it to your server and run:"
echo "  tar xzf haven-$VERSION.tar.gz && cd haven-$VERSION && sudo ./install.sh"
