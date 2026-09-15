#!/usr/bin/env bash
set -euo pipefail

# NerdyAgent installer — agent systemd unit + Linux tray autostart.
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/Nerdy-Technician/NerdyAgent/main/scripts/install.sh | \
#     NRMM_SERVER=https://your-server.com NRMM_TOKEN=your-token bash
#
# Existing hosts (Asgard /opt/nerdyrmm or /usr/local/bin) are detected and
# updated in place. deviceId/token/serverUrl are never overwritten.

AGENT_VERSION="${AGENT_VERSION:-latest}"
SERVER_URL="${NRMM_SERVER:-${1:-}}"
ENROLLMENT_TOKEN="${NRMM_TOKEN:-${2:-}}"
GITHUB_REPO="${NRMM_AGENT_GITHUB_REPO:-Nerdy-Technician/NerdyAgent}"
LAYOUT="${NRMM_LAYOUT:-auto}"   # auto | opt | usr
SKIP_TRAY="${NRMM_SKIP_TRAY:-0}"

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; NC='\033[0m'
log()  { echo -e "${GREEN}[+]${NC} $*"; }
warn() { echo -e "${YELLOW}[!]${NC} $*"; }
err()  { echo -e "${RED}[!]${NC} $*" >&2; exit 1; }

[ "$(id -u)" -eq 0 ] || err "Must be run as root (sudo)"

detect_layout() {
  if [[ "$LAYOUT" == "opt" ]]; then
    INSTALL_DIR="/opt/nerdyrmm"
    BINARY_NAME="nerdyrmm-agent"
    CONFIG_DIR="/etc/nerdyrmm-agent"
    SERVICE_NAME="nerdyrmm-agent"
    return
  fi
  if [[ "$LAYOUT" == "usr" ]]; then
    INSTALL_DIR="/usr/local/bin"
    BINARY_NAME="nerdyagent"
    CONFIG_DIR="/etc/nerdyagent"
    SERVICE_NAME="nerdyagent"
    return
  fi
  if [[ -x /opt/nerdyrmm/nerdyrmm-agent || -f /etc/nerdyrmm-agent/config.json ]] || \
     systemctl cat nerdyrmm-agent.service >/dev/null 2>&1; then
    INSTALL_DIR="/opt/nerdyrmm"
    BINARY_NAME="nerdyrmm-agent"
    CONFIG_DIR="/etc/nerdyrmm-agent"
    SERVICE_NAME="nerdyrmm-agent"
    return
  fi
  INSTALL_DIR="/usr/local/bin"
  BINARY_NAME="nerdyagent"
  CONFIG_DIR="/etc/nerdyagent"
  SERVICE_NAME="nerdyagent"
}

detect_layout
CONFIG_FILE="$CONFIG_DIR/config.json"
SYSTEMD_FILE="/etc/systemd/system/${SERVICE_NAME}.service"
BIN_PATH="$INSTALL_DIR/$BINARY_NAME"

if [[ ! -f "$CONFIG_FILE" ]]; then
  [ -n "$SERVER_URL" ] || err "NRMM_SERVER is required for a new install (or pass as first argument)."
  [ -n "$ENROLLMENT_TOKEN" ] || err "NRMM_TOKEN is required for a new install (or pass as second argument)."
else
  log "Preserving enrolled config at $CONFIG_FILE (deviceId/token untouched)"
  if [[ -z "$SERVER_URL" ]] && command -v python3 >/dev/null 2>&1; then
    SERVER_URL="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1])).get("serverUrl",""))' "$CONFIG_FILE" 2>/dev/null || true)"
  fi
fi

ARCH="$(uname -m)"
case "$ARCH" in
  x86_64)  ARCH="amd64" ;;
  aarch64) ARCH="arm64" ;;
  *) err "Unsupported architecture: $ARCH" ;;
esac

ASSET="nerdyrmm-agent-linux-${ARCH}"
log "Installing NerdyAgent (${AGENT_VERSION}) linux/${ARCH} into $BIN_PATH"
log "Service: $SERVICE_NAME"

resolve_github_tag() {
  if [[ "$AGENT_VERSION" != "latest" ]]; then
    echo "v${AGENT_VERSION#v}"
    return
  fi
  local tag=""
  tag="$(curl -fsSL -H 'Accept: application/vnd.github+json' \
    "https://api.github.com/repos/${GITHUB_REPO}/releases/latest" \
    | python3 -c 'import json,sys; print(json.load(sys.stdin).get("tag_name",""))' 2>/dev/null || true)"
  if [[ -n "$tag" ]]; then
    echo "$tag"
    return
  fi
  echo ""
}

