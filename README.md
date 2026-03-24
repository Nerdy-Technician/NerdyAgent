# NerdyAgent

NerdyAgent is the standalone, publicly distributable RMM agent for [NerdyRMM](https://github.com/Nerdy-Technician/NerdyRMM). Install it on any Linux or Windows machine to connect it to your NerdyRMM server for remote monitoring and management.

## Quick Install

### Linux (one-liner)

```bash
curl -fsSL https://raw.githubusercontent.com/Nerdy-Technician/NerdyAgent/main/scripts/install.sh | \
  NRMM_SERVER=https://your-server.com NRMM_TOKEN=your-enrollment-token bash
```

Requires root. Installs to `/usr/local/bin/nerdyagent`, config to `/etc/nerdyagent/config.json`, and registers a systemd service.

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

3. Place the binary at `/usr/local/bin/nerdyagent` (Linux) or `C:\Program Files\NerdyAgent\nerdyagent.exe` (Windows).

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
| `agentVersion` | string | `0.3.5` | Reported agent version. Updated automatically on agent self-update. |
| `jobTimeoutSec` | int | `120` | Maximum seconds a single job (command/script) may run before being killed. |
| `outputMaxBytes` | int | `131072` | Maximum bytes of output captured per job (128 KB). Excess is truncated. |

The config file path can be overridden via the `NRMM_AGENT_CONFIG` environment variable.

## Building from Source

Requirements: Go 1.24+

```bash
git clone https://github.com/Nerdy-Technician/NerdyAgent.git
cd NerdyAgent
go build -o nerdyagent ./cmd/agent
```

Or use Make for cross-platform builds:

```bash
make build          # build for current platform
make build-linux    # linux amd64 + arm64
make build-windows  # windows amd64
make build-darwin   # macOS amd64 + arm64
make build-all      # all of the above → dist/
make clean          # remove build artifacts
```

## Service Management

### Linux (systemd)

```bash
# Check status
systemctl status nerdyagent

# View live logs
journalctl -u nerdyagent -f

# Restart
systemctl restart nerdyagent

# Stop / disable
systemctl stop nerdyagent
systemctl disable nerdyagent
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

**Agent fails to start — config not found**

The agent panics if the config file does not exist. Verify the path:
- Linux default: `/etc/nerdyagent/config.json`
- Windows default: `C:\ProgramData\NerdyAgent\config.json`
- Override: `NRMM_AGENT_CONFIG=/path/to/config.json`

Check the agent log file in the same directory as `config.json` (`agent.log`).

**Agent registered but device does not appear in the server UI**

After the first successful registration, the agent saves `deviceId` and `token` to `config.json` and clears `enrollmentToken`. Check that the file is writable by the agent process.

**Check-in fails with HTTP 4xx/5xx**

- Confirm `serverUrl` is reachable from the machine.
- Confirm the enrollment token or device credentials are correct.
- Check firewall rules — the agent needs outbound HTTPS to `serverUrl`.

**Binary self-update fails**

- The update job requires the server to serve the new binary at a URL reachable by the agent.
- On Linux, the agent must have write permission to its own executable path.
- The service is restarted automatically after a successful binary swap.

**Shell tunnel (browser terminal) not working on Windows**

The PTY-based shell tunnel is not supported on Windows. Use the command/script job types for remote execution on Windows agents.
