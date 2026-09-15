package tray

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// Prefs are user-local tray settings. Never stored in agent config.json.
type Prefs struct {
	NotifyTechnicianConnect bool `json:"notifyTechnicianConnect"`
}

var (
	prefsMu   sync.Mutex
	lastPrefs Prefs
)

func prefsPath() string {
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		dir = filepath.Join(os.TempDir(), "nerdyrmm-agent")
	} else {
		dir = filepath.Join(dir, "nerdyrmm-agent")
	}
	return filepath.Join(dir, "prefs.json")
}

func loadPrefs() Prefs {
	prefsMu.Lock()
	defer prefsMu.Unlock()
	b, err := os.ReadFile(prefsPath())
	if err != nil {
		return Prefs{}
	}
	var p Prefs
	if json.Unmarshal(b, &p) != nil {
		return Prefs{}
	}
	lastPrefs = p
	return p
}

func savePrefs(p Prefs) error {
	path := prefsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	prefsMu.Lock()
	lastPrefs = p
	prefsMu.Unlock()
	return os.WriteFile(path, b, 0o600)
}
