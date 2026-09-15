package tray

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/nerdyrmm/agent/internal/paths"
	"github.com/nerdyrmm/agent/internal/status"
)

type view struct {
	Title          string
	Tooltip        string
	Status         string
	Detail         string
	Online         bool
	UpdateAvail    bool
	TunnelOnline   bool
	DocsURL        string
	WebURL         string
	StatusFn       string
	Version        string
	Hostname       string
	OS             string
	LastCheckin    string
	LastCheckinRel string
	ServerURL      string
	DeviceID       int64
	Service        string
	ConfigPath     string
	TokenMasked    string
	Sessions       []status.Session
	Tickets        []status.Ticket
	TicketsNote    string
	Chat           []status.ChatLine
	IPCSocket      string
	IPCToken       string
	NotifyPref     bool
	LastSession    *status.SessionEvt
}

func loadView() view {
	cfg := paths.ConfigFile()
	v := view{
		Title:      "NerdyRMM Agent",
		Tooltip:    "NerdyRMM Agent — Offline",
		Status:     "Offline",
		Detail:     "Waiting for agent status.json",
		DocsURL:    "https://github.com/Nerdy-Technician/NerdyAgent",
		StatusFn:   paths.StatusFile(cfg),
		ConfigPath: cfg,
		NotifyPref: loadPrefs().NotifyTechnicianConnect,
	}
	snap, err := status.ReadSnapshot(cfg)
	if err != nil {
		if os.IsNotExist(err) {
			v.Status = "Offline"
			v.Tooltip = "NerdyRMM Agent — Offline"
			v.Detail = "status.json not found — is the agent service running?"
			return v
		}
		v.Status = "Offline"
		v.Tooltip = "NerdyRMM Agent — Offline"
		v.Detail = "status.json unreadable"
		return v
	}
	v.Version = snap.Version
	v.Hostname = snap.Hostname
	v.OS = snap.OS
	v.LastCheckin = snap.LastCheckin
	v.LastCheckinRel = relativeTime(snap.LastCheckin)
	v.ServerURL = snap.ServerURL
	v.DeviceID = snap.DeviceID
	v.Service = snap.Service
	if snap.ConfigPath != "" {
		v.ConfigPath = snap.ConfigPath
	}
	v.TokenMasked = snap.TokenMasked
	v.Sessions = snap.ActiveSessions
	v.Tickets = snap.Tickets
	v.TicketsNote = snap.TicketsNote
	v.Chat = snap.Chat
	v.IPCSocket = snap.IPCSocket
	v.IPCToken = snap.IPCToken
	v.TunnelOnline = snap.TunnelOnline
	v.LastSession = snap.LastSession
	v.WebURL = strings.TrimRight(snap.ServerURL, "/")
	v.UpdateAvail = snap.UpdateAvailable
	v.Online = snap.LastCheckinOK && snap.Running
	v.Status, v.Tooltip = highLevelStatus(v.Online, v.UpdateAvail)
	return v
}

func highLevelStatus(online, updateAvail bool) (statusLine, tooltip string) {
	switch {
	case updateAvail:
		return "Update available", "NerdyRMM Agent — Update available"
	case online:
		return "Online", "NerdyRMM Agent — Online"
	default:
		return "Offline", "NerdyRMM Agent — Offline"
	}
}

func relativeTime(rfc3339 string) string {
	s := strings.TrimSpace(rfc3339)
	if s == "" {
		return "never"
	}
	ts, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return s
	}
	d := time.Since(ts)
	if d < 0 {
		d = 0
	}
	switch {
	case d < 10*time.Second:
		return "just now"
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%d min ago", int(d.Minutes()))
	case d < 24*time.Hour:
		h := int(d.Hours())
		if h == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", h)
	default:
		days := int(d.Hours() / 24)
		if days == 1 {
			return "1 day ago"
		}
		return fmt.Sprintf("%d days ago", days)
	}
}

func tooltipContainsSecrets(tooltip string) bool {
	low := strings.ToLower(tooltip)
	needles := []string{
		"http://", "https://", "ws://", "wss://",
		"serverurl", "server url", "deviceid", "device id", "device ",
		"token", "enroll", "config.json", "config path", "/etc/",
		"status.json",
	}
	for _, n := range needles {
		if strings.Contains(low, n) {
			return true
		}
	}
	return false
}
