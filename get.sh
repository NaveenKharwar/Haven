#!/usr/bin/env bash
# Haven one-line installer.
#
#   curl -fsSL <release address>/get.sh | sudo bash
#
# It downloads the newest Haven release, checks every file against the
# release's checksum list, then runs the installer. Rerun it any time to upgrade.
#
# Options (environment variables, put them after sudo):
#   HAVEN_VERSION=0.1.15   install this version (0.1.15 or newer) instead of the newest
#   HAVEN_PORT=9000        listen on this port (default 8484, or the one already in use)
#   HAVEN_DRY_RUN=1        download and check the files, but install nothing
#
# To remove Haven (your apps and their data stay):
#   curl -fsSL <release address>/get.sh | sudo bash -s -- uninstall
#
# Want to read it before running it? Save it, read it, then run it:
#   curl -fsSLO <release address>/get.sh && less get.sh && sudo bash get.sh
set -euo pipefail

# The release process replaces @REPO@ with the repository that published this file.
# (Only on the next line: the check further down is written so it isn't rewritten as well.)
REPO="${HAVEN_REPO:-@REPO@}"
VERSION="${HAVEN_VERSION:-latest}"
ACTION="${1:-install}"

say()  { printf '\033[1;34m==>\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m!!\033[0m %s\n' "$*" >&2; }
die()  { printf '\033[1;31mError:\033[0m %s\n' "$*" >&2; exit 1; }

case "$ACTION" in install|uninstall) ;; -h|--help|help) sed -n '2,19p' "$0" 2>/dev/null | sed 's/^# \{0,1\}//'; exit 0 ;; *) die "unknown command '$ACTION' (use: install or uninstall)" ;; esac

DRY="${HAVEN_DRY_RUN:-}"
[ "${HAVEN_NO_ROOT_CHECK:-}" = "1" ] || [ -n "$DRY" ] || [ "$(id -u)" -eq 0 ] || die "please run with sudo, for example: curl -fsSL <address>/get.sh | sudo bash"

case "$(uname -m)" in
  x86_64|amd64)  ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  *) die "unsupported CPU type: $(uname -m) (Haven supports amd64 and arm64)" ;;
esac

if [ -n "${HAVEN_BASE_URL:-}" ]; then
  BASE="$HAVEN_BASE_URL"                                  # used by the tests
else
  [ "$REPO" != "@""REPO@" ] || die "this copy of get.sh was not published by a release. Download it from the Releases page, or set HAVEN_REPO=owner/name"
  case "$REPO" in */*) ;; *) die "HAVEN_REPO must look like owner/name" ;; esac
  if [ "$VERSION" = "latest" ]; then
    BASE="https://github.com/$REPO/releases/latest/download"
  else
    BASE="https://github.com/$REPO/releases/download/v${VERSION#v}"
  fi
fi

if command -v curl >/dev/null 2>&1; then
  fetch() { curl -fsSL --retry 3 --connect-timeout 15 -o "$2" "$1"; }
elif command -v wget >/dev/null 2>&1; then
  fetch() { wget -q -O "$2" "$1"; }
else
  die "curl or wget is needed to download Haven (on Ubuntu: sudo apt install curl)"
fi

if command -v sha256sum >/dev/null 2>&1; then
  sha() { sha256sum "$1" | awk '{print $1}'; }
elif command -v shasum >/dev/null 2>&1; then
  sha() { shasum -a 256 "$1" | awk '{print $1}'; }
else
  die "sha256sum is needed to check the download"
fi

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
PKG="$TMP/haven"
mkdir -p "$PKG"

if [ "$ACTION" = "uninstall" ]; then FILES=(uninstall.sh); else FILES=("haven-linux-$ARCH" install.sh uninstall.sh); fi

say "Downloading Haven (${VERSION}) for $ARCH"
fetch "$BASE/SHA256SUMS" "$TMP/SHA256SUMS" || die "could not download the checksum list from $BASE. Check the address, the version and your internet connection"
for f in "${FILES[@]}"; do
  fetch "$BASE/$f" "$PKG/$f" || die "could not download $f from $BASE. If you picked a version with HAVEN_VERSION, note that releases before 0.1.15 don't include the install scripts"
done

say "Checking the files against the release's checksums"
for f in "${FILES[@]}"; do
  want="$(awk -v n="$f" '{ name=$2; sub(/^\*/, "", name); if (name == n) { print $1; exit } }' "$TMP/SHA256SUMS")"
  [ -n "$want" ] || die "$f is not in the checksum list, so it cannot be trusted. Nothing was installed"
  got="$(sha "$PKG/$f")"
  [ "$got" = "$want" ] || die "$f does not match its checksum (expected $want, got $got). The download may be damaged or tampered with. Nothing was installed"
done
chmod +x "$PKG"/*.sh 2>/dev/null || true
say "All files verified"

if [ -n "$DRY" ]; then
  say "Dry run: nothing was installed."
  exit 0
fi

# The installer must not read from this script's own pipe.
if [ "$ACTION" = "uninstall" ]; then
  exec bash "$PKG/uninstall.sh" </dev/null
fi
cd "$PKG"
bash ./install.sh </dev/null
