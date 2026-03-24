package runner

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/nerdyagent/agent/internal/protocol"
)

type Config struct {
	TimeoutSec     int
	OutputMaxBytes int
	CurrentVersion string
	ConfigPath     string
	ServerURL      string
}

func Run(job protocol.Job, cfg Config) (status, output string) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.TimeoutSec)*time.Second)
	defer cancel()

	var payload map[string]interface{}
	_ = json.Unmarshal([]byte(job.PayloadRaw), &payload)

	switch job.Type {
	case "command":
		cmd, _ := payload["command"].(string)
		return execShell(ctx, cmd, cfg.OutputMaxBytes)
	case "script":
		script, _ := payload["script"].(string)
		language, _ := payload["language"].(string)
		return execScript(ctx, script, language, cfg.OutputMaxBytes)
	case "file_deploy":
		filePath, _ := payload["filePath"].(string)
		dst, _ := payload["destination"].(string)
		execute, _ := payload["execute"].(bool)
		return deployFile(ctx, filePath, dst, execute, cfg.OutputMaxBytes)
	case "update_agent":
		return updateAgentBinary(ctx, payload, cfg)
	case "desktop_enable":
		return enableDesktopAccess(ctx, payload, cfg.OutputMaxBytes)
	case "managed_deploy":
		return managedDeploy(ctx, payload, cfg)
	case "software_remove":
		return removeSoftware(ctx, payload)
	default:
		return "failed", "unsupported job type"
	}
}

// psSingleQuoteEscape escapes a string for safe use inside PowerShell single-quoted strings.
// It escapes backticks (PowerShell's escape character) first, then single quotes.
func psSingleQuoteEscape(s string) string {
	s = strings.ReplaceAll(s, "`", "``")
	s = strings.ReplaceAll(s, "'", "''")
	return s
}

func removeSoftware(ctx context.Context, payload map[string]interface{}) (string, string) {
	name := strings.TrimSpace(fmt.Sprintf("%v", payload["name"]))
	if name == "" || name == "<nil>" {
		return "failed", "software name is required"
	}
	source := strings.TrimSpace(fmt.Sprintf("%v", payload["source"]))
	if runtime.GOOS == "windows" {
		return removeSoftwareWindows(ctx, name, source)
	}
	return removeSoftwareLinux(ctx, name, source)
}

func removeSoftwareWindows(ctx context.Context, name, source string) (string, string) {
	escapedName := psSingleQuoteEscape(name)
	escapedSource := psSingleQuoteEscape(source)
	script := fmt.Sprintf(`$ErrorActionPreference='Continue'
$name='%s'
$src='%s'
$paths=@('HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\*','HKLM:\SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall\*','HKCU:\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\*')
$items=@()
foreach($p in $paths){
  $items += Get-ItemProperty $p -ErrorAction SilentlyContinue | Where-Object { $_.DisplayName -and $_.DisplayName -like "*${name}*" }
}
$items = $items | Sort-Object DisplayName, DisplayVersion -Unique
if(-not $items -or $items.Count -eq 0){
  Write-Output ("No installed software matched: " + $name)
  exit 2
}
$failed=0
foreach($app in $items){
  $display = [string]$app.DisplayName
  $ver = [string]$app.DisplayVersion
  $quiet = [string]$app.QuietUninstallString
  $uninst = [string]$app.UninstallString
  $cmd = ''
  if($quiet){ $cmd = $quiet } elseif($uninst){ $cmd = $uninst }
  Write-Output ("Software Removal - " + $display + " " + $ver)
  if(-not $cmd){
    Write-Output "No uninstall command found."
    $failed++
    continue
  }
  $normalized = $cmd.Trim()
  if($normalized -match '(?i)^msiexec(\\.exe)?\s+'){
    $normalized = [Regex]::Replace($normalized, '(?i)\s+/i\s+', ' /x ')
    if($normalized -notmatch '(?i)\s+/x\s+'){
      $normalized += ' /x'
    }
    if($normalized -notmatch '(?i)\s+/qn\b'){ $normalized += ' /qn' }
    if($normalized -notmatch '(?i)\s+/norestart\b'){ $normalized += ' /norestart' }
  }
  Write-Output ("Executing: " + $normalized)
  cmd.exe /c $normalized
  $exitCode = $LASTEXITCODE
  Write-Output ("Exit code: " + $exitCode)
  if($exitCode -ne 0){ $failed++ }
}
if($failed -gt 0){
  Write-Output ("Software removal finished with failures: " + $failed)
  exit 1
}
Write-Output "Software removal completed successfully"
exit 0
`, escapedName, escapedSource)
	cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", script)
	b, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return "failed", string(b) + "\nsoftware removal timed out"
	}
	out := strings.TrimSpace(string(b))
	if err != nil {
		if out == "" {
			out = err.Error()
		}
		return "failed", out
	}
	return "success", out
}

