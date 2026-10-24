#!/usr/bin/env bash
# Removes the Haven program. Your apps keep running and their files stay in
# /opt/haven, so you can keep managing them with `docker compose`.
set -euo pipefail
[ "$(id -u)" -eq 0 ] || { echo "please run with sudo"; exit 1; }
systemctl disable --now haven 2>/dev/null || true
rm -f /etc/systemd/system/haven.service /usr/local/bin/haven
systemctl daemon-reload
echo "Haven removed. Apps and data are untouched in /opt/haven."
echo "To delete everything (apps and their data), remove each app in Haven first, or run:"
echo "  for d in /opt/haven/apps/*/; do (cd \"\$d\" && docker compose -p haven-\$(basename \"\$d\") down); done && sudo rm -rf /opt/haven"