download_agent() {
  local tmp="/tmp/${ASSET}"
  local tag gh_url srv_url
  tag="$(resolve_github_tag)"
  if [[ -n "$tag" ]]; then
    gh_url="https://github.com/${GITHUB_REPO}/releases/download/${tag}/${ASSET}"
    log "Downloading $gh_url"
    if curl -fsSL -o "$tmp" "$gh_url"; then
      echo "$tmp"
      return
    fi
    warn "GitHub download failed, trying server /downloads/"
  fi
  [ -n "$SERVER_URL" ] || err "No GitHub asset and NRMM_SERVER is empty"
  srv_url="${SERVER_URL%/}/downloads/${ASSET}"
  log "Downloading $srv_url"
  curl -fsSL -o "$tmp" "$srv_url" || err "Failed to download agent binary"
  echo "$tmp"
}

TMP_BIN="$(download_agent)"
chmod +x "$TMP_BIN"
if ! python3 -c 'import sys; sys.exit(0 if open(sys.argv[1],"rb").read(4)==b"\x7fELF" else 1)' "$TMP_BIN"; then
  err "Downloaded file is not an ELF binary; aborting"
fi

install -d -m 0755 "$INSTALL_DIR"
if systemctl is-active --quiet "$SERVICE_NAME" 2>/dev/null; then
  systemctl stop "$SERVICE_NAME" || true
fi
install -m 0755 "$TMP_BIN" "$BIN_PATH"
ln -sfn "$BIN_PATH" "$INSTALL_DIR/nerdyrmm-agent-tray" 2>/dev/null || true
log "Agent binary installed to $BIN_PATH"

install -d -m 0755 "$CONFIG_DIR"
if [[ ! -f "$CONFIG_FILE" ]]; then
  cat > "$CONFIG_FILE" <<EOF
{
  "serverUrl": "$SERVER_URL",
  "enrollmentToken": "$ENROLLMENT_TOKEN",
  "checkinEvery": "60s",
  "agentVersion": "${AGENT_VERSION#v}",
  "jobTimeoutSec": 120,
  "outputMaxBytes": 131072
}
EOF
  chmod 600 "$CONFIG_FILE"
  log "Config written to $CONFIG_FILE"
else
  if command -v python3 >/dev/null 2>&1 && [[ "$AGENT_VERSION" != "latest" ]]; then
    python3 - "$CONFIG_FILE" "${AGENT_VERSION#v}" <<'PY'
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
    log "Bumped agentVersion to ${AGENT_VERSION#v} (auth fields preserved)"
  fi
fi

cat > "$SYSTEMD_FILE" <<EOF
[Unit]
Description=NerdyAgent RMM Agent
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=$BIN_PATH
Restart=always
RestartSec=5
Environment=NRMM_AGENT_CONFIG=$CONFIG_FILE
StandardOutput=journal
StandardError=journal
SyslogIdentifier=$SERVICE_NAME

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable "$SERVICE_NAME" >/dev/null
systemctl restart "$SERVICE_NAME"
log "Service $SERVICE_NAME started"