func removeSoftwareLinux(ctx context.Context, name, source string) (string, string) {
	escapedName := shellEscape(name)
	escapedSource := strings.ToLower(strings.TrimSpace(source))
	cmdText := ""
	switch {
	case strings.Contains(escapedSource, "apt"):
		cmdText = fmt.Sprintf("sudo DEBIAN_FRONTEND=noninteractive apt-get remove -y --purge %s", escapedName)
	case strings.Contains(escapedSource, "dnf"):
		cmdText = fmt.Sprintf("sudo dnf remove -y %s", escapedName)
	case strings.Contains(escapedSource, "yum"):
		cmdText = fmt.Sprintf("sudo yum remove -y %s", escapedName)
	case strings.Contains(escapedSource, "pacman"):
		cmdText = fmt.Sprintf("sudo pacman -Rns --noconfirm %s", escapedName)
	default:
		cmdText = fmt.Sprintf(`bash -lc '
if command -v apt-get >/dev/null 2>&1; then sudo DEBIAN_FRONTEND=noninteractive apt-get remove -y --purge %s;
elif command -v dnf >/dev/null 2>&1; then sudo dnf remove -y %s;
elif command -v yum >/dev/null 2>&1; then sudo yum remove -y %s;
elif command -v pacman >/dev/null 2>&1; then sudo pacman -Rns --noconfirm %s;
else echo "No supported package manager found"; exit 2; fi'`, escapedName, escapedName, escapedName, escapedName)
	}
	return execShell(ctx, cmdText, 10*1024*1024)
}

func execShell(ctx context.Context, command string, max int) (string, string) {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd", "/C", command)
	} else {
		cmd = exec.CommandContext(ctx, "bash", "-lc", command)
	}
	b, err := cmd.CombinedOutput()
	out := trimOutput(string(b), max)
	if ctx.Err() == context.DeadlineExceeded {
		return "failed", out + "\ncommand timed out"
	}
	if err != nil {
		return "failed", out
	}
	return "success", out
}

// managedDeploy is a placeholder for future managed software deployment support.
// It currently returns a clear message so scheduler feedback is explicit.
func managedDeploy(ctx context.Context, payload map[string]interface{}, cfg Config) (string, string) {
	return "failed", "managed_deploy job type not implemented in agent yet"
}

func execScript(ctx context.Context, script, language string, max int) (string, string) {
	lang := detectScriptLanguage(language, script)
	ext := ".sh"
	switch lang {
	case "powershell":
		ext = ".ps1"
	case "cmd":
		ext = ".cmd"
	case "python":
		ext = ".py"
	}
	tmp, err := os.CreateTemp("", "nerdyagent-script-*"+ext)
	if err != nil {
		return "failed", err.Error()
	}
	defer os.Remove(tmp.Name())
	_, _ = tmp.WriteString(script)
	_ = tmp.Close()
	_ = os.Chmod(tmp.Name(), 0o700)

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		switch lang {
		case "powershell":
			cmd = exec.CommandContext(ctx, "powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", tmp.Name())
		case "python":
			py := "python"
			if _, err := exec.LookPath(py); err != nil {
				py = "py"
			}
			cmd = exec.CommandContext(ctx, py, tmp.Name())
		case "bash", "sh":
			cmd = exec.CommandContext(ctx, "bash", tmp.Name())
		default:
			cmd = exec.CommandContext(ctx, "cmd", "/C", tmp.Name())
		}
	} else {
		switch lang {
		case "python":
			py := "python3"
			if _, err := exec.LookPath(py); err != nil {
				py = "python"
			}
			cmd = exec.CommandContext(ctx, py, tmp.Name())
		case "powershell":
			cmd = exec.CommandContext(ctx, "pwsh", "-NoProfile", "-File", tmp.Name())
		default:
			cmd = exec.CommandContext(ctx, "bash", tmp.Name())
		}
	}

	b, err := cmd.CombinedOutput()
	out := trimOutput(string(b), max)
	if ctx.Err() == context.DeadlineExceeded {
		return "failed", out + "\nscript timed out"
	}
	if err != nil {
		return "failed", out
	}
	return "success", out
}

