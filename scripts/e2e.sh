#!/usr/bin/env bash
# Run on a FRESH install (before creating your account): sudo bash scripts/e2e.sh
# It creates the account admin / password123 - change that password afterwards.
# End-to-end test of the Haven API against a real Docker.
set -u
B=${HAVEN_URL:-http://127.0.0.1:8484}
J=$(mktemp)
DATA=${HAVEN_DATA:-/opt/haven}  # needs read access to the setup-token file
rm -f "$J"
c() { curl -s -b "$J" -c "$J" -H 'X-Haven: 1' -H 'Content-Type: application/json' "$@"; }
pass=0; fail=0
ok() { echo "PASS $1"; pass=$((pass+1)); }
bad() { echo "FAIL $1 :: $2"; fail=$((fail+1)); }
expect() { if echo "$2" | grep -q -- "$3"; then ok "$1"; else bad "$1" "$2"; fi; }

for i in $(seq 1 20); do curl -s $B/api/auth/status >/dev/null && break; sleep 0.5; done

expect "status needs setup" "$(c $B/api/auth/status)" '"setupRequired":true'
expect "unauth api blocked" "$(c -o /dev/null -w '%{http_code}' $B/api/apps)" '401'
expect "csrf header required" "$(curl -s -o /dev/null -w '%{http_code}' -X POST $B/api/auth/login -d '{}')" '403'
expect "wrong setup code" "$(c -X POST $B/api/auth/setup -d '{"token":"NOPE","username":"admin","password":"password123"}')" 'incorrect'
TOK=$(cat $DATA/setup-token)
BODY=$(printf '{"token":"%s","username":"admin","password":"password123"}' "$TOK")
expect "setup ok" "$(c -X POST $B/api/auth/setup -d "$BODY")" '"ok":true'
expect "authenticated" "$(c $B/api/auth/status)" '"authenticated":true'
c -X POST $B/api/auth/logout >/dev/null
expect "bad login" "$(c -X POST $B/api/auth/login -d '{"username":"admin","password":"wrong"}')" 'wrong username'
expect "login ok" "$(c -X POST $B/api/auth/login -d '{"username":"admin","password":"password123"}')" '"ok":true'
expect "catalog" "$(c $B/api/catalog | head -c 200000)" '"id":"jellyfin"'
expect "system" "$(c $B/api/system)" '"cpuCores"'
expect "spa fallback" "$(curl -s $B/some/route)" '<div id="root">'

# Preflight: port 8484 is Haven itself, so it must be flagged as in use
expect "preflight catches busy port" "$(c -X POST $B/api/preflight -d '{"catalogId":"ittools","values":{"WEB_PORT":"8484"}}')" 'already in use'
expect "preflight bad value" "$(c -X POST $B/api/preflight -d '{"catalogId":"ittools","values":{"WEB_PORT":"99999"}}')" '"level":"fail"'
expect "install blocked by preflight" "$(c -X POST $B/api/apps -d '{"catalogId":"ittools","values":{"WEB_PORT":"8484"}}')" '"checks"'

# Real install of a small app
R=$(c -X POST $B/api/apps -d '{"catalogId":"ittools","values":{"WEB_PORT":"18084"}}')
JOB=$(echo "$R" | sed -n 's/.*"id":"\([a-f0-9]*\)".*/\1/p')
expect "install started" "$R" '"status":"running"'
expect "second op on same app rejected" "$(c -X POST $B/api/apps/ittools/restart)" 'busy'
for i in $(seq 1 120); do S=$(c $B/api/jobs/$JOB); echo "$S" | grep -q '"status":"running"' || break; sleep 1; done
expect "install succeeded" "$S" '"status":"success"'
expect "app reachable" "$(curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:18084/)" '200'
expect "app listed running" "$(c $B/api/apps)" '"status":"running"'
expect "compose files on disk" "$(ls -a $DATA/apps/ittools)" 'compose.yml'
expect "env file private" "$(stat -c '%a' $DATA/apps/ittools/.env 2>/dev/null || stat -f '%Lp' $DATA/apps/ittools/.env)" '600'
expect "duplicate install rejected" "$(c -X POST $B/api/apps -d '{"catalogId":"ittools","values":{"WEB_PORT":"18085"}}')" 'already installed'
expect "port clash with stopped app detected" "$(c -X POST $B/api/preflight -d '{"catalogId":"memos","values":{"WEB_PORT":"18084"}}')" 'assigned to IT Tools'

# Per-app usage: memory, image size and data size appear shortly after install
for i in $(seq 1 30); do U=$(c $B/api/apps); echo "$U" | grep -q '"images":[1-9]' && echo "$U" | grep -q '"mem":[1-9]' && break; sleep 1; done
expect "app memory reported" "$U" '"mem":[1-9]'
expect "app image size reported" "$U" '"images":[1-9]'
expect "app data measured" "$U" '"dataAt":"2'
expect "diagnose healthy app" "$(c -X POST $B/api/apps/ittools/diagnose)" 'The web page answers on port'
expect "server health check runs" "$(c $B/api/diagnose)" '"findings"'
expect "power status answers" "$(c $B/api/host/power)" '"supported":true'
expect "local name status answers" "$(c $B/api/host/name)" '"supported":true'
expect "remote status answers" "$(c $B/api/remote)" '"supported":true'
waitjob() { local id; id=$(echo "$1" | sed -n 's/.*"id":"\([a-f0-9]*\)".*/\1/p'); for i in $(seq 1 120); do S=$(c $B/api/jobs/$id); echo "$S" | grep -q '"status":"running"' || break; sleep 1; done; echo "$S"; }
expect "stop" "$(waitjob "$(c -X POST $B/api/apps/ittools/stop)")" '"status":"success"'
expect "listed stopped" "$(c $B/api/apps)" '"status":"stopped"'
expect "start" "$(waitjob "$(c -X POST $B/api/apps/ittools/start)")" '"status":"success"'
expect "logs" "$(c $B/api/apps/ittools/logs)" '"logs"'
expect "update (re-pull)" "$(waitjob "$(c -X POST $B/api/apps/ittools/update)")" '"status":"success"'
expect "reconfigure port" "$(waitjob "$(c -X PUT $B/api/apps/ittools/settings -d '{"values":{"WEB_PORT":"18086"}}')")" '"status":"success"'
expect "new port reachable" "$(curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:18086/)" '200'

# Custom app with a container that crashes: health watch must catch it
R=$(c -X POST $B/api/apps/custom -d '{"id":"crashy","name":"Crashy","compose":"services:\n  c:\n    image: alpine:3.20\n    restart: unless-stopped\n    command: [\"sh\",\"-c\",\"sleep 1; exit 3\"]\n"}')
expect "crash detected" "$(waitjob "$R")" '"status":"failed"'
expect "bad custom compose rejected" "$(c -X POST $B/api/apps/custom -d '{"id":"broken","compose":"services: [oops"}')" 'invalid'
expect "uninstall crashy" "$(waitjob "$(c -X DELETE "$B/api/apps/crashy?removeData=1")")" '"status":"success"'

# Uninstall keeping data, then fully
expect "uninstall keep data" "$(waitjob "$(c -X DELETE $B/api/apps/ittools)")" '"status":"success"'
expect "app dir removed" "$(ls $DATA/apps)" '^$'
expect "path traversal blocked" "$(c -X DELETE "$B/api/apps/..%2F..%2Fetc")" 'not found'
expect "no leftover containers" "$(docker ps -a --filter label=com.docker.compose.project=haven-ittools -q | wc -l | tr -d ' ')" '^0$'

# TV pairing: the TV gets a code, a signed-in phone approves it, the TV is signed in
T=$(mktemp)
P=$(curl -s -c "$T" -H 'X-Haven: 1' -X POST $B/api/auth/tv/start)
PCODE=$(echo "$P" | sed -n 's/.*"code":"\([A-Z0-9-]*\)".*/\1/p')
PID=$(echo "$P" | sed -n 's/.*"id":"\([a-f0-9]*\)".*/\1/p')
expect "tv pairing starts" "$P" '"expiresIn"'
expect "tv pending" "$(curl -s -b "$T" -c "$T" -H 'X-Haven: 1' -X POST $B/api/auth/tv/poll -d "{\"id\":\"$PID\"}")" 'pending'
expect "approve needs sign-in" "$(curl -s -H 'X-Haven: 1' -X POST $B/api/auth/tv/approve -d "{\"code\":\"$PCODE\"}")" 'sign in'
expect "phone approves code" "$(c -X POST $B/api/auth/tv/approve -d "{\"code\":\"$PCODE\"}")" '"ok":true'
expect "tv signed in" "$(curl -s -b "$T" -c "$T" -H 'X-Haven: 1' -X POST $B/api/auth/tv/poll -d "{\"id\":\"$PID\"}")" 'approved'
expect "tv session works" "$(curl -s -b "$T" $B/api/auth/status)" '"authenticated":true'
rm -f "$T"

echo "---- $pass passed, $fail failed"
