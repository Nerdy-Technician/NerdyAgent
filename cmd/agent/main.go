package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/nerdyrmm/agent/internal/config"
	"github.com/nerdyrmm/agent/internal/protocol"
	"github.com/nerdyrmm/agent/internal/runner"
	"github.com/nerdyrmm/agent/internal/status"
	"github.com/nerdyrmm/agent/internal/sysinfo"
	"github.com/nerdyrmm/agent/internal/tunnel"
)

type agentFileLog struct {
	path string
}

func newAgentFileLog(cfgPath string) *agentFileLog {
	dir := filepath.Dir(cfgPath)
	if strings.TrimSpace(dir) == "" {
		dir = "."
	}
	_ = os.MkdirAll(dir, 0o755)
	return &agentFileLog{path: filepath.Join(dir, "agent.log")}
}

func (l *agentFileLog) writef(format string, args ...interface{}) {
	if l == nil {
		return
	}
	msg := strings.TrimSpace(fmt.Sprintf(format, args...))
	if msg == "" {
		return
	}
	line := fmt.Sprintf("%s %s\n", time.Now().UTC().Format(time.RFC3339), msg)
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	_, _ = f.WriteString(line)
	_ = f.Close()
}

func main() {
	cfgPath := defaultConfigPath()
	if v := os.Getenv("NRMM_AGENT_CONFIG"); v != "" {
		cfgPath = v
	}
	fileLog := newAgentFileLog(cfgPath)
	fileLog.writef("agent bootstrap started; cfg=%s", cfgPath)
	defer func() {
		if r := recover(); r != nil {
			fileLog.writef("agent panic: %v", r)
			panic(r)
		}
	}()

	handled, err := maybeRunAsWindowsService(cfgPath)
	if err != nil {
		fileLog.writef("windows service bootstrap error: %v", err)
		panic(err)
	}
	if handled {
		fileLog.writef("running under windows service mode")
		return
	}

	runAgent(cfgPath, fileLog)
}

func defaultConfigPath() string {
	cfgPath := "/etc/nerdyrmm-agent/config.json"
	if runtime.GOOS == "windows" {
		programData := os.Getenv("ProgramData")
		if strings.TrimSpace(programData) == "" {
			programData = `C:\ProgramData`
		}
		cfgPath = filepath.Join(programData, "NerdyRMM", "config.json")
	}
	return cfgPath
}

const (
	githubReleasesAPI  = "https://api.github.com/repos/Nerdy-Technician/NerdyAgent/releases/latest"
	githubDownloadBase = "https://github.com/Nerdy-Technician/NerdyAgent/releases/download"
	forceUpdateEvery   = 12 * time.Hour
)

func fetchGitHubLatestVersion() (version string, err error) {
	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequest("GET", githubReleasesAPI, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "NerdyAgent-updater")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", err
	}
	v := strings.TrimPrefix(strings.TrimSpace(release.TagName), "v")
	if v == "" {
		return "", fmt.Errorf("empty tag_name")
	}
	return v, nil
}

func githubBinaryURL(version string) string {
	tag := "v" + version
	return githubDownloadBase + "/" + tag + "/" + runner.AgentBinaryFilename()
}

func runVersionWatcher(cfgPath string, fileLog *agentFileLog) {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	lastForced := time.Now()
	for range ticker.C {
		cfg, err := config.Load(cfgPath)
		if err != nil {
			continue
		}
		latestVersion, err := fetchGitHubLatestVersion()
		if err != nil {
			fileLog.writef("version watcher: github check failed: %v", err)
			continue
		}
		isNewer := runner.CompareVersions(cfg.AgentVersion, latestVersion) < 0
		forceInstall := time.Since(lastForced) >= forceUpdateEvery
		if !isNewer && !forceInstall {
			continue
		}
		reason := "newer version available"
		if forceInstall && !isNewer {
			reason = "forced 12h reinstall"
		}
		fileLog.writef("version watcher: current=%s latest=%s reason=%s — installing", cfg.AgentVersion, latestVersion, reason)
		payload := map[string]interface{}{
			"version":     latestVersion,
			"binaryUrl":   githubBinaryURL(latestVersion),
			"serviceName": "nerdyrmm-agent",
		}
		st, output := runner.RunUpdateAgent(payload, runner.Config{
			TimeoutSec:     300,
			OutputMaxBytes: 65536,
			CurrentVersion: cfg.AgentVersion,
			ConfigPath:     cfgPath,
			ServerURL:      cfg.ServerURL,
		})
		fileLog.writef("version watcher update result: status=%s output=%s", st, output)
		lastForced = time.Now()
	}
}