func deployFile(ctx context.Context, srcPath, dst string, execute bool, max int) (string, string) {
	if srcPath == "" || dst == "" {
		return "failed", "invalid file payload"
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "failed", err.Error()
	}
	src, err := os.Open(srcPath)
	if err != nil {
		return "failed", err.Error()
	}
	defer src.Close()
	out, err := os.Create(dst)
	if err != nil {
		return "failed", err.Error()
	}
	if _, err := io.Copy(out, src); err != nil {
		_ = out.Close()
		return "failed", err.Error()
	}
	_ = out.Close()
	_ = os.Chmod(dst, 0o755)
	msg := fmt.Sprintf("file deployed to %s", dst)
	if execute {
		status, execOut := execLocalBinary(ctx, dst, max)
		return status, msg + "\n" + execOut
	}
	return "success", msg
}

func trimOutput(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "\n[truncated]"
}

func updateAgentBinary(ctx context.Context, payload map[string]interface{}, cfg Config) (string, string) {
	targetVersion, _ := payload["version"].(string)
	targetVersion = strings.TrimSpace(targetVersion)
	if targetVersion == "" {
		return "failed", "missing target version"
	}
	if compareVersions(cfg.CurrentVersion, targetVersion) >= 0 {
		return "success", fmt.Sprintf("agent already at %s", cfg.CurrentVersion)
	}

	binaryURL, _ := payload["binaryUrl"].(string)
	binaryURL = strings.TrimSpace(binaryURL)
	if binaryURL == "" {
		return "failed", "missing binary URL"
	}
	serviceName, _ := payload["serviceName"].(string)
	serviceName = strings.TrimSpace(serviceName)
	if serviceName == "" {
		if runtime.GOOS == "windows" {
			serviceName = "NerdyAgent"
		} else {
			serviceName = "nerdyagent"
		}
	}

	if runtime.GOOS == "windows" {
		return updateAgentBinaryWindows(ctx, payload, cfg, serviceName)
	}

	candidateURLs := []string{binaryURL}
	if fallbackURL := buildFallbackBinaryURL(binaryURL, cfg.ServerURL); fallbackURL != "" && fallbackURL != binaryURL {
		candidateURLs = append(candidateURLs, fallbackURL)
	}
	resp, sourceURL, downloadErr := downloadBinaryFromCandidates(ctx, candidateURLs)
	if downloadErr != nil {
		return "failed", downloadErr.Error()
	}
	defer resp.Body.Close()

	exePath, err := os.Executable()
	if err != nil {
		return "failed", err.Error()
	}
	tmpPath := exePath + ".new"
	tmpFile, err := os.Create(tmpPath)
	if err != nil {
		return "failed", err.Error()
	}
	if _, err := io.Copy(tmpFile, resp.Body); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return "failed", err.Error()
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return "failed", err.Error()
	}
	_ = os.Chmod(tmpPath, 0o755)

	bakPath := exePath + ".bak"
	_ = os.Remove(bakPath)
	if err := os.Rename(exePath, bakPath); err != nil {
		_ = os.Remove(tmpPath)
		return "failed", fmt.Sprintf("backup current binary failed: %v", err)
	}
	if err := os.Rename(tmpPath, exePath); err != nil {
		_ = os.Rename(bakPath, exePath)
		_ = os.Remove(tmpPath)
		return "failed", fmt.Sprintf("activate updated binary failed: %v", err)
	}
	_ = os.Remove(bakPath)
	_ = writeAgentVersionToConfig(cfg.ConfigPath, targetVersion)

	restartCmd := serviceRestartCommand(serviceName, exePath)
	_, restartOut := execShell(context.Background(), restartCmd, cfg.OutputMaxBytes)
	msg := fmt.Sprintf("updated agent binary to %s from %s; service restart scheduled", targetVersion, sourceURL)
	if strings.TrimSpace(restartOut) != "" {
		msg = msg + "\n" + restartOut
	}
	return "success", msg
}

