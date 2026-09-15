#!/usr/bin/env bash
set -euo pipefail

# Legacy / Asgard installer. Installs to /opt/nerdyrmm + /etc/nerdyrmm-agent
# without wiping an already-enrolled deviceId/token.
#
# Fresh enroll:  sudo ./install.sh https://rmm-api.example.com '' '' ENROLL_TOKEN
# In-place:      sudo ./install.sh            # keeps config, replaces local binary
# Or set NRMM_SERVER / NRMM_TOKEN.

SERVER_URL="${1:-${NRMM_SERVER:-}}"
DEVICE_ID="${2:-}"
TOKEN="${3:-}"
ENROLLMENT_TOKEN="${4:-${NRMM_TOKEN:-}}"
GITHUB_REPO="${NRMM_AGENT_GITHUB_REPO:-Nerdy-Technician/NerdyAgent}"
AGENT_VERSION="${AGENT_VERSION:-0.3.10.1}"

if [[ $EUID -ne 0 ]]; then
  echo "This installer must be run as root (sudo)." >&2
  exit 1
fi

CONFIG_DIR="/etc/nerdyrmm-agent"
CONFIG_FILE="$CONFIG_DIR/config.json"
INSTALL_DIR="/opt/nerdyrmm"
BIN_PATH="$INSTALL_DIR/nerdyrmm-agent"
SERVICE_NAME="nerdyrmm-agent"

has_systemctl=0
has_service=0
has_rc=0
if command -v systemctl >/dev/null 2>&1; then has_systemctl=1; fi
if command -v service >/dev/null 2>&1; then has_service=1; fi
if command -v rc-service >/dev/null 2>&1; then has_rc=1; fi

if [[ $has_systemctl -eq 1 ]]; then
  systemctl stop nerdyrmm-agent 2>/dev/null || true
  systemctl reset-failed nerdyrmm-agent 2>/dev/null || true
elif [[ $has_service -eq 1 ]]; then
  service nerdyrmm-agent stop 2>/dev/null || true
elif [[ $has_rc -eq 1 ]]; then
  rc-service nerdyrmm-agent stop 2>/dev/null || true
fi
pkill -f "/opt/nerdyrmm/nerdyrmm-agent" 2>/dev/null || true

install -d -m 0755 "$CONFIG_DIR"
if [[ -f "$CONFIG_FILE" ]]; then
  echo "Preserving existing $CONFIG_FILE (deviceId/token/serverUrl untouched)"
  if command -v python3 >/dev/null 2>&1; then
    python3 - "$CONFIG_FILE" "$AGENT_VERSION" <<'PY'
import json, sys
path, ver = sys.argv[1], sys.argv[2]
with open(path) as f:
    data = json.load(f)
data["agentVersion"] = ver
with open(path, "w") as f:
    json.dump(data, f, indent=2)
    f.write("\n")
PY
    chmod 600 "$CONFIG_FILE"
  fi
else
  if [[ -z "$ENROLLMENT_TOKEN" && ( -z "$DEVICE_ID" || -z "$TOKEN" ) ]]; then
    echo "Usage (recommended): $0 <server_url> '' '' <enrollment_token>"
    echo "Legacy usage: $0 <server_url> <device_id> <token>"
    echo "In-place upgrade: run from a directory that contains nerdyrmm-agent after a previous enroll."
    exit 1
  fi
  [[ -n "$SERVER_URL" ]] || { echo "server_url required for a new install" >&2; exit 1; }
  cat >"$CONFIG_FILE" <<JSON
{
  "serverUrl": "${SERVER_URL}",
  "deviceId": ${DEVICE_ID:-0},
  "token": "${TOKEN}",
  "enrollmentToken": "${ENROLLMENT_TOKEN}",
  "checkinEvery": 30000000000,
  "agentVersion": "${AGENT_VERSION}",
  "jobTimeoutSec": 120,
  "outputMaxBytes": 131072
}
JSON
  chmod 600 "$CONFIG_FILE"
fi

