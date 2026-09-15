# NerdyAgent

NerdyAgent is the standalone, publicly distributable RMM agent for [NerdyRMM](https://github.com/Nerdy-Technician/NerdyRMM). Install it on any Linux or Windows machine to connect it to your NerdyRMM server for remote monitoring and management.

## Quick Install

### Linux (one-liner)

```bash
curl -fsSL https://raw.githubusercontent.com/Nerdy-Technician/NerdyAgent/main/scripts/install.sh | \
  NRMM_SERVER=https://your-server.com NRMM_TOKEN=your-enrollment-token bash
```

Requires root. Detects an existing install and **keeps `deviceId` / `token` / `serverUrl`**. Fresh installs go to `/usr/local/bin/nerdyagent` + `/etc/nerdyagent/config.json` + `nerdyagent.service`. Hosts that already use the Asgard / legacy layout (`/opt/nerdyrmm/nerdyrmm-agent`, `/etc/nerdyrmm-agent/config.json`, `nerdyrmm-agent.service`) stay on that layout. Force with `NRMM_LAYOUT=opt` or `NRMM_LAYOUT=usr`.

The installer also drops a user autostart entry so the **Linux tray icon** appears after graphical login (Cinnamon / AppIndicator / StatusNotifier). The tray runs as the desktop user, not as the root systemd MainPID.

### Windows (PowerShell)

```powershell
.\scripts\install.ps1 -ServerURL https://your-server.com -Token your-enrollment-token
```

Run as Administrator. Installs to `%ProgramFiles%\NerdyAgent\`, config to `%ProgramData%\NerdyAgent\config.json`, and registers a Windows Service.

## Manual Install

1. Download the binary for your platform from the [Releases](../../releases) page, or build from source (see below).

2. Create the config directory and write `config.json`:

   **Linux:**
   ```bash
   sudo mkdir -p /etc/nerdyagent
   sudo tee /etc/nerdyagent/config.json <<EOF
   {
     "serverUrl": "https://your-server.com",
     "enrollmentToken": "your-enrollment-token",
     "checkinEvery": "60s",
     "jobTimeoutSec": 120,
     "outputMaxBytes": 131072
   }
   EOF
   sudo chmod 600 /etc/nerdyagent/config.json
   ```

   **Windows:** Create `C:\ProgramData\NerdyAgent\config.json` with the same content.

3. Place the binary at `/usr/local/bin/nerdyagent` (Linux README layout), `/opt/nerdyrmm/nerdyrmm-agent` (Asgard / legacy), or `C:\Program Files\NerdyAgent\nerdyagent.exe` (Windows).

4. Install the service (see Service Management below).

## Configuration Reference

The agent reads its configuration from `config.json`. All fields are optional except `serverUrl` and one of `enrollmentToken` (first-time enrollment) or `deviceId`+`token` (already-registered device).

| Field | Type | Default | Description |
|---|---|---|---|
| `serverUrl` | string | `http://localhost:8080` | URL of your NerdyRMM server |
| `enrollmentToken` | string | — | One-time token used to register a new device. Cleared after registration. |
| `deviceId` | int | `0` | Device ID assigned by the server after registration. Set automatically. |
| `token` | string | — | Per-device auth token assigned by the server after registration. Set automatically. |
| `checkinEvery` | duration | `30s` | How often the agent checks in with the server (e.g. `"60s"`, `"5m"`). |
| `agentVersion` | string | `0.3.10` | Reported agent version. Bumped automatically on a successful self-update. |
| `jobTimeoutSec` | int | `120` | Maximum seconds a single job (command/script) may run before being killed. |
| `outputMaxBytes` | int | `131072` | Maximum bytes of output captured per job (128 KB). Excess is truncated. |

The config file path can be overridden via the `NRMM_AGENT_CONFIG` environment variable. If unset, the agent uses the first file that exists:

- `/etc/nerdyrmm-agent/config.json` (Asgard / legacy default in the binary)
- `/etc/nerdyagent/config.json` (README / `scripts/install.sh` default for new hosts)
- Windows: `%ProgramData%\NerdyRMM\config.json`, then `%ProgramData%\NerdyAgent\config.json`

Do not put the device token in any tray or status file. The agent writes a secret-free `status.json` next to `config.json` for the tray.

## Building from Source

Requirements: Go 1.24+

```bash
git clone https://github.com/Nerdy-Technician/NerdyAgent.git
cd NerdyAgent
go build -o nerdyrmm-agent ./cmd/agent
./nerdyrmm-agent --help
```

The same binary is the agent **and** the Linux tray (`--tray`). No GTK/CGO build dependency: the tray talks StatusNotifierItem / AppIndicator over the user session D-Bus (`github.com/godbus/dbus`). Cinnamon on Ubuntu is the tested target.

Or use Make for cross-platform builds:

```bash
make test           # unit tests
make build          # current platform
make build-linux    # linux amd64 + arm64 + armv7 (+ tray-named copies)
make build-windows  # windows amd64 + arm64
make build-darwin   # macOS amd64 + arm64
make build-all      # all of the above + dist/SHA256SUMS
make clean          # remove build artifacts
```

## Service Management

### Linux (systemd)

```bash
# README layout
systemctl status nerdyagent
journalctl -u nerdyagent -f

# Asgard / legacy layout
systemctl status nerdyrmm-agent
journalctl -u nerdyrmm-agent -f
```

### Windows Service

```powershell
# Check status
Get-Service NerdyAgent

# View recent logs
Get-EventLog -LogName Application -Source NerdyAgent -Newest 50

# Restart
Restart-Service NerdyAgent

# Stop / remove
Stop-Service NerdyAgent
sc.exe delete NerdyAgent
```

## Troubleshooting

## Self-update

The agent polls **the NerdyRMM server first** (`Authorization: Bearer <device token>` on `/api/agent/latest-version`, then `/api/agent/update`, `/api/agent/version`, and public `/downloads/agent-version.txt`), then **GitHub Releases** (`NRMM_AGENT_GITHUB_REPO`, default `Nerdy-Technician/NerdyAgent`). It installs the **newest** of those sources only when it is newer than `config.agentVersion`.

A successful update:

1. Downloads the matching `nerdyrmm-agent-linux-amd64` (or current OS/arch) asset
2. Verifies SHA-256 when the server, GitHub `digest`, or `SHA256SUMS` provides one
3. Rejects HTML/error pages and non-executables (will not replace the live binary)
4. Replaces the binary atomically (`rename` + `.bak` rollback on failure)
5. Writes **only** `agentVersion` in `config.json` — `deviceId`, `token`, and `serverUrl` stay put
6. Restarts the detected systemd unit (`nerdyrmm-agent` or `nerdyagent`)

One-shot (after 0.3.10 is already installed):

```bash
sudo /opt/nerdyrmm/nerdyrmm-agent --self-update
# or
sudo /usr/local/bin/nerdyagent --self-update
```

## Linux tray icon

`nerdyrmm-agent --tray` (or the `nerdyrmm-agent-tray-*` release asset, same binary) shows a StatusNotifier / AppIndicator icon in the **user** graphical session.

- Reads `/etc/nerdyrmm-agent/status.json` or `/etc/nerdyagent/status.json` (no token)
- Tooltip / click: running + last check-in + server URL
- Menu: open docs, open status file, quit tray (does **not** stop the agent service)
- Autostart: `/etc/xdg/autostart/nerdyrmm-agent-tray.desktop` (installed by `scripts/install.sh` and `install.sh`)

On Ubuntu Cinnamon (Asgard: `DISPLAY=:0`, user `roffo`) the tray must **not** be the systemd MainPID. Log out/in once after install, or start it immediately:

```bash
sudo -u roffo env DISPLAY=:0 XDG_RUNTIME_DIR=/run/user/$(id -u roffo) \
  DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/$(id -u roffo)/bus \
  /opt/nerdyrmm/nerdyrmm-agent --tray &
```

**Deps:** session D-Bus and a StatusNotifier host (Cinnamon, GNOME AppIndicator extension, KDE). No extra GTK packages to *build* the agent. `notify-send` and `xdg-open` are optional for click actions.

Windows tray is not required.

## Asgard: replace 0.3.9.5 without re-enrollment

Live layout on Asgard:

| Piece | Path |
|---|---|
| Binary | `/opt/nerdyrmm/nerdyrmm-agent` |
| Config | `/etc/nerdyrmm-agent/config.json` |
| Unit | `nerdyrmm-agent.service` |
| Server | `https://rmm-api.nerdytech.dev` |

After this version is published as GitHub release `v0.3.10`:

```bash
sudo AGENT_VERSION=0.3.10 bash -c '
  curl -fsSL -o /tmp/upgrade-inplace.sh \
    https://raw.githubusercontent.com/Nerdy-Technician/NerdyAgent/main/scripts/upgrade-inplace.sh
  bash /tmp/upgrade-inplace.sh
'
```

Manual equivalent (do **not** rewrite config.json except `agentVersion`):

```bash
TAG=v0.3.10
curl -fsSL -o /tmp/nerdyrmm-agent-linux-amd64 \
  https://github.com/Nerdy-Technician/NerdyAgent/releases/download/${TAG}/nerdyrmm-agent-linux-amd64
curl -fsSL -o /tmp/SHA256SUMS \
  https://github.com/Nerdy-Technician/NerdyAgent/releases/download/${TAG}/SHA256SUMS
(cd /tmp && sha256sum -c --ignore-missing SHA256SUMS)

sudo systemctl stop nerdyrmm-agent
sudo install -m 0755 /tmp/nerdyrmm-agent-linux-amd64 /opt/nerdyrmm/nerdyrmm-agent

sudo python3 - <<'PY'
import json
path = "/etc/nerdyrmm-agent/config.json"
with open(path) as f:
    cfg = json.load(f)
assert cfg.get("deviceId"), "deviceId missing — abort"
assert cfg.get("token"), "token missing — abort"
cfg["agentVersion"] = "0.3.10"
with open(path, "w") as f:
    json.dump(cfg, f, indent=2)
    f.write("\n")
PY
sudo chmod 600 /etc/nerdyrmm-agent/config.json

sudo tee /etc/xdg/autostart/nerdyrmm-agent-tray.desktop >/dev/null <<'EOF'
[Desktop Entry]
Type=Application
Name=NerdyRMM Agent
Exec=/opt/nerdyrmm/nerdyrmm-agent --tray
Icon=network-idle
Terminal=false
X-GNOME-Autostart-enabled=true
EOF

sudo systemctl start nerdyrmm-agent
sudo systemctl status nerdyrmm-agent --no-pager

# Tray in the existing Cinnamon session (roffo / DISPLAY=:0)
sudo -u roffo env DISPLAY=:0 XDG_RUNTIME_DIR=/run/user/$(id -u roffo) \
  DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/$(id -u roffo)/bus \
  /opt/nerdyrmm/nerdyrmm-agent --tray >/tmp/nerdyrmm-agent-tray.log 2>&1 &
```

Until `v0.3.10` exists on GitHub, build from this branch (`make build-linux`) and pass the local file:

```bash
sudo ./scripts/upgrade-inplace.sh dist/nerdyrmm-agent-linux-amd64
```

**Agent fails to start — config not found**

The agent panics if the config file does not exist. Verify the path:
- Linux Asgard / legacy: `/etc/nerdyrmm-agent/config.json`
- Linux README layout: `/etc/nerdyagent/config.json`
- Windows: `C:\ProgramData\NerdyRMM\config.json` or `C:\ProgramData\NerdyAgent\config.json`
- Override: `NRMM_AGENT_CONFIG=/path/to/config.json`

Check the agent log file in the same directory as `config.json` (`agent.log`).

**Agent registered but device does not appear in the server UI**

After the first successful registration, the agent saves `deviceId` and `token` to `config.json` and clears `enrollmentToken`. Check that the file is writable by the agent process.

**Check-in fails with HTTP 4xx/5xx**

- Confirm `serverUrl` is reachable from the machine.
- Confirm the enrollment token or device credentials are correct.
- Check firewall rules — the agent needs outbound HTTPS to `serverUrl`.

**Binary self-update fails**

- The watcher prefers the NerdyRMM latest-version API, then GitHub. A stale server `/downloads/agent-version.txt` (older than GitHub) will not downgrade the agent.
- Checksums are verified when published. A failed download leaves the current binary and auth fields alone.
- The process user must be able to replace its own executable and restart `nerdyrmm-agent` or `nerdyagent`.
- After a successful swap the detected systemd unit is restarted.

**Shell tunnel (browser terminal) not working on Windows**

The PTY-based shell tunnel is not supported on Windows. Use the command/script job types for remote execution on Windows agents.