func updateAgentBinaryWindows(ctx context.Context, payload map[string]interface{}, cfg Config, serviceName string) (string, string) {
	targetVersion, _ := payload["version"].(string)
	targetVersion = strings.TrimSpace(targetVersion)
	if targetVersion == "" {
		return "failed", "missing target version"
	}
	if compareVersions(cfg.CurrentVersion, targetVersion) >= 0 {
		return "success", fmt.Sprintf("agent already at %s", cfg.CurrentVersion)
	}
	binaryURL, _ := payload["binaryUrl"].(string)
	binaryURL = strings.TrimSpace(binaryURL)
	if binaryURL == "" {
		return "failed", "missing binary URL"
	}
	candidateURLs := []string{binaryURL}
	if fallbackURL := buildFallbackBinaryURL(binaryURL, cfg.ServerURL); fallbackURL != "" && fallbackURL != binaryURL {
		candidateURLs = append(candidateURLs, fallbackURL)
	}
	resp, sourceURL, downloadErr := downloadBinaryFromCandidates(ctx, candidateURLs)
	if downloadErr != nil {
		return "failed", downloadErr.Error()
	}
	defer resp.Body.Close()

	exePath, err := os.Executable()
	if err != nil {
		return "failed", err.Error()
	}
	tmpPath := exePath + ".new"
	tmpFile, err := os.Create(tmpPath)
	if err != nil {
		return "failed", err.Error()
	}
	if _, err := io.Copy(tmpFile, resp.Body); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return "failed", err.Error()
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return "failed", err.Error()
	}

	psScriptPath := filepath.Join(os.TempDir(), "nerdyagent-self-update.ps1")
	psScript := fmt.Sprintf(`$ErrorActionPreference='SilentlyContinue'
Start-Sleep -Seconds 2
Stop-Service -Name '%s' -Force -ErrorAction SilentlyContinue
Start-Sleep -Milliseconds 500
Move-Item -Path '%s' -Destination '%s' -Force
Start-Service -Name '%s'
`, psSingleQuoteEscape(serviceName), psSingleQuoteEscape(tmpPath), psSingleQuoteEscape(exePath), psSingleQuoteEscape(serviceName))
	if err := os.WriteFile(psScriptPath, []byte(psScript), 0o600); err != nil {
		return "failed", err.Error()
	}
	launcher := exec.Command("powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", psScriptPath)
	if err := launcher.Start(); err != nil {
		return "failed", fmt.Sprintf("failed to schedule windows service restart: %v", err)
	}
	_ = writeAgentVersionToConfig(cfg.ConfigPath, targetVersion)
	return "success", fmt.Sprintf("updated agent binary to %s from %s; service restart scheduled", targetVersion, sourceURL)
}

func downloadBinaryFromCandidates(ctx context.Context, candidates []string) (*http.Response, string, error) {
	errs := make([]string, 0, len(candidates))
	for _, rawURL := range candidates {
		urlText := strings.TrimSpace(rawURL)
		if urlText == "" {
			continue
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlText, nil)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", urlText, err))
			continue
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", urlText, err))
			continue
		}
		if resp.StatusCode >= 300 {
			_ = resp.Body.Close()
			errs = append(errs, fmt.Sprintf("%s: status %d", urlText, resp.StatusCode))
			continue
		}
		return resp, urlText, nil
	}
	if len(errs) == 0 {
		return nil, "", fmt.Errorf("download failed: no candidate URLs")
	}
	return nil, "", fmt.Errorf("download failed: %s", strings.Join(errs, " | "))
}

