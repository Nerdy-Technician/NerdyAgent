package status

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Snapshot is a secret-free view of agent health for the tray and operators.
// Never include token or enrollmentToken.
type Snapshot struct {
	Version       string `json:"version"`
	Running       bool   `json:"running"`
	LastCheckin   string `json:"lastCheckin,omitempty"`
	LastCheckinOK bool   `json:"lastCheckinOK"`
	LastError     string `json:"lastError,omitempty"`
	ServerURL     string `json:"serverUrl,omitempty"`
	DeviceID      int64  `json:"deviceId,omitempty"`
	Service       string `json:"service,omitempty"`
	ConfigPath    string `json:"configPath,omitempty"`
	UpdatedAt     string `json:"updatedAt"`
}

var snapshotMu sync.Mutex

// WriteSnapshot atomically replaces status.json next to the config file.
func WriteSnapshot(configPath string, snap Snapshot) error {
	path := snapshotPath(configPath)
	if path == "" {
		return nil
	}
	if strings.TrimSpace(snap.UpdatedAt) == "" {
		snap.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	snap.Running = true
	b, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	snapshotMu.Lock()
	defer snapshotMu.Unlock()
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// ReadSnapshot loads the last published status file.
func ReadSnapshot(configPath string) (Snapshot, error) {
	var snap Snapshot
	path := strings.TrimSpace(configPath)
	if path == "" {
		return snap, os.ErrNotExist
	}
	if !strings.HasSuffix(path, "status.json") {
		path = snapshotPath(path)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return snap, err
	}
	if err := json.Unmarshal(b, &snap); err != nil {
		return snap, err
	}
	return snap, nil
}

func snapshotPath(configPath string) string {
	configPath = strings.TrimSpace(configPath)
	if configPath == "" {
		return ""
	}
	if strings.HasSuffix(configPath, "status.json") {
		return configPath
	}
	return filepath.Join(filepath.Dir(configPath), "status.json")
}