func runAgent(cfgPath string, fileLog *agentFileLog) {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		fileLog.writef("failed to load config: %v", err)
		panic(err)
	}
	fileLog.writef("config loaded; server=%s deviceId=%d", cfg.ServerURL, cfg.DeviceID)

	go runVersionWatcher(cfgPath, fileLog)

	tunnelStarted := false
	startTunnel := func(current config.Config) {
		if tunnelStarted {
			return
		}
		if current.DeviceID <= 0 || strings.TrimSpace(current.Token) == "" {
			return
		}
		tunnelStarted = true
		go tunnel.Run(current)
	}
	startTunnel(cfg)

	statusLog := status.New(filepath.Dir(cfgPath))
	statusLog.Write(fmt.Sprintf("agent started (version %s)", cfg.AgentVersion))
	backoff := cfg.CheckinEvery
	for {
		nextCfg, err, statusMsg := cycle(cfg, cfgPath)
		cfg = nextCfg
		startTunnel(cfg)
		if statusMsg != "" {
			statusLog.Write(statusMsg)
		}
		if err != nil {
			statusLog.Write(fmt.Sprintf("checkin failed: %v", err))
			fileLog.writef("checkin failed: %v", err)
			fmt.Printf("checkin failed: %v\n", err)
			if backoff < 5*time.Minute {
				backoff *= 2
			}
			time.Sleep(backoff)
			continue
		}
		backoff = cfg.CheckinEvery
		fmt.Printf("checkin success (interval %s)\n", cfg.CheckinEvery)
		time.Sleep(cfg.CheckinEvery)
	}
}

func cycle(cfg config.Config, cfgPath string) (config.Config, error, string) {
	if cfg.DeviceID <= 0 || strings.TrimSpace(cfg.Token) == "" {
		if strings.TrimSpace(cfg.EnrollmentToken) == "" {
			return cfg, fmt.Errorf("device credentials missing and enrollment token is empty"), ""
		}
		if err := registerAgent(&cfg, cfgPath); err != nil {
			return cfg, err, ""
		}
	}
	inventory := sysinfo.Inventory()
	running, failed := runner.CollectServiceStatus()
	if len(running) > 0 {
		inventory["services_running"] = strings.Join(running, ",")
	}
	if len(failed) > 0 {
		inventory["services_failed"] = strings.Join(failed, ",")
	}
	payload := protocol.CheckinRequest{
		DeviceID:     cfg.DeviceID,
		Token:        cfg.Token,
		Hostname:     sysinfo.Hostname(),
		OS:           sysinfo.OS(),
		Arch:         sysinfo.Arch(),
		AgentVersion: cfg.AgentVersion,
		IPs:          sysinfo.IPs(),
		Metrics:      sysinfo.Metrics(),
		Inventory:    inventory,
	}
	b, _ := json.Marshal(payload)
	resp, err := http.Post(cfg.ServerURL+"/api/agent/checkin", "application/json", bytes.NewReader(b))
	if err != nil {
		return cfg, err, ""
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return cfg, fmt.Errorf("checkin status: %d", resp.StatusCode), ""
	}
	var out protocol.CheckinResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return cfg, err, ""
	}
	for _, j := range out.Jobs {
		status, output := runner.Run(j, runner.Config{
			TimeoutSec:     cfg.JobTimeoutSec,
			OutputMaxBytes: cfg.OutputMaxBytes,
			CurrentVersion: cfg.AgentVersion,
			ConfigPath:     cfgPath,
			ServerURL:      cfg.ServerURL,
		})
		jr := protocol.JobResultRequest{
			DeviceID: cfg.DeviceID,
			Token:    cfg.Token,
			JobID:    j.ID,
			Status:   status,
			Output:   output,
		}
		jb, _ := json.Marshal(jr)
		_, _ = http.Post(cfg.ServerURL+"/api/agent/job-result", "application/json", bytes.NewReader(jb))
	}
	return cfg, nil, fmt.Sprintf("check-in success device=%d server=%s", cfg.DeviceID, cfg.ServerURL)
}

func registerAgent(cfg *config.Config, cfgPath string) error {
	req := protocol.RegisterRequest{
		EnrollmentToken: strings.TrimSpace(cfg.EnrollmentToken),
		Hostname:        sysinfo.Hostname(),
		OS:              sysinfo.OS(),
		Arch:            sysinfo.Arch(),
		AgentVersion:    cfg.AgentVersion,
		IPs:             sysinfo.IPs(),
		Inventory:       sysinfo.Inventory(),
	}
	b, _ := json.Marshal(req)
	resp, err := http.Post(cfg.ServerURL+"/api/agent/register", "application/json", bytes.NewReader(b))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("register status: %d", resp.StatusCode)
	}
	var out protocol.RegisterResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return err
	}
	if out.DeviceID <= 0 || strings.TrimSpace(out.Token) == "" {
		return fmt.Errorf("register response missing device credentials")
	}
	cfg.DeviceID = out.DeviceID
	cfg.Token = strings.TrimSpace(out.Token)
	cfg.EnrollmentToken = ""
	if err := config.Save(cfgPath, *cfg); err != nil {
		return err
	}
	fmt.Printf("agent registered: deviceId=%d\n", out.DeviceID)
	return nil
}
