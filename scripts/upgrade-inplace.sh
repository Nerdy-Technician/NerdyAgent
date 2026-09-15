#!/usr/bin/env bash
set -euo pipefail

# In-place upgrade for an already-enrolled host (Asgard /opt/nerdyrmm or
# /usr/local/bin/nerdyagent). Never rewrites deviceId/token/serverUrl.
#
#   sudo AGENT_VERSION=0.3.10.2 ./scripts/upgrade-inplace.sh
#   sudo ./scripts/upgrade-inplace.sh /path/to/nerdyrmm-agent-linux-amd64

AGENT_VERSION="${AGENT_VERSION:-0.3.10.2}"
GITHUB_REPO="${NRMM_AGENT_GITHUB_REPO:-Nerdy-Technician/NerdyAgent}"
LOCAL_BIN="${1:-}"

[ "$(id -u)" -eq 0 ] || { echo "Must run as root (sudo)" >&2; exit 1; }

if [[ -x /opt/nerdyrmm/nerdyrmm-agent || -f /etc/nerdyrmm-agent/config.json ]]; then
  INSTALL_DIR="/opt/nerdyrmm"
  BINARY_NAME="nerdyrmm-agent"
  CONFIG_FILE="/etc/nerdyrmm-agent/config.json"
  SERVICE_NAME="nerdyrmm-agent"
else
  INSTALL_DIR="/usr/local/bin"
  BINARY_NAME="nerdyagent"
  CONFIG_FILE="/etc/nerdyagent/config.json"
  SERVICE_NAME="nerdyagent"
fi
BIN_PATH="$INSTALL_DIR/$BINARY_NAME"

if [[ ! -f "$CONFIG_FILE" ]]; then
  echo "No existing config at $CONFIG_FILE — this host is not enrolled. Use scripts/install.sh instead." >&2
  exit 1
fi

ARCH="$(uname -m)"
case "$ARCH" in
  x86_64) ARCH=amd64 ;;
  aarch64) ARCH=arm64 ;;
  *) echo "Unsupported arch $ARCH" >&2; exit 1 ;;
esac

TMP="/tmp/nerdyrmm-agent-linux-${ARCH}"
if [[ -n "$LOCAL_BIN" ]]; then
  cp "$LOCAL_BIN" "$TMP"
else
  TAG="v${AGENT_VERSION#v}"
  URL="https://github.com/${GITHUB_REPO}/releases/download/${TAG}/nerdyrmm-agent-linux-${ARCH}"
  SUMS="https://github.com/${GITHUB_REPO}/releases/download/${TAG}/SHA256SUMS"
  echo "Downloading $URL"
  curl -fsSL -o "$TMP" "$URL"
  if curl -fsSL -o /tmp/SHA256SUMS "$SUMS"; then
    (cd /tmp && sha256sum -c --ignore-missing SHA256SUMS)
  else
    echo "SHA256SUMS not available; continuing after ELF check only"
  fi
fi

python3 -c 'import sys; sys.exit(0 if open(sys.argv[1],"rb").read(4)==b"\x7fELF" else 1)' "$TMP" \
  || { echo "Refusing to install non-ELF download" >&2; exit 1; }

echo "Stopping $SERVICE_NAME"
systemctl stop "$SERVICE_NAME" || true
install -d -m 0755 "$INSTALL_DIR"
install -m 0755 "$TMP" "$BIN_PATH"
ln -sfn "$BIN_PATH" "$INSTALL_DIR/nerdyrmm-agent-tray" 2>/dev/null || true
"$BIN_PATH" --install-icons >/dev/null 2>&1 || true

python3 - "$CONFIG_FILE" "${AGENT_VERSION#v}" <<'PY'
import json, sys
path, ver = sys.argv[1], sys.argv[2]
with open(path) as f:
    data = json.load(f)
for required in ("deviceId", "token", "serverUrl"):
    if required not in data:
        raise SystemExit(f"refusing to continue: {required} missing from {path}")
data["agentVersion"] = ver
with open(path, "w") as f:
    json.dump(data, f, indent=2)
    f.write("\n")
PY
chmod 600 "$CONFIG_FILE"

install -d -m 0755 /etc/xdg/autostart
cat >/etc/xdg/autostart/nerdyrmm-agent-tray.desktop <<EOF
[Desktop Entry]
Type=Application
Name=NerdyRMM Agent
Comment=NerdyRMM / NerdyAgent status (user session tray)
Exec=$BIN_PATH --tray
Terminal=false
Categories=System;Monitor;
StartupNotify=false
X-GNOME-Autostart-enabled=true
X-GNOME-Autostart-Delay=3
Hidden=false
EOF

systemctl daemon-reload
systemctl enable "$SERVICE_NAME" >/dev/null
systemctl start "$SERVICE_NAME"
systemctl --no-pager --full status "$SERVICE_NAME" || true

launch_tray_user() {
  local user="$1"
  [[ -z "$user" || "$user" == "root" ]] && return
  local uid
  uid="$(id -u "$user" 2>/dev/null || true)"
  [[ -z "$uid" ]] && return
  if [[ ! -S "/run/user/${uid}/bus" ]]; then
    echo "No session bus for $user; tray will appear after login"
    return
  fi
  pkill -u "$user" -f "nerdyrmm-agent --tray|nerdyagent --tray" 2>/dev/null || true
  sudo -u "$user" env DISPLAY="${DISPLAY:-:0}" XDG_RUNTIME_DIR="/run/user/${uid}" \
    DBUS_SESSION_BUS_ADDRESS="unix:path=/run/user/${uid}/bus" \
    "$BIN_PATH" --tray >/tmp/nerdyrmm-agent-tray.log 2>&1 &
  echo "Tray launched for $user on DISPLAY=${DISPLAY:-:0}"
}

[[ -n "${SUDO_USER:-}" ]] && launch_tray_user "$SUDO_USER"
id roffo >/dev/null 2>&1 && launch_tray_user roffo

echo
echo "In-place upgrade complete."
echo "  Binary:  $BIN_PATH"
echo "  Config:  $CONFIG_FILE  (deviceId/token preserved)"
echo "  Service: $SERVICE_NAME"
echo "  Tray:    $BIN_PATH --tray  (user session / Cinnamon autostart)"
