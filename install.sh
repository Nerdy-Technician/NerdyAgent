#!/usr/bin/env bash
set -euo pipefail

SERVER_URL="${1:-http://localhost:8080}"
DEVICE_ID="${2:-}"
TOKEN="${3:-}"
ENROLLMENT_TOKEN="${4:-}"

if [[ -z "$ENROLLMENT_TOKEN" && ( -z "$DEVICE_ID" || -z "$TOKEN" ) ]]; then
  echo "Usage (recommended): $0 <server_url> '' '' <enrollment_token>"
  echo "Legacy usage: $0 <server_url> <device_id> <token>"
  exit 1
fi

if [[ $EUID -ne 0 ]]; then
  echo "This installer must be run as root (sudo)." >&2
  exit 1
fi

has_systemctl=0
has_service=0
has_rc=0
if command -v systemctl >/dev/null 2>&1; then has_systemctl=1; fi
if command -v service >/dev/null 2>&1; then has_service=1; fi
if command -v rc-service >/dev/null 2>&1; then has_rc=1; fi

# Stop any existing agent before replacing files to avoid running old bits.
if [[ $has_systemctl -eq 1 ]]; then
  systemctl stop nerdyrmm-agent 2>/dev/null || true
  systemctl reset-failed nerdyrmm-agent 2>/dev/null || true
elif [[ $has_service -eq 1 ]]; then
  service nerdyrmm-agent stop 2>/dev/null || true
elif [[ $has_rc -eq 1 ]]; then
  rc-service nerdyrmm-agent stop 2>/dev/null || true
fi
pkill -f "/opt/nerdyrmm/nerdyrmm-agent" 2>/dev/null || true

install -d -m 0755 /etc/nerdyrmm-agent
cat >/etc/nerdyrmm-agent/config.json <<JSON
{
  "serverUrl": "${SERVER_URL}",
  "deviceId": ${DEVICE_ID:-0},
  "token": "${TOKEN}",
  "enrollmentToken": "${ENROLLMENT_TOKEN}",
  "checkinEvery": 30000000000,
  "agentVersion": "0.3.6",
  "jobTimeoutSec": 120,
  "outputMaxBytes": 131072
}
JSON

install -d -m 0755 /opt/nerdyrmm
install -m 0755 nerdyrmm-agent /opt/nerdyrmm/nerdyrmm-agent

cat >/etc/systemd/system/nerdyrmm-agent.service <<SERVICE
[Unit]
Description=NerdyRMM Agent
After=network.target

[Service]
Type=simple
ExecStart=/opt/nerdyrmm/nerdyrmm-agent
Restart=always
RestartSec=5

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
  if ! service nerdyrmm-agent restart 2>/dev/null; then
    service nerdyrmm-agent start 2>/dev/null || true
  fi
elif [[ $has_rc -eq 1 ]]; then
  if ! rc-service nerdyrmm-agent restart 2>/dev/null; then
    rc-service nerdyrmm-agent start 2>/dev/null || true
  fi
else
  # Fallback for non-systemd environments: run in background (best-effort).
  nohup /opt/nerdyrmm/nerdyrmm-agent >/var/log/nerdyrmm-agent.log 2>&1 &
fi

# Always restart the systemd service at the very end, even if it was already started.
systemctl restart nerdyrmm-agent 2>/dev/null || true

if [[ -n "$ENROLLMENT_TOKEN" ]]; then
  echo "NerdyRMM agent installed. Device appears after first registration check-in."
  systemctl restart nerdyrmm-agent 2>/dev/null || true
else
  echo "NerdyRMM agent installed and restarted."
  systemctl restart nerdyrmm-agent 2>/dev/null || true
fi