install_tray() {
  [[ "$SKIP_TRAY" == "1" ]] && return
  local desktop_dst exec_line
  desktop_dst="/etc/xdg/autostart/nerdyrmm-agent-tray.desktop"
  install -d -m 0755 /etc/xdg/autostart
  exec_line="$BIN_PATH --tray"
  cat > "$desktop_dst" <<EOF
[Desktop Entry]
Type=Application
Name=NerdyRMM Agent
Comment=NerdyRMM / NerdyAgent Datto-style tray (user session)
Exec=$exec_line
Icon=nerdyrmm-agent
Terminal=false
Categories=System;Monitor;
StartupNotify=false
X-GNOME-Autostart-enabled=true
X-GNOME-Autostart-Delay=3
Hidden=false
EOF
  log "Tray autostart installed at $desktop_dst"

  "$BIN_PATH" --install-icons /usr/share/icons/hicolor >/dev/null 2>&1 || true
  if command -v gtk-update-icon-cache >/dev/null 2>&1; then
    gtk-update-icon-cache -f /usr/share/icons/hicolor >/dev/null 2>&1 || true
  fi

  install -d -m 0755 /usr/lib/systemd/user
  cat >/usr/lib/systemd/user/nerdyrmm-agent-tray.service <<EOF
[Unit]
Description=NerdyRMM Agent tray (user session)
PartOf=graphical-session.target
After=graphical-session.target

[Service]
Type=simple
ExecStart=$BIN_PATH --tray
Restart=on-failure
RestartSec=3

[Install]
WantedBy=graphical-session.target
EOF
  log "User systemd unit template: nerdyrmm-agent-tray.service"

  install -d -m 0755 /usr/libexec/nerdyrmm
  cat >/usr/libexec/nerdyrmm/nerdyrmm-agent-restart <<'RESTART'
#!/bin/sh
set -eu
for unit in nerdyrmm-agent.service nerdyagent.service; do
  state="$(systemctl show -p LoadState --value "$unit" 2>/dev/null || true)"
  if [ "$state" = "loaded" ]; then
    systemctl restart "$unit"
    echo "restarted $unit"
    exit 0
  fi
done
echo "No nerdyrmm-agent.service or nerdyagent.service is installed." >&2
exit 1
RESTART
  chmod 0755 /usr/libexec/nerdyrmm/nerdyrmm-agent-restart
  if [[ -d /usr/share/polkit-1/actions ]]; then
    cat >/usr/share/polkit-1/actions/org.nerdyrmm.agent.policy <<'PK'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE policyconfig PUBLIC "-//freedesktop//DTD PolicyKit Policy Configuration 1.0//EN"
 "http://www.freedesktop.org/standards/PolicyKit/1/policyconfig.dtd">
<policyconfig>
  <vendor>Nerdy Technician</vendor>
  <action id="org.nerdyrmm.agent.restart">
    <description>Restart the NerdyRMM agent service</description>
    <message>Authentication is required to restart the NerdyRMM agent</message>
    <defaults>
      <allow_any>auth_admin</allow_any>
      <allow_inactive>auth_admin</allow_inactive>
      <allow_active>auth_admin_keep</allow_active>
    </defaults>
    <annotate key="org.freedesktop.policykit.exec.path">/usr/libexec/nerdyrmm/nerdyrmm-agent-restart</annotate>
    <annotate key="org.freedesktop.policykit.exec.allow_gui">true</annotate>
  </action>
</policyconfig>
PK
  fi

  launch_for_user() {
    local user="$1"
    [[ -z "$user" || "$user" == "root" ]] && return
    local uid home display bus
    uid="$(id -u "$user" 2>/dev/null || true)"
    [[ -z "$uid" ]] && return
    home="$(getent passwd "$user" | cut -d: -f6)"
    display="${DISPLAY:-:0}"
    bus="unix:path=/run/user/${uid}/bus"
    if [[ ! -S "/run/user/${uid}/bus" ]]; then
      warn "No session bus for $user; tray will start at next graphical login"
      if [[ -n "$home" ]]; then
        install -d -m 0755 -o "$user" -g "$user" "$home/.config/autostart"
        cp "$desktop_dst" "$home/.config/autostart/nerdyrmm-agent-tray.desktop"
        chown "$user:$user" "$home/.config/autostart/nerdyrmm-agent-tray.desktop"
      fi
      return
    fi
    pkill -u "$user" -f "nerdyrmm-agent --tray|nerdyagent --tray" 2>/dev/null || true
    sudo -u "$user" env DISPLAY="$display" WAYLAND_DISPLAY="${WAYLAND_DISPLAY:-}" \
      XDG_RUNTIME_DIR="/run/user/${uid}" DBUS_SESSION_BUS_ADDRESS="$bus" \
      "$BIN_PATH" --tray >/tmp/nerdyrmm-agent-tray.log 2>&1 &
    log "Tray launched for $user on DISPLAY=$display"
  }

  if [[ -n "${SUDO_USER:-}" ]]; then
    launch_for_user "$SUDO_USER"
  fi
  if command -v loginctl >/dev/null 2>&1; then
    loginctl list-sessions --no-legend 2>/dev/null | awk '{print $3}' | sort -u | while read -r u; do
      [[ "$u" == "root" || -z "$u" ]] && continue
      launch_for_user "$u"
    done
  fi
}

install_tray

log ""
log "NerdyAgent installed successfully."
log "  Agent:  $BIN_PATH"
log "  Config: $CONFIG_FILE"
log "  Unit:   $SERVICE_NAME"
log "  Tray:   starts in the user graphical session (not as root MainPID)"
log "Check:    systemctl status $SERVICE_NAME"
log "Logs:     journalctl -u $SERVICE_NAME -f"