func buildFallbackBinaryURL(binaryURL, serverURL string) string {
	serverURL = strings.TrimRight(strings.TrimSpace(serverURL), "/")
	if serverURL == "" {
		return ""
	}
	binaryURL = strings.TrimSpace(binaryURL)
	if binaryURL == "" {
		return ""
	}
	parts := strings.Split(binaryURL, "/")
	fileName := strings.TrimSpace(parts[len(parts)-1])
	if fileName == "" {
		return ""
	}
	return serverURL + "/downloads/" + fileName
}

func compareVersions(current, target string) int {
	a := parseVersionParts(current)
	b := parseVersionParts(target)
	maxLen := len(a)
	if len(b) > maxLen {
		maxLen = len(b)
	}
	for i := 0; i < maxLen; i++ {
		av := 0
		if i < len(a) {
			av = a[i]
		}
		bv := 0
		if i < len(b) {
			bv = b[i]
		}
		if av < bv {
			return -1
		}
		if av > bv {
			return 1
		}
	}
	return 0
}

func parseVersionParts(v string) []int {
	trimmed := strings.TrimSpace(strings.TrimPrefix(strings.ToLower(v), "v"))
	if trimmed == "" {
		return []int{0}
	}
	parts := strings.Split(trimmed, ".")
	out := make([]int, 0, len(parts))
	for _, part := range parts {
		digits := make([]rune, 0, len(part))
		for _, r := range part {
			if r >= '0' && r <= '9' {
				digits = append(digits, r)
			} else {
				break
			}
		}
		if len(digits) == 0 {
			out = append(out, 0)
			continue
		}
		n := 0
		for _, r := range digits {
			n = n*10 + int(r-'0')
		}
		out = append(out, n)
	}
	return out
}

func writeAgentVersionToConfig(configPath, version string) error {
	configPath = strings.TrimSpace(configPath)
	if configPath == "" || strings.TrimSpace(version) == "" {
		return nil
	}
	b, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}
	m := map[string]interface{}{}
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	m["agentVersion"] = version
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	out = append(out, '\n')
	return os.WriteFile(configPath, out, 0o600)
}

// serviceRestartCommand builds a restart snippet that works across distros.
func serviceRestartCommand(serviceName, exePath string) string {
	service := shellEscape(serviceName)
	exe := shellEscape(exePath)
	pathExport := "PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:$PATH"
	return fmt.Sprintf(`(sleep 2;
%s;
if command -v systemctl >/dev/null 2>&1; then
  systemctl restart %s
elif command -v service >/dev/null 2>&1; then
  service %s restart
else
  pkill -f %s >/dev/null 2>&1 || true
  nohup %s >/tmp/nerdyagent-manual-restart.log 2>&1 &
fi) >/tmp/nerdyagent-update.log 2>&1 &`, pathExport, service, service, exe, exe)
}

