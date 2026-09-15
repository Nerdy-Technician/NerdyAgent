#!/usr/bin/env bash
# Shared Linux extras: NR hicolor icons, tray autostart, user systemd unit,
# pkexec restart helper. Sourced by install.sh / scripts/install.sh /
# scripts/upgrade-inplace.sh (or inlined). Safe to run multiple times.
#
# Expected env: BIN_PATH (absolute agent binary), optional SKIP_TRAY=1

install_nerdyrmm_tray_extras() {
  local bin="${1:-${BIN_PATH:-}}"
  local skip="${SKIP_TRAY:-${NRMM_SKIP_TRAY:-0}}"
  [[ "$skip" == "1" ]] && return 0
  [[ -n "$bin" ]] || return 0

  install -d -m 0755 /usr/share/icons/hicolor
  local sz src dst
  for sz in 16 22 24 32 48 64 128 256; do
    src=""
    for cand in \
      "packaging/icons/hicolor/${sz}x${sz}/apps/nerdyrmm-agent.png" \
      "${0%/*}/../packaging/icons/hicolor/${sz}x${sz}/apps/nerdyrmm-agent.png" \
      "/usr/share/nerdyrmm/icons/hicolor/${sz}x${sz}/apps/nerdyrmm-agent.png"
    do
      if [[ -f "$cand" ]]; then src="$cand"; break; fi
    done
    dst="/usr/share/icons/hicolor/${sz}x${sz}/apps/nerdyrmm-agent.png"
    if [[ -n "$src" ]]; then
      install -D -m 0644 "$src" "$dst"
    elif command -v python3 >/dev/null 2>&1 && [[ -x "$bin" ]]; then
      # Last-resort: copy whatever the running tray would write; skip if missing.
      true
    fi
  done
  if command -v gtk-update-icon-cache >/dev/null 2>&1; then
    gtk-update-icon-cache -f /usr/share/icons/hicolor >/dev/null 2>&1 || true
  fi

  install -d -m 0755 /etc/xdg/autostart
  cat >/etc/xdg/autostart/nerdyrmm-agent-tray.desktop <<EOF
[Desktop Entry]
Type=Application
Name=NerdyRMM Agent
Comment=NerdyRMM / NerdyAgent Datto-style tray (user session)
Exec=$bin --tray
Icon=nerdyrmm-agent
Terminal=false
Categories=System;Monitor;
StartupNotify=false
X-GNOME-Autostart-enabled=true
X-GNOME-Autostart-Delay=3
Hidden=false
EOF

  install -d -m 0755 /usr/lib/systemd/user
  cat >/usr/lib/systemd/user/nerdyrmm-agent-tray.service <<EOF
[Unit]
Description=NerdyRMM Agent tray (user session)
PartOf=graphical-session.target
After=graphical-session.target

[Service]
Type=simple
ExecStart=$bin --tray
Restart=on-failure
RestartSec=3

[Install]
WantedBy=graphical-session.target
EOF

  install -d -m 0755 /usr/libexec/nerdyrmm
  cat >/usr/libexec/nerdyrmm/nerdyrmm-agent-restart <<'EOF'
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
EOF
  chmod 0755 /usr/libexec/nerdyrmm/nerdyrmm-agent-restart

  if [[ -f /usr/share/polkit-1/actions/org.nerdyrmm.agent.policy ]] || [[ -d /usr/share/polkit-1/actions ]]; then
    install -d -m 0755 /usr/share/polkit-1/actions
    if [[ -f packaging/org.nerdyrmm.agent.policy ]]; then
      install -m 0644 packaging/org.nerdyrmm.agent.policy /usr/share/polkit-1/actions/org.nerdyrmm.agent.policy
    elif [[ -f "${0%/*}/../packaging/org.nerdyrmm.agent.policy" ]]; then
      install -m 0644 "${0%/*}/../packaging/org.nerdyrmm.agent.policy" /usr/share/polkit-1/actions/org.nerdyrmm.agent.policy
    fi
  fi
}
