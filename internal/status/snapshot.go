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
// Never include token or enrollmentToken in cleartext. tokenMasked is last-4 only.
type Snapshot struct {
	Version         string      `json:"version"`
	Running         bool        `json:"running"`
	LastCheckin     string      `json:"lastCheckin,omitempty"`
	LastCheckinOK   bool        `json:"lastCheckinOK"`
	LastError       string      `json:"lastError,omitempty"`
	ServerURL       string      `json:"serverUrl,omitempty"`
	DeviceID        int64       `json:"deviceId,omitempty"`
	Service         string      `json:"service,omitempty"`
	ConfigPath      string      `json:"configPath,omitempty"`
	Hostname        string      `json:"hostname,omitempty"`
	OS              string      `json:"os,omitempty"`
	TokenMasked     string      `json:"tokenMasked,omitempty"`
	TunnelOnline    bool        `json:"tunnelOnline"`
	ActiveSessions  []Session   `json:"activeSessions,omitempty"`
	Tickets         []Ticket    `json:"tickets,omitempty"`
	TicketsNote     string      `json:"ticketsNote,omitempty"`
	Chat            []ChatLine  `json:"chat,omitempty"`
	IPCSocket       string      `json:"ipcSocket,omitempty"`
	IPCToken        string      `json:"ipcToken,omitempty"`
	LatestVersion   string      `json:"latestVersion,omitempty"`
	UpdateAvailable bool        `json:"updateAvailable"`
	LastSession     *SessionEvt `json:"lastSession,omitempty"`
	UpdatedAt       string      `json:"updatedAt"`
}

// Session is an active remote control channel (no payloads / secrets).
type Session struct {
	ID        string `json:"id"`
	Type      string `json:"type"` // ssh, desktop, chat, tcp
	StartedAt string `json:"startedAt"`
	Label     string `json:"label,omitempty"`
}

// SessionEvt is the most recent open/close for tray technician-connect notify.
type SessionEvt struct {
	Kind string `json:"kind"` // open | close
	Type string `json:"type"`
	ID   string `json:"id,omitempty"`
	At   string `json:"at"`
}

// Ticket is a safe summary for the tray Tickets tab.
type Ticket struct {
	ID        int64  `json:"id"`
	Title     string `json:"title"`
	Status    string `json:"status,omitempty"`
	UpdatedAt string `json:"updatedAt,omitempty"`
}

// ChatLine is a short text chat message (never credentials).
type ChatLine struct {
	SessionID string `json:"sessionId"`
	From      string `json:"from"` // technician | user
	Text      string `json:"text"`
	At        string `json:"at"`
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

// MaskToken returns a display-only token preview (never the full secret).
func MaskToken(token string) string {
	t := strings.TrimSpace(token)
	if t == "" {
		return ""
	}
	if len(t) <= 4 {
		return "••••"
	}
	return "••••••••" + t[len(t)-4:]
}

// RedactSecret strips a known secret from log/error text.
func RedactSecret(s, secret string) string {
	if strings.TrimSpace(secret) == "" || s == "" {
		return s
	}
	return strings.ReplaceAll(s, secret, "***")
}
