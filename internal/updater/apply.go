package updater

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/nerdyrmm/agent/internal/paths"
)

const minBinaryBytes = 64 * 1024

var applyMu sync.Mutex

// ApplyRequest is an update_agent job or a watcher-discovered release.
type ApplyRequest struct {
	Version        string
	BinaryURL      string
	SHA256         string
	ServiceName    string
	ConfigPath     string
	CurrentVersion string
	ServerURL      string
	Timeout        time.Duration
	OutputMaxBytes int
	HTTPClient     *http.Client
}

// Apply downloads, verifies, atomically replaces the running binary, bumps
// agentVersion only, and schedules a service restart. Failed downloads leave
// the current binary and config credentials untouched.
func Apply(ctx context.Context, req ApplyRequest) (status, output string) {
	applyMu.Lock()
	defer applyMu.Unlock()

	target := normalizeVersion(req.Version)
	if target == "" {
		return "failed", "missing target version"
	}
	if CompareVersions(req.CurrentVersion, target) >= 0 {
		return "success", fmt.Sprintf("agent already at %s", req.CurrentVersion)
	}
	binaryURL := strings.TrimSpace(req.BinaryURL)
	if binaryURL == "" {
		return "failed", "missing binary URL"
	}
	serviceName := strings.TrimSpace(req.ServiceName)
	if serviceName == "" {
		exe, _ := os.Executable()
		serviceName = paths.DetectServiceName(exe)
	}

	timeout := req.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var cancel context.CancelFunc
	ctx, cancel = context.WithTimeout(ctx, timeout)
	defer cancel()

	client := req.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: timeout}
	}

	candidates := []string{binaryURL}
	if fallback := fallbackDownloadURL(binaryURL, req.ServerURL); fallback != "" && fallback != binaryURL {
		candidates = append(candidates, fallback)
	}

	data, sourceURL, err := downloadBytes(ctx, client, candidates)
	if err != nil {
		return "failed", err.Error()
	}
	if err := verifyBinary(data, req.SHA256); err != nil {
		return "failed", err.Error()
	}

	exePath, err := os.Executable()
	if err != nil {
		return "failed", err.Error()
	}
	exePath, err = filepath.EvalSymlinks(exePath)
	if err != nil {
		exePath, _ = os.Executable()
	}

	if runtime.GOOS == "windows" {
		return applyWindows(data, exePath, target, sourceURL, serviceName, req.ConfigPath)
	}
	return applyUnix(data, exePath, target, sourceURL, serviceName, req.ConfigPath, req.OutputMaxBytes)
}

func applyUnix(data []byte, exePath, target, sourceURL, serviceName, configPath string, maxBytes int) (string, string) {
	tmpPath := exePath + ".new"
	if err := os.WriteFile(tmpPath, data, 0o755); err != nil {
		return "failed", err.Error()
	}
	if err := os.Chmod(tmpPath, 0o755); err != nil {
		_ = os.Remove(tmpPath)
		return "failed", err.Error()
	}

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
	if err := bumpAgentVersion(configPath, target); err != nil {
		_ = os.Rename(exePath, tmpPath)
		_ = os.Rename(bakPath, exePath)
		_ = os.Remove(tmpPath)
		return "failed", fmt.Sprintf("config version bump failed, binary restored: %v", err)
	}

	restartCmd := serviceRestartCommand(serviceName, exePath)
	_, restartOut := execShell(restartCmd, maxBytes)
	msg := fmt.Sprintf("updated agent binary to %s from %s; service restart scheduled", target, sourceURL)
	if strings.TrimSpace(restartOut) != "" {
		msg = msg + "\n" + restartOut
	}
	return "success", msg
}

func applyWindows(data []byte, exePath, target, sourceURL, serviceName, configPath string) (string, string) {
	tmpPath := exePath + ".new"
	if err := os.WriteFile(tmpPath, data, 0o755); err != nil {
		return "failed", err.Error()
	}
	psScriptPath := filepath.Join(os.TempDir(), "nerdyrmm-agent-self-update.ps1")
	psScript := fmt.Sprintf(`$ErrorActionPreference='SilentlyContinue'
Start-Sleep -Seconds 2
Stop-Service -Name '%s' -Force -ErrorAction SilentlyContinue
Start-Sleep -Milliseconds 500
Move-Item -Path '%s' -Destination '%s' -Force
Start-Service -Name '%s'
`, serviceName, strings.ReplaceAll(tmpPath, `'`, `''`), strings.ReplaceAll(exePath, `'`, `''`), serviceName)
	if err := os.WriteFile(psScriptPath, []byte(psScript), 0o600); err != nil {
		_ = os.Remove(tmpPath)
		return "failed", err.Error()
	}
	if err := bumpAgentVersion(configPath, target); err != nil {
		_ = os.Remove(tmpPath)
		return "failed", fmt.Sprintf("config version bump failed: %v", err)
	}
	launcher := exec.Command("powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", psScriptPath)
	if err := launcher.Start(); err != nil {
		return "failed", fmt.Sprintf("failed to schedule windows service restart: %v", err)
	}
	return "success", fmt.Sprintf("updated agent binary to %s from %s; service restart scheduled", target, sourceURL)
}

