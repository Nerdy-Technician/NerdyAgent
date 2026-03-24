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

	"github.com/nerdyagent/agent/internal/config"
	"github.com/nerdyagent/agent/internal/protocol"
	"github.com/nerdyagent/agent/internal/runner"
	"github.com/nerdyagent/agent/internal/status"
	"github.com/nerdyagent/agent/internal/sysinfo"
	"github.com/nerdyagent/agent/internal/tunnel"
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
	cfgPath := "/etc/nerdyagent/config.json"
	if runtime.GOOS == "windows" {
		programDataDir := os.Getenv("ProgramData")
		if strings.TrimSpace(programDataDir) == "" {
			programDataDir = `C:\ProgramData`
		}
		cfgPath = filepath.Join(programDataDir, "NerdyAgent", "config.json")
	}
	return cfgPath
}

func runAgent(cfgPath string, fileLog *agentFileLog) {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		fileLog.writef("failed to load config: %v", err)
		panic(err)
	}
	fileLog.writef("config loaded; server=%s deviceId=%d", cfg.ServerURL, cfg.DeviceID)
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
	payload := protocol.CheckinRequest{
		DeviceID:     cfg.DeviceID,
		Token:        cfg.Token,
		Hostname:     sysinfo.Hostname(),
		OS:           sysinfo.OS(),
		Arch:         sysinfo.Arch(),
		AgentVersion: cfg.AgentVersion,
		IPs:          sysinfo.IPs(),
		Metrics:      sysinfo.Metrics(),
		Inventory:    sysinfo.Inventory(),
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
