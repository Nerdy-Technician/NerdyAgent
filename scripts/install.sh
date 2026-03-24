#!/usr/bin/env bash
set -euo pipefail

# NerdyAgent installer
# Usage: curl -fsSL https://raw.githubusercontent.com/OWNER/NerdyAgent/main/scripts/install.sh | \
#        NRMM_SERVER=https://your-server.com NRMM_TOKEN=your-token bash

AGENT_VERSION="${AGENT_VERSION:-latest}"
SERVER_URL="${NRMM_SERVER:-${1:-}}"
ENROLLMENT_TOKEN="${NRMM_TOKEN:-${2:-}}"
SERVICE_NAME="nerdyagent"
INSTALL_DIR="/usr/local/bin"
CONFIG_DIR="/etc/nerdyagent"
CONFIG_FILE="$CONFIG_DIR/config.json"
SYSTEMD_FILE="/etc/systemd/system/${SERVICE_NAME}.service"

# Colors
RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; NC='\033[0m'
log()  { echo -e "${GREEN}[+]${NC} $*"; }
warn() { echo -e "${YELLOW}[!]${NC} $*"; }
err()  { echo -e "${RED}[!]${NC} $*" >&2; exit 1; }

[ "$(id -u)" -eq 0 ] || err "Must be run as root (sudo)"
[ -n "$SERVER_URL" ] || err "NRMM_SERVER is required. Set it as an env var or pass as first argument."
[ -n "$ENROLLMENT_TOKEN" ] || err "NRMM_TOKEN is required. Set it as an env var or pass as second argument."

# Detect arch
ARCH=$(uname -m)
case "$ARCH" in
  x86_64)  ARCH="amd64" ;;
  aarch64) ARCH="arm64" ;;
  *) err "Unsupported architecture: $ARCH" ;;
esac

log "Installing NerdyAgent (${AGENT_VERSION}) for linux/${ARCH}"
log "Server: $SERVER_URL"

# Download binary
BINARY_URL="${SERVER_URL}/downloads/nerdyagent-linux-${ARCH}"
log "Downloading agent from $BINARY_URL"
curl -fsSL -o /tmp/nerdyagent "$BINARY_URL" || err "Failed to download agent binary from $BINARY_URL"
chmod +x /tmp/nerdyagent
mv /tmp/nerdyagent "$INSTALL_DIR/nerdyagent"
log "Agent binary installed to $INSTALL_DIR/nerdyagent"

# Write config
mkdir -p "$CONFIG_DIR"
cat > "$CONFIG_FILE" <<EOF
{
  "serverURL": "$SERVER_URL",
  "enrollmentToken": "$ENROLLMENT_TOKEN",
  "checkinEvery": "60s",
  "jobTimeoutSec": 120,
  "outputMaxBytes": 131072
}
EOF
chmod 600 "$CONFIG_FILE"
log "Config written to $CONFIG_FILE"

# Write systemd service
cat > "$SYSTEMD_FILE" <<EOF
[Unit]
Description=NerdyAgent RMM Agent
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=$INSTALL_DIR/nerdyagent
Restart=always
RestartSec=30
Environment=NRMM_AGENT_CONFIG=$CONFIG_FILE
StandardOutput=journal
StandardError=journal
SyslogIdentifier=nerdyagent

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable "$SERVICE_NAME"
systemctl restart "$SERVICE_NAME"
log "Service $SERVICE_NAME started"
log ""
log "NerdyAgent installed successfully!"
log "Check status: systemctl status $SERVICE_NAME"
log "View logs:    journalctl -u $SERVICE_NAME -f"