func downloadBytes(ctx context.Context, client *http.Client, candidates []string) ([]byte, string, error) {
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
		req.Header.Set("User-Agent", "NerdyAgent-updater")
		resp, err := client.Do(req)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", urlText, err))
			continue
		}
		data, readErr := io.ReadAll(io.LimitReader(resp.Body, 80<<20))
		_ = resp.Body.Close()
		if readErr != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", urlText, readErr))
			continue
		}
		if resp.StatusCode >= 300 {
			errs = append(errs, fmt.Sprintf("%s: status %d", urlText, resp.StatusCode))
			continue
		}
		return data, urlText, nil
	}
	if len(errs) == 0 {
		return nil, "", fmt.Errorf("download failed: no candidate URLs")
	}
	return nil, "", fmt.Errorf("download failed: %s", strings.Join(errs, " | "))
}

func verifyBinary(data []byte, wantSHA string) error {
	if len(data) < minBinaryBytes {
		return fmt.Errorf("downloaded file too small (%d bytes); refusing to replace binary", len(data))
	}
	if looksLikeHTML(data) {
		return fmt.Errorf("downloaded file looks like HTML; refusing to replace binary")
	}
	if !hasExecMagic(data) {
		return fmt.Errorf("downloaded file is not a recognized executable; refusing to replace binary")
	}
	wantSHA = parseDigestSHA256(wantSHA)
	if wantSHA == "" {
		return nil
	}
	sum := sha256.Sum256(data)
	got := hex.EncodeToString(sum[:])
	if got != wantSHA {
		return fmt.Errorf("checksum mismatch: got %s want %s", got, wantSHA)
	}
	return nil
}

func hasExecMagic(data []byte) bool {
	if len(data) < 4 {
		return false
	}
	// ELF
	if data[0] == 0x7f && data[1] == 'E' && data[2] == 'L' && data[3] == 'F' {
		return true
	}
	// PE
	if data[0] == 'M' && data[1] == 'Z' {
		return true
	}
	// Mach-O (thin / fat)
	if (data[0] == 0xcf && data[1] == 0xfa && data[2] == 0xed && data[3] == 0xfe) ||
		(data[0] == 0xce && data[1] == 0xfa && data[2] == 0xed && data[3] == 0xfe) ||
		(data[0] == 0xca && data[1] == 0xfe && data[2] == 0xba && data[3] == 0xbe) ||
		(data[0] == 0xfe && data[1] == 0xed && data[2] == 0xfa && data[3] == 0xcf) {
		return true
	}
	return false
}

func looksLikeHTML(data []byte) bool {
	s := strings.ToLower(string(data[:min(len(data), 256)]))
	s = strings.TrimSpace(s)
	return strings.HasPrefix(s, "<!doctype") || strings.HasPrefix(s, "<html") || strings.Contains(s, "<html")
}

func fallbackDownloadURL(binaryURL, serverURL string) string {
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

// bumpAgentVersion writes only agentVersion, preserving deviceId/token/serverUrl.
func bumpAgentVersion(configPath, version string) error {
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
	m["agentVersion"] = normalizeVersion(version)
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	out = append(out, '\n')
	tmp := configPath + ".tmp"
	if err := os.WriteFile(tmp, out, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, configPath); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

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
  nohup %s >/tmp/nerdyrmm-agent-manual-restart.log 2>&1 &
fi) >/tmp/nerdyrmm-agent-update.log 2>&1 &`, pathExport, service, service, exe, exe)
}

func execShell(command string, max int) (string, string) {
	if max <= 0 {
		max = 65536
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd", "/C", command)
	} else {
		cmd = exec.CommandContext(ctx, "bash", "-lc", command)
	}
	b, err := cmd.CombinedOutput()
	out := string(b)
	if len(out) > max {
		out = out[:max] + "\n[truncated]"
	}
	if err != nil {
		return "failed", out
	}
	return "success", out
}

func shellEscape(value string) string {
	if strings.TrimSpace(value) == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// PayloadFromMap adapts an update_agent job payload.
func PayloadFromMap(payload map[string]interface{}) ApplyRequest {
	req := ApplyRequest{}
	if payload == nil {
		return req
	}
	req.Version = firstString(payload, "version", "latestVersion")
	req.BinaryURL = firstString(payload, "binaryUrl", "binary_url", "url")
	req.SHA256 = firstString(payload, "sha256", "checksum", "digest", "hash")
	req.ServiceName = firstString(payload, "serviceName", "service_name")
	return req
}