// shellEscape wraps values for safe use in shell snippets.
func shellEscape(value string) string {
	if strings.TrimSpace(value) == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func enableDesktopAccess(ctx context.Context, payload map[string]interface{}, max int) (string, string) {
	password := strings.TrimSpace(fmt.Sprintf("%v", payload["password"]))
	if password == "" || password == "<nil>" {
		var err error
		password, err = randomHexToken(10)
		if err != nil {
			return "failed", fmt.Sprintf("password generation failed: %v", err)
		}
	}
	port := toInt(payload["port"])
	if port <= 0 || port > 65535 {
		port = 5900
	}
	display := strings.TrimSpace(fmt.Sprintf("%v", payload["display"]))
	if display == "" || display == "<nil>" {
		display = ":0"
	}
	if runtime.GOOS == "windows" {
		return enableDesktopAccessWindows(ctx, password, port, max)
	}
	return enableDesktopAccessLinux(ctx, password, port, display, max)
}

func enableDesktopAccessLinux(ctx context.Context, password string, port int, display string, max int) (string, string) {
	if port <= 0 || port > 65535 {
		port = 5900
	}
	cmd := fmt.Sprintf(`set +e
need_install=0
if ! command -v x11vnc >/dev/null 2>&1; then
  need_install=1
fi
if ! command -v Xvfb >/dev/null 2>&1; then
  need_install=1
fi
if [ "$need_install" -eq 1 ]; then
  if command -v apt-get >/dev/null 2>&1; then
    DEBIAN_FRONTEND=noninteractive apt-get update -y >/tmp/nerdyagent-vnc-install.log 2>&1
    DEBIAN_FRONTEND=noninteractive apt-get install -y x11vnc xvfb >>/tmp/nerdyagent-vnc-install.log 2>&1
  elif command -v dnf >/dev/null 2>&1; then
    dnf install -y x11vnc xorg-x11-server-Xvfb >/tmp/nerdyagent-vnc-install.log 2>&1
  elif command -v yum >/dev/null 2>&1; then
    yum install -y x11vnc xorg-x11-server-Xvfb >/tmp/nerdyagent-vnc-install.log 2>&1
  elif command -v pacman >/dev/null 2>&1; then
    pacman -Sy --noconfirm x11vnc xorg-server-xvfb >/tmp/nerdyagent-vnc-install.log 2>&1
  else
    echo "desktop prerequisites are missing and no supported package manager was detected"
    exit 99
  fi
  if ! command -v x11vnc >/dev/null 2>&1 || ! command -v Xvfb >/dev/null 2>&1; then
    echo "desktop prerequisite install failed (need x11vnc + Xvfb)"
    tail -n 60 /tmp/nerdyagent-vnc-install.log 2>/dev/null || true
    exit 99
  fi
fi
mkdir -p /etc/nerdyagent || true
x11vnc -storepasswd "%s" /etc/nerdyagent/vnc.pass
if [ $? -ne 0 ]; then
  echo "failed to write x11vnc password file"
  exit 97
fi
pkill -x x11vnc >/dev/null 2>&1 || true

DISPLAY_CAND="${DISPLAY:-}"
if [ -z "$DISPLAY_CAND" ] && command -v loginctl >/dev/null 2>&1; then
  DISPLAY_CAND=$(loginctl list-sessions --no-legend 2>/dev/null | awk '{print $1}' | while read -r sid; do
    loginctl show-session "$sid" -p Display --value 2>/dev/null || true
  done | sed '/^$/d' | head -n1 || true)
fi
if [ -z "$DISPLAY_CAND" ] && command -v xset >/dev/null 2>&1; then
  for d in :0 :1 :2; do
    if xset -display "$d" q >/dev/null 2>&1; then
      DISPLAY_CAND="$d"
      break
    fi
  done
fi
if [ -z "$DISPLAY_CAND" ]; then
  DISPLAY_CAND="%s"
fi

echo "Using display: $DISPLAY_CAND"
nohup x11vnc -localhost -display "$DISPLAY_CAND" -auth guess -rfbport %d -forever -shared -rfbauth /etc/nerdyagent/vnc.pass -noxdamage -o /var/log/nerdyagent-x11vnc.log >/dev/null 2>&1 &
sleep 2
if ! pgrep -x x11vnc >/dev/null 2>&1; then
  nohup x11vnc -localhost -create -rfbport %d -forever -shared -rfbauth /etc/nerdyagent/vnc.pass -noxdamage -o /var/log/nerdyagent-x11vnc.log >/dev/null 2>&1 &
  sleep 2
  if ! pgrep -x x11vnc >/dev/null 2>&1; then
    echo "x11vnc process did not start"
    tail -n 80 /var/log/nerdyagent-x11vnc.log 2>/dev/null || true
    exit 98
  fi
fi
if command -v ss >/dev/null 2>&1; then
  if ! ss -ltn 2>/dev/null | awk '{print $4}' | grep -q ":%d$"; then
    echo "x11vnc process is running but no localhost listener on port %d"
    tail -n 80 /var/log/nerdyagent-x11vnc.log 2>/dev/null || true
    exit 98
  fi
fi
exit 0
`, password, display, port, port, port, port)
	status, output := execShell(ctx, cmd, max)
	if status != "success" {
		return status, output
	}
	resp := map[string]interface{}{
		"enabled":  true,
		"host":     "127.0.0.1",
		"port":     port,
		"password": password,
		"display":  display,
	}
	b, _ := json.Marshal(resp)
	return "success", string(b)
}

func enableDesktopAccessWindows(ctx context.Context, _ string, port int, max int) (string, string) {
	if port <= 0 || port > 65535 {
		port = 5900
	}
	script := fmt.Sprintf(`$ErrorActionPreference='Stop'
$port=%d
$tvn=Join-Path $env:ProgramFiles 'TightVNC\tvnserver.exe'
if (!(Test-Path $tvn)) {
  $winget=Get-Command winget -ErrorAction SilentlyContinue
  if ($winget) {
    winget install --id GlavSoft.TightVNC -e --silent --accept-source-agreements --accept-package-agreements | Out-Null
  }
}
if (!(Test-Path $tvn)) { throw 'TightVNC is required for Browser noVNC sessions and could not be installed automatically.' }
$reg='HKLM:\SOFTWARE\TightVNC\Server'
if (!(Test-Path $reg)) { New-Item -Path $reg -Force | Out-Null }
Set-ItemProperty -Path $reg -Name LoopbackOnly -Type DWord -Value 1
Set-ItemProperty -Path $reg -Name RfbPort -Type DWord -Value $port
Set-ItemProperty -Path $reg -Name AcceptHttpConnections -Type DWord -Value 0
Set-ItemProperty -Path $reg -Name UseVncAuthentication -Type DWord -Value 0
Start-Process -FilePath $tvn -ArgumentList '-start' -WindowStyle Hidden
Start-Sleep -Seconds 2
$listener=Get-NetTCPConnection -State Listen -LocalPort $port -ErrorAction SilentlyContinue | Select-Object -First 1
if (-not $listener) { throw ('VNC listener not detected on localhost:' + $port) }
@{enabled=$true;host='127.0.0.1';port=$port;password='';display='console'} | ConvertTo-Json -Compress
`, port)
	cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", script)
	b, err := cmd.CombinedOutput()
	out := trimOutput(string(b), max)
	if ctx.Err() == context.DeadlineExceeded {
		return "failed", out + "\ndesktop setup timed out"
	}
	if err != nil {
		return "failed", out
	}
	return "success", strings.TrimSpace(out)
}

func execLocalBinary(ctx context.Context, path string, max int) (string, string) {
	if runtime.GOOS == "windows" {
		cmd := exec.CommandContext(ctx, "cmd", "/C", path)
		b, err := cmd.CombinedOutput()
		out := trimOutput(string(b), max)
		if err != nil {
			return "failed", out
		}
		return "success", out
	}
	return execShell(ctx, path, max)
}

func detectScriptLanguage(rawLanguage, script string) string {
	v := strings.ToLower(strings.TrimSpace(rawLanguage))
	switch v {
	case "powershell", "ps1", "pwsh":
		return "powershell"
	case "cmd", "batch", "bat":
		return "cmd"
	case "python", "py":
		return "python"
	case "bash", "sh", "shell":
		return "bash"
	}
	lines := strings.Split(script, "\n")
	for _, line := range lines {
		l := strings.TrimSpace(strings.ToLower(line))
		if strings.HasPrefix(l, "# @language:") {
			return detectScriptLanguage(strings.TrimSpace(strings.TrimPrefix(l, "# @language:")), "")
		}
		if strings.HasPrefix(l, "#!/") {
			if strings.Contains(l, "powershell") || strings.Contains(l, "pwsh") {
				return "powershell"
			}
			if strings.Contains(l, "python") {
				return "python"
			}
			if strings.Contains(l, "bash") || strings.Contains(l, "sh") {
				return "bash"
			}
		}
	}
	if runtime.GOOS == "windows" {
		return "powershell"
	}
	return "bash"
}

func toInt(v interface{}) int {
	switch t := v.(type) {
	case int:
		return t
	case int64:
		return int(t)
	case float64:
		return int(t)
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(t))
		return n
	default:
		return 0
	}
}

func randomHexToken(n int) (string, error) {
	if n <= 0 {
		n = 8
	}
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
