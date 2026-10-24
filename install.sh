#!/usr/bin/env bash
# Installs (or upgrades) Haven on Ubuntu. Run from the unpacked release folder:
#   sudo ./install.sh
set -euo pipefail

# A port given by the user wins; otherwise keep the port an existing install already uses.
PORT="${HAVEN_PORT:-}"
if [ -z "$PORT" ] && [ -f /etc/systemd/system/haven.service ]; then
  PORT="$(sed -n 's/.*--addr :\([0-9][0-9]*\).*/\1/p' /etc/systemd/system/haven.service | head -n 1)"
fi
PORT="${PORT:-8484}"
DATA=/opt/haven
HERE="$(cd "$(dirname "$0")" && pwd)"

say()  { printf '\033[1;34m==>\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m!!\033[0m %s\n' "$*"; }
die()  { printf '\033[1;31mError:\033[0m %s\n' "$*" >&2; exit 1; }

[ "$(id -u)" -eq 0 ] || die "please run with sudo: sudo ./install.sh"
command -v systemctl >/dev/null || die "systemd is required"

case "$(uname -m)" in
  x86_64|amd64) ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  *) die "unsupported CPU type: $(uname -m) (Haven supports amd64 and arm64)" ;;
esac
BIN="$HERE/haven-linux-$ARCH"
[ -f "$BIN" ] || die "missing $BIN — run this from the unpacked release folder"

if [ -r /etc/os-release ]; then
  . /etc/os-release
  [ "${ID:-}" = "ubuntu" ] || [ "${ID:-}" = "debian" ] || warn "Haven is tested on Ubuntu; you are on ${PRETTY_NAME:-unknown}"
fi

# --- Docker ---------------------------------------------------------------
if ! command -v docker >/dev/null 2>&1; then
  say "Docker not found. Installing it with Docker's official script (get.docker.com)"
  command -v curl >/dev/null || { apt-get update -qq && apt-get install -y -qq curl ca-certificates; }
  curl -fsSL https://get.docker.com | sh
fi
systemctl enable --now docker >/dev/null 2>&1 || true
if ! docker compose version >/dev/null 2>&1; then
  say "Installing the Docker Compose plugin"
  apt-get update -qq && apt-get install -y -qq docker-compose-plugin || die "could not install docker-compose-plugin"
fi
docker info >/dev/null 2>&1 || die "Docker is installed but not running (try: sudo systemctl status docker)"
say "Docker $(docker version --format '{{.Server.Version}}') with Compose $(docker compose version --short)"

# --- User that owns app files --------------------------------------------
# Apps run as your normal user (not root) so files they create are yours.
OWNER="${SUDO_USER:-}"
if [ -z "$OWNER" ] || [ "$OWNER" = "root" ]; then
  OWNER="$(getent passwd 1000 | cut -d: -f1 || true)"
fi
if [ -n "$OWNER" ]; then
  PUID="$(id -u "$OWNER")"; PGID="$(id -g "$OWNER")"
else
  PUID=1000; PGID=1000
fi
say "Apps will run as user ${OWNER:-uid 1000} ($PUID:$PGID)"

# --- Install --------------------------------------------------------------
if systemctl is-active --quiet haven 2>/dev/null; then
  say "Upgrading existing Haven (your apps keep running)"
  systemctl stop haven
fi
install -m 0755 "$BIN" /usr/local/bin/haven
mkdir -p "$DATA/apps" "$DATA/appdata"
chmod 0755 "$DATA"

cat > /etc/systemd/system/haven.service <<EOF
[Unit]
Description=Haven home server manager
After=network-online.target docker.service
Wants=network-online.target docker.service

[Service]
ExecStart=/usr/local/bin/haven --addr :$PORT --data $DATA
Environment=HAVEN_PUID=$PUID
Environment=HAVEN_PGID=$PGID
Restart=always
RestartSec=3
# Haven drives Docker, which needs root.
NoNewPrivileges=true
PrivateTmp=true

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable --now haven >/dev/null

say "Waiting for Haven to start"
for _ in $(seq 1 30); do
  if curl -fsS "http://127.0.0.1:$PORT/api/auth/status" >/dev/null 2>&1; then break; fi
  sleep 1
done
curl -fsS "http://127.0.0.1:$PORT/api/auth/status" >/dev/null 2>&1 || die "Haven did not start. See: sudo journalctl -u haven -n 50"

if command -v ufw >/dev/null && ufw status 2>/dev/null | grep -q "Status: active"; then
  warn "The ufw firewall is on. To reach Haven from other devices run: sudo ufw allow $PORT/tcp"
fi

IP="$(hostname -I 2>/dev/null | awk '{print $1}')"
echo
printf '\033[1;32mHaven is running!\033[0m\n'
echo "  Open:  http://${IP:-<server-ip>}:$PORT"
if [ -f "$DATA/setup-token" ]; then
  echo "  Setup code:  $(cat "$DATA/setup-token")"
fi
echo
echo "  Logs:  sudo journalctl -u haven -f"
