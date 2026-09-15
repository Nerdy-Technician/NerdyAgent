package tray

import (
	"strings"
	"testing"

	"github.com/nerdyrmm/agent/internal/status"
)

func TestStatusPanelTextHasNoConnectionSecrets(t *testing.T) {
	v := view{
		Status:         "Online",
		LastCheckinRel: "2 min ago",
		Version:        "0.4.0",
		Hostname:       "asgard",
		OS:             "Ubuntu 24.04",
		TunnelOnline:   true,
		ServerURL:      "https://rmm-api.nerdytech.dev",
		DeviceID:       7,
		ConfigPath:     "/etc/nerdyrmm-agent/config.json",
		TokenMasked:    "••••oken",
		Service:        "nerdyrmm-agent",
	}
	got := statusPanelText(v)
	for _, needle := range []string{"rmm-api", "secret", "token", "config.json", "Device", "https://", "nerdyrmm-agent/config"} {
		if strings.Contains(got, needle) {
			t.Fatalf("status panel leaked %q: %s", needle, got)
		}
	}
	if !strings.Contains(got, "Online") || !strings.Contains(got, "asgard") || !strings.Contains(got, "0.4.0") {
		t.Fatalf("status panel missing fields: %s", got)
	}
}

func TestConnectionPanelMasksToken(t *testing.T) {
	v := view{
		ServerURL:   "https://rmm-api.nerdytech.dev",
		DeviceID:    7,
		ConfigPath:  "/etc/nerdyrmm-agent/config.json",
		Service:     "nerdyrmm-agent",
		TokenMasked: status.MaskToken("secret-token-do-not-leak"),
	}
	got := connectionPanelText(v)
	if strings.Contains(got, "secret-token-do-not-leak") {
		t.Fatal("connection panel showed cleartext token")
	}
	if !strings.Contains(got, "https://rmm-api.nerdytech.dev") || !strings.Contains(got, "7") {
		t.Fatalf("connection panel missing identity: %s", got)
	}
	if !strings.Contains(got, "••••") {
		t.Fatalf("expected masked token: %s", got)
	}
}

func TestLaunchBrowserAppRejectsEmpty(t *testing.T) {
	if launchBrowserApp("") {
		t.Fatal("empty url should fail")
	}
}
