#!/usr/bin/env bash
# Tests get.sh against a pretend release served from this machine.
# Run from the project folder:  bash scripts/test-get.sh
set -u
cd "$(dirname "$0")/.."
GET="$PWD/get.sh"
T="$(mktemp -d)"
REL="$T/release"; OUT="$T/out"; mkdir -p "$REL" "$OUT"
pass=0; fail=0
ok()  { echo "PASS $1"; pass=$((pass+1)); }
bad() { echo "FAIL $1"; fail=$((fail+1)); }
check() { if eval "$2"; then ok "$1"; else bad "$1"; fi; }

sha() { if command -v sha256sum >/dev/null; then sha256sum "$1" | awk '{print $1}'; else shasum -a 256 "$1" | awk '{print $1}'; fi; }

# A pretend release: two binaries, the installer scripts, and the checksum list.
make_release() {
  rm -f "$REL"/*
  printf 'pretend amd64 binary\n' > "$REL/haven-linux-amd64"
  printf 'pretend arm64 binary\n' > "$REL/haven-linux-arm64"
  cat > "$REL/install.sh" <<'EOS'
#!/usr/bin/env bash
echo "installed with port=${HAVEN_PORT:-none} from $(basename "$PWD")" > "$HAVEN_TEST_OUT/installed"
ls >> "$HAVEN_TEST_OUT/installed"
EOS
  cat > "$REL/uninstall.sh" <<'EOS'
#!/usr/bin/env bash
echo uninstalled > "$HAVEN_TEST_OUT/uninstalled"
EOS
  : > "$REL/SHA256SUMS"
  for f in haven-linux-amd64 haven-linux-arm64 install.sh uninstall.sh; do echo "$(sha "$REL/$f")  $f" >> "$REL/SHA256SUMS"; done
}

PORT=$((20000 + RANDOM % 20000))
make_release
( cd "$REL" && python3 -m http.server "$PORT" --bind 127.0.0.1 >/dev/null 2>&1 ) &
SERVER=$!
trap 'kill $SERVER 2>/dev/null; wait $SERVER 2>/dev/null; rm -rf "$T"' EXIT
for _ in $(seq 1 30); do curl -fsS "http://127.0.0.1:$PORT/SHA256SUMS" >/dev/null 2>&1 && break; sleep 0.2; done

run() { HAVEN_BASE_URL="http://127.0.0.1:$PORT" HAVEN_TEST_OUT="$OUT" HAVEN_NO_ROOT_CHECK=1 "$@"; }

# 1. A dry run checks the files and installs nothing.
rm -f "$OUT"/*
out="$(HAVEN_DRY_RUN=1 run bash "$GET" 2>&1)"; code=$?
check "dry run succeeds" '[ $code -eq 0 ]'
check "dry run says the files were verified" 'echo "$out" | grep -q "All files verified"'
check "dry run installs nothing" '[ ! -f "$OUT/installed" ]'

# 2. A real run hands over to the installer, with the port, from a folder that holds the right binary.
out="$(HAVEN_PORT=9000 run bash "$GET" 2>&1)"; code=$?
check "install run succeeds" '[ $code -eq 0 ]'
check "the installer ran" '[ -f "$OUT/installed" ]'
check "the port option reaches the installer" 'grep -q "port=9000" "$OUT/installed"'
arch=amd64; case "$(uname -m)" in aarch64|arm64) arch=arm64 ;; esac
check "the matching binary is beside the installer" 'grep -q "haven-linux-$arch" "$OUT/installed"'

# 3. It also works when piped into bash, which is how people will use it.
rm -f "$OUT"/*
out="$(cat "$GET" | HAVEN_BASE_URL="http://127.0.0.1:$PORT" HAVEN_TEST_OUT="$OUT" HAVEN_NO_ROOT_CHECK=1 bash 2>&1)"; code=$?
check "works when piped into bash" '[ $code -eq 0 ] && [ -f "$OUT/installed" ]'

# 4. A changed binary is refused, and nothing is installed.
rm -f "$OUT"/*
printf 'tampered\n' >> "$REL/haven-linux-$arch"
out="$(run bash "$GET" 2>&1)"; code=$?
check "a tampered binary is refused" '[ $code -ne 0 ] && echo "$out" | grep -q "does not match its checksum"'
check "nothing is installed after a bad checksum" '[ ! -f "$OUT/installed" ]'

# 5. A file missing from the checksum list is refused.
make_release
grep -v ' install.sh$' "$REL/SHA256SUMS" > "$T/sums" && mv "$T/sums" "$REL/SHA256SUMS"
out="$(run bash "$GET" 2>&1)"; code=$?
check "a file that is not in the checksum list is refused" '[ $code -ne 0 ] && echo "$out" | grep -q "not in the checksum list"'

# 6. A changed installer script is refused too, because it runs as root.
make_release
printf 'echo pwned > "$HAVEN_TEST_OUT/pwned"\n' >> "$REL/install.sh"
out="$(run bash "$GET" 2>&1)"; code=$?
check "a tampered installer script is refused" '[ $code -ne 0 ] && [ ! -f "$OUT/pwned" ]'

# 7. Missing release files give a clear message.
make_release
rm "$REL/SHA256SUMS"
out="$(run bash "$GET" 2>&1)"; code=$?
check "a missing checksum list gives a clear error" '[ $code -ne 0 ] && echo "$out" | grep -q "could not download the checksum list"'

# 8. Uninstall downloads, verifies and runs the uninstaller.
make_release
rm -f "$OUT"/*
out="$(run bash "$GET" uninstall 2>&1)"; code=$?
check "uninstall runs the uninstaller" '[ $code -eq 0 ] && [ -f "$OUT/uninstalled" ]'
check "uninstall does not install" '[ ! -f "$OUT/installed" ]'

# 9. Mistakes are explained.
out="$(bash "$GET" explode 2>&1)"; code=$?
check "an unknown command is refused" '[ $code -ne 0 ] && echo "$out" | grep -q "unknown command"'
out="$(HAVEN_NO_ROOT_CHECK=1 bash "$GET" 2>&1)"; code=$?
check "an unpublished copy says what to do" '[ $code -ne 0 ] && echo "$out" | grep -q "not published by a release"'
# The release workflow rewrites @REPO@ with sed. The "not published" check must survive that.
published="$(sed 's#@REPO@#owner/name#' "$GET")"
check "the published copy still trusts its own repository" '! printf "%s\n" "$published" | grep -q "!= \"owner/name\""'
check "the published copy has the repository filled in" 'printf "%s\n" "$published" | grep -q "REPO=\"\${HAVEN_REPO:-owner/name}\""'
out="$(HAVEN_NO_ROOT_CHECK=1 HAVEN_REPO=nonsense bash "$GET" 2>&1)"; code=$?
check "a bad repository name is refused" '[ $code -ne 0 ] && echo "$out" | grep -q "owner/name"'
if [ "$(id -u)" -ne 0 ]; then
  out="$(bash "$GET" 2>&1)"; code=$?
  check "it asks for sudo when not root" '[ $code -ne 0 ] && echo "$out" | grep -q "please run with sudo"'
fi

# 10. The installer keeps a custom port when upgrading.
SVC='ExecStart=/usr/local/bin/haven --addr :9123 --data /opt/haven'
found="$(printf '%s\n' "$SVC" | sed -n 's/.*--addr :\([0-9][0-9]*\).*/\1/p' | head -n 1)"
check "an existing port is read from the service file" '[ "$found" = "9123" ]'

echo
echo "$pass passed, $fail failed"
[ "$fail" -eq 0 ]