install -d -m 0755 "$INSTALL_DIR"
if [[ -x ./nerdyrmm-agent ]]; then
  SRC="./nerdyrmm-agent"
elif [[ -x ./nerdyrmm-agent-linux-amd64 ]]; then
  SRC="./nerdyrmm-agent-linux-amd64"
else
  ARCH="$(uname -m)"
  case "$ARCH" in
    x86_64) ARCH=amd64 ;;
    aarch64) ARCH=arm64 ;;
    *) echo "Unsupported arch $ARCH and no local nerdyrmm-agent binary" >&2; exit 1 ;;
  esac
  TAG="v${AGENT_VERSION#v}"
  URL="https://github.com/${GITHUB_REPO}/releases/download/${TAG}/nerdyrmm-agent-linux-${ARCH}"
  echo "Downloading $URL"
  curl -fsSL -o /tmp/nerdyrmm-agent-linux-${ARCH} "$URL"
  SRC="/tmp/nerdyrmm-agent-linux-${ARCH}"
fi
install -m 0755 "$SRC" "$BIN_PATH"
ln -sfn "$BIN_PATH" "$INSTALL_DIR/nerdyrmm-agent-tray" 2>/dev/null || true

cat >/etc/systemd/system/nerdyrmm-agent.service <<SERVICE
[Unit]
Description=NerdyRMM Agent
After=network.target

[Service]
Type=simple
ExecStart=$BIN_PATH
Restart=always
RestartSec=5
Environment=NRMM_AGENT_CONFIG=$CONFIG_FILE

[Install]
WantedBy=multi-user.target
SERVICE

if [[ $has_systemctl -eq 1 ]]; then
  systemctl daemon-reload
  systemctl enable nerdyrmm-agent >/dev/null 2>&1
  if ! systemctl restart nerdyrmm-agent; then
    systemctl status --no-pager -l nerdyrmm-agent || true
    echo "ERROR: failed to start nerdyrmm-agent service." >&2
    exit 1
  fi
  if ! systemctl is-active --quiet nerdyrmm-agent; then
    systemctl status --no-pager -l nerdyrmm-agent || true
    echo "ERROR: nerdyrmm-agent is not active after restart." >&2
    exit 1
  fi
elif [[ $has_service -eq 1 ]]; then
  service nerdyrmm-agent restart 2>/dev/null || service nerdyrmm-agent start 2>/dev/null || true
elif [[ $has_rc -eq 1 ]]; then
  rc-service nerdyrmm-agent restart 2>/dev/null || rc-service nerdyrmm-agent start 2>/dev/null || true
else
  nohup "$BIN_PATH" >/var/log/nerdyrmm-agent.log 2>&1 &
fi

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

launch_tray_user() {
  local user="$1"
  [[ -z "$user" || "$user" == "root" ]] && return
  local uid
  uid="$(id -u "$user" 2>/dev/null || true)"
  [[ -z "$uid" || ! -S "/run/user/${uid}/bus" ]] && return
  pkill -u "$user" -f "nerdyrmm-agent --tray" 2>/dev/null || true
  sudo -u "$user" env DISPLAY="${DISPLAY:-:0}" XDG_RUNTIME_DIR="/run/user/${uid}" \
    DBUS_SESSION_BUS_ADDRESS="unix:path=/run/user/${uid}/bus" \
    "$BIN_PATH" --tray >/tmp/nerdyrmm-agent-tray.log 2>&1 &
  echo "Tray launched for $user"
}

if [[ -n "${SUDO_USER:-}" ]]; then
  launch_tray_user "$SUDO_USER"
fi
# Asgard default interactive user when present.
if id roffo >/dev/null 2>&1; then
  launch_tray_user roffo
fi

if [[ -n "$ENROLLMENT_TOKEN" && ! -s "$CONFIG_FILE" ]]; then
  echo "NerdyRMM agent installed. Device appears after first registration check-in."
else
  echo "NerdyRMM agent installed/updated. Tray starts at next graphical login (or immediately if a session bus was found)."
fi
