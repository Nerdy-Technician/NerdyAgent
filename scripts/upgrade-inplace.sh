#!/usr/bin/env bash
set -euo pipefail

# In-place upgrade for an already-enrolled host (Asgard /opt/nerdyrmm or
# /usr/local/bin/nerdyagent). Never rewrites deviceId/token/serverUrl.
#
#   sudo AGENT_VERSION=0.4.0 ./scripts/upgrade-inplace.sh
#   sudo ./scripts/upgrade-inplace.sh /path/to/nerdyrmm-agent-linux-amd64

AGENT_VERSION="${AGENT_VERSION:-0.4.0}"
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
Comment=NerdyRMM / NerdyAgent Datto-style tray (user session)
Exec=$BIN_PATH --tray
Icon=nerdyrmm-agent
Terminal=false
Categories=System;Monitor;
StartupNotify=false
X-GNOME-Autostart-enabled=true
X-GNOME-Autostart-Delay=3
Hidden=false
EOF

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
