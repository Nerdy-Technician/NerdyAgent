package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nerdyrmm/agent/internal/config"
	"github.com/nerdyrmm/agent/internal/paths"
	"github.com/nerdyrmm/agent/internal/protocol"
	"github.com/nerdyrmm/agent/internal/runner"
	"github.com/nerdyrmm/agent/internal/status"
	"github.com/nerdyrmm/agent/internal/sysinfo"
	"github.com/nerdyrmm/agent/internal/tray"
	"github.com/nerdyrmm/agent/internal/tunnel"
	"github.com/nerdyrmm/agent/internal/updater"
)

// Version is overridden by -ldflags at build time.
var Version = "0.3.10.3"

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
	args := os.Args[1:]
	if len(args) > 0 {
		switch args[0] {
		case "--tray", "tray":
			if err := tray.Run(); err != nil {
				fmt.Fprintf(os.Stderr, "tray: %v\n", err)
				os.Exit(1)
			}
			return
		case "--version", "version", "-v":
			fmt.Println(Version)
			return
		case "--install-icons", "install-icons":
			path, theme, err := tray.InstallThemeIcons()
			if err != nil {
				fmt.Fprintf(os.Stderr, "install-icons: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("installed theme icons: %s (theme %s)\n", path, theme)
			return
		case "--self-update", "self-update":
			if err := runSelfUpdateOnce(); err != nil {
				fmt.Fprintf(os.Stderr, "self-update: %v\n", err)
				os.Exit(1)
			}
			return
		case "--help", "help", "-h":
			printUsage()
			return
		}
	}

	cfgPath := defaultConfigPath()
	if v := os.Getenv("NRMM_AGENT_CONFIG"); v != "" {
		cfgPath = v
	}
	fileLog := newAgentFileLog(cfgPath)
	fileLog.writef("agent bootstrap started; cfg=%s version=%s", cfgPath, Version)
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

func printUsage() {
	fmt.Printf(`NerdyAgent %s

Usage:
  nerdyrmm-agent                 Run the RMM agent (systemd / Windows service)
  nerdyrmm-agent --tray          Linux system tray (user graphical session)
  nerdyrmm-agent --install-icons Install hicolor/pixmaps PNGs (root → /usr/share)
  nerdyrmm-agent --self-update   Poll server/GitHub and apply a newer build
  nerdyrmm-agent --version       Print version

Config: NRMM_AGENT_CONFIG or first existing of
  /etc/nerdyrmm-agent/config.json  (Asgard / legacy)
  /etc/nerdyagent/config.json      (README layout)
`, Version)
}

func defaultConfigPath() string {
	return paths.ConfigFile()
}

func runSelfUpdateOnce() error {
	cfgPath := defaultConfigPath()
	if v := os.Getenv("NRMM_AGENT_CONFIG"); v != "" {
		cfgPath = v
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return err
	}
	if strings.TrimSpace(cfg.AgentVersion) == "" {
		cfg.AgentVersion = Version
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	rel, err := updater.Discover(ctx, cfg.AgentVersion, updater.DiscoverConfig{
		ServerURL:  cfg.ServerURL,
		Token:      cfg.Token,
		GitHubRepo: os.Getenv("NRMM_AGENT_GITHUB_REPO"),
	})
	if err != nil {
		return err
	}
	if updater.CompareVersions(cfg.AgentVersion, rel.Version) >= 0 {
		fmt.Printf("already up to date (%s)\n", cfg.AgentVersion)
		return nil
	}
	st, out := updater.Apply(ctx, updater.ApplyRequest{
		Version:        rel.Version,
		BinaryURL:      rel.BinaryURL,
		SHA256:         rel.SHA256,
		ServiceName:    rel.ServiceName,
		ConfigPath:     cfgPath,
		CurrentVersion: cfg.AgentVersion,
		ServerURL:      cfg.ServerURL,
		Timeout:        5 * time.Minute,
		OutputMaxBytes: 65536,
	})
	fmt.Printf("%s: %s\n", st, out)
	if st != "success" {
		return fmt.Errorf("%s", out)
	}
	return nil
}

func runVersionWatcher(cfgPath string, fileLog *agentFileLog) {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		cfg, err := config.Load(cfgPath)
		if err != nil {
			continue
		}
		if strings.TrimSpace(cfg.AgentVersion) == "" {
			cfg.AgentVersion = Version
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		rel, err := updater.Discover(ctx, cfg.AgentVersion, updater.DiscoverConfig{
			ServerURL:  cfg.ServerURL,
			Token:      cfg.Token,
			GitHubRepo: os.Getenv("NRMM_AGENT_GITHUB_REPO"),
		})
		cancel()
		if err != nil {
			fileLog.writef("version watcher: discover failed: %v", err)
			continue
		}
		if updater.CompareVersions(cfg.AgentVersion, rel.Version) >= 0 {
			continue
		}
		fileLog.writef("version watcher: current=%s latest=%s source=%s — installing", cfg.AgentVersion, rel.Version, rel.Source)
		payload := map[string]interface{}{
			"version":     rel.Version,
			"binaryUrl":   rel.BinaryURL,
			"sha256":      rel.SHA256,
			"serviceName": rel.ServiceName,
		}
		if strings.TrimSpace(rel.ServiceName) == "" {
			payload["serviceName"] = runner.AgentServiceName()
		}
		st, output := runner.RunUpdateAgent(payload, runner.Config{
			TimeoutSec:     300,
			OutputMaxBytes: 65536,
			CurrentVersion: cfg.AgentVersion,
			ConfigPath:     cfgPath,
			ServerURL:      cfg.ServerURL,
		})
		fileLog.writef("version watcher update result: status=%s output=%s", st, output)
	}
}

func publishStatus(cfgPath string, cfg config.Config, ok bool, errMsg string) {
	_ = status.WriteSnapshot(cfgPath, status.Snapshot{
		Version:       firstNonEmpty(cfg.AgentVersion, Version),
		LastCheckin:   time.Now().UTC().Format(time.RFC3339),
		LastCheckinOK: ok,
		LastError:     errMsg,
		ServerURL:     cfg.ServerURL,
		DeviceID:      cfg.DeviceID,
		Service:       runner.AgentServiceName(),
		ConfigPath:    cfgPath,
	})
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func runAgent(cfgPath string, fileLog *agentFileLog) {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		fileLog.writef("failed to load config: %v", err)
		panic(err)
	}
	if strings.TrimSpace(cfg.AgentVersion) == "" {
		cfg.AgentVersion = Version
	}
	fileLog.writef("config loaded; server=%s deviceId=%d version=%s", cfg.ServerURL, cfg.DeviceID, cfg.AgentVersion)
	publishStatus(cfgPath, cfg, false, "")

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
			publishStatus(cfgPath, cfg, false, err.Error())
			if backoff < 5*time.Minute {
				backoff *= 2
			}
			time.Sleep(backoff)
			continue
		}
		publishStatus(cfgPath, cfg, true, "")
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
		st, output := runner.Run(j, runner.Config{
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
			Status:   st,
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
