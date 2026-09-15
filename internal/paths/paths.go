// Package paths detects NerdyAgent install layouts so updates and the tray
// keep working on both the current README paths and the older Asgard/opt layout.
package paths

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

const (
	// Legacy Asgard / early NerdyRMM layout.
	LegacyBinDir    = "/opt/nerdyrmm"
	LegacyBinName   = "nerdyrmm-agent"
	LegacyConfigDir = "/etc/nerdyrmm-agent"
	LegacyService   = "nerdyrmm-agent"

	// Documented standalone NerdyAgent layout.
	CurrentBinDir    = "/usr/local/bin"
	CurrentBinName   = "nerdyagent"
	CurrentConfigDir = "/etc/nerdyagent"
	CurrentService   = "nerdyagent"
)

// Layout describes where the agent binary, config, and service live.
type Layout struct {
	BinDir    string
	BinName   string
	BinPath   string
	ConfigDir string
	Config    string
	Service   string
	Name      string // "legacy", "current", or "windows"
}

// ConfigCandidates returns config.json paths in preference order.
func ConfigCandidates() []string {
	if v := strings.TrimSpace(os.Getenv("NRMM_AGENT_CONFIG")); v != "" {
		return []string{v}
	}
	if runtime.GOOS == "windows" {
		programData := strings.TrimSpace(os.Getenv("ProgramData"))
		if programData == "" {
			programData = `C:\ProgramData`
		}
		return []string{
			filepath.Join(programData, "NerdyRMM", "config.json"),
			filepath.Join(programData, "NerdyAgent", "config.json"),
		}
	}
	return []string{
		filepath.Join(LegacyConfigDir, "config.json"),
		filepath.Join(CurrentConfigDir, "config.json"),
	}
}

// ConfigFile returns the first existing config path, or the preferred default.
func ConfigFile() string {
	cands := ConfigCandidates()
	for _, p := range cands {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if len(cands) == 0 {
		return filepath.Join(LegacyConfigDir, "config.json")
	}
	return cands[0]
}

// StatusFile returns the world-readable status snapshot path (no secrets).
func StatusFile(configPath string) string {
	dir := filepath.Dir(strings.TrimSpace(configPath))
	if dir == "" || dir == "." {
		dir = LegacyConfigDir
	}
	return filepath.Join(dir, "status.json")
}

// Detect returns the install layout from env, existing files, or the running executable.
func Detect() Layout {
	if runtime.GOOS == "windows" {
		return windowsLayout()
	}
	return detectUnix()
}

func detectUnix() Layout {
	legacy := Layout{
		BinDir:    LegacyBinDir,
		BinName:   LegacyBinName,
		BinPath:   filepath.Join(LegacyBinDir, LegacyBinName),
		ConfigDir: LegacyConfigDir,
		Config:    filepath.Join(LegacyConfigDir, "config.json"),
		Service:   LegacyService,
		Name:      "legacy",
	}
	current := Layout{
		BinDir:    CurrentBinDir,
		BinName:   CurrentBinName,
		BinPath:   filepath.Join(CurrentBinDir, CurrentBinName),
		ConfigDir: CurrentConfigDir,
		Config:    filepath.Join(CurrentConfigDir, "config.json"),
		Service:   CurrentService,
		Name:      "current",
	}

	if exe, err := os.Executable(); err == nil {
		exe = resolved(exe)
		switch {
		case strings.HasPrefix(exe, LegacyBinDir+string(os.PathSeparator)) || filepath.Base(exe) == LegacyBinName:
			legacy.BinPath = exe
			legacy.BinDir = filepath.Dir(exe)
			legacy.Service = DetectServiceName(exe)
			if cfg := ConfigFile(); strings.Contains(cfg, "nerdyagent") && !strings.Contains(cfg, "nerdyrmm-agent") {
				legacy.ConfigDir = filepath.Dir(cfg)
				legacy.Config = cfg
			}
			return legacy
		case strings.HasPrefix(exe, CurrentBinDir+string(os.PathSeparator)) || filepath.Base(exe) == CurrentBinName:
			current.BinPath = exe
			current.Service = DetectServiceName(exe)
			if cfg := ConfigFile(); cfg != "" {
				current.ConfigDir = filepath.Dir(cfg)
				current.Config = cfg
			}
			return current
		}
	}

	if fileExists(legacy.Config) || fileExists(legacy.BinPath) || unitExists(LegacyService) {
		legacy.Service = DetectServiceName(legacy.BinPath)
		return legacy
	}
	if fileExists(current.Config) || fileExists(current.BinPath) || unitExists(CurrentService) {
		current.Service = DetectServiceName(current.BinPath)
		return current
	}
	return current
}

func windowsLayout() Layout {
	programFiles := strings.TrimSpace(os.Getenv("ProgramFiles"))
	if programFiles == "" {
		programFiles = `C:\Program Files`
	}
	programData := strings.TrimSpace(os.Getenv("ProgramData"))
	if programData == "" {
		programData = `C:\ProgramData`
	}
	cfg := ConfigFile()
	bin := filepath.Join(programFiles, "NerdyAgent", "nerdyagent.exe")
	if exe, err := os.Executable(); err == nil {
		bin = exe
	}
	return Layout{
		BinDir:    filepath.Dir(bin),
		BinName:   filepath.Base(bin),
		BinPath:   bin,
		ConfigDir: filepath.Dir(cfg),
		Config:    cfg,
		Service:   "NerdyAgent",
		Name:      "windows",
	}
}

var (
	serviceOnce sync.Once
	serviceName string
)

// CachedServiceName remembers DetectServiceName(os.Executable()) for the process.
func CachedServiceName() string {
	serviceOnce.Do(func() {
		exe, _ := os.Executable()
		serviceName = DetectServiceName(exe)
	})
	return serviceName
}

// DetectServiceName returns the systemd/Windows service that owns exePath.
func DetectServiceName(exePath string) string {
	if runtime.GOOS == "windows" {
		return "NerdyAgent"
	}
	exePath = resolved(exePath)
	base := filepath.Base(exePath)
	if strings.Contains(exePath, LegacyBinDir) || base == LegacyBinName {
		return LegacyService
	}
	if base == CurrentBinName {
		return CurrentService
	}
	for _, name := range []string{LegacyService, CurrentService} {
		if unitMentions(name, exePath) {
			return name
		}
	}
	for _, name := range []string{LegacyService, CurrentService} {
		if unitExists(name) {
			return name
		}
	}
	return LegacyService
}

func unitExists(name string) bool {
	if name == "" || runtime.GOOS == "windows" {
		return false
	}
	cmd := exec.Command("systemctl", "status", name+".service")
	if err := cmd.Run(); err == nil {
		return true
	}
	// systemctl status returns non-zero for inactive-but-installed units.
	show := exec.Command("systemctl", "show", "-p", "LoadState", "--value", name+".service")
	out, err := show.Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "loaded"
}

func unitMentions(name, exePath string) bool {
	if name == "" || exePath == "" || runtime.GOOS == "windows" {
		return false
	}
	out, err := exec.Command("systemctl", "show", "-p", "ExecStart", "--value", name+".service").Output()
	if err != nil {
		return false
	}
	text := strings.TrimSpace(string(out))
	if text == "" {
		return false
	}
	base := filepath.Base(exePath)
	return strings.Contains(text, exePath) || (base != "" && strings.Contains(text, base))
}

func fileExists(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

func resolved(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return path
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	if ev, err := filepath.EvalSymlinks(path); err == nil {
		return ev
	}
	return path
}
