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
	Title    string
	Status   string
	Detail   string
	Online   bool
	DocsURL  string
	StatusFn string
	LogFn    string
}

func loadView() view {
	cfg := paths.ConfigFile()
	v := view{
		Title:    "NerdyRMM Agent",
		Status:   "Waiting for agent status",
		Detail:   "The tray does not store device credentials. Status is read from status.json.",
		DocsURL:  "https://github.com/Nerdy-Technician/NerdyAgent",
		StatusFn: paths.StatusFile(cfg),
		LogFn:    strings.TrimSuffix(cfg, "config.json") + "status.log",
	}
	snap, err := status.ReadSnapshot(cfg)
	if err != nil {
		if os.IsNotExist(err) {
			v.Status = "Agent status unavailable"
			v.Detail = "status.json not found — is nerdyrmm-agent.service running?"
			return v
		}
		v.Status = "Agent status unreadable"
		v.Detail = err.Error()
		return v
	}
	if strings.TrimSpace(snap.Version) != "" {
		v.Title = "NerdyRMM Agent " + snap.Version
	}
	v.Online = snap.LastCheckinOK && snap.Running
	if v.Online {
		v.Status = "Online — last check-in ok"
	} else if snap.LastError != "" {
		v.Status = "Check-in failed"
		v.Detail = snap.LastError
	} else {
		v.Status = "Agent running"
	}
	parts := []string{}
	if snap.ServerURL != "" {
		parts = append(parts, "Server: "+snap.ServerURL)
	}
	if snap.DeviceID > 0 {
		parts = append(parts, fmt.Sprintf("Device %d", snap.DeviceID))
	}
	if snap.Service != "" {
		parts = append(parts, "Service: "+snap.Service)
	}
	if snap.LastCheckin != "" {
		if ts, err := time.Parse(time.RFC3339, snap.LastCheckin); err == nil {
			parts = append(parts, "Check-in: "+ts.Local().Format(time.Kitchen))
		} else {
			parts = append(parts, "Check-in: "+snap.LastCheckin)
		}
	}
	if len(parts) > 0 {
		v.Detail = strings.Join(parts, "\n")
	}
	return v
}
