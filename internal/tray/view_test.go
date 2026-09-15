package tray

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nerdyrmm/agent/internal/status"
)

func TestLoadViewTooltipHasNoConnectionDetails(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.json")
	if err := os.WriteFile(cfg, []byte(`{"token":"secret-token","serverUrl":"https://rmm-api.nerdytech.dev"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NRMM_AGENT_CONFIG", cfg)
	if err := status.WriteSnapshot(cfg, status.Snapshot{
		Version:       "0.4.0",
		LastCheckinOK: true,
		LastCheckin:   "2026-09-14T12:00:00Z",
		ServerURL:     "https://rmm-api.nerdytech.dev",
		DeviceID:      7,
		Service:       "nerdyrmm-agent",
		ConfigPath:    cfg,
		Hostname:      "asgard",
		OS:            "Ubuntu",
		TokenMasked:   status.MaskToken("secret-token"),
	}); err != nil {
		t.Fatal(err)
	}
	v := loadView()
	if !v.Online {
		t.Fatalf("view = %+v", v)
	}
	if v.Tooltip != "NerdyRMM Agent — Online" {
		t.Fatalf("tooltip = %q", v.Tooltip)
	}
	if tooltipContainsSecrets(v.Tooltip) {
		t.Fatalf("tooltip leaked connection details: %q", v.Tooltip)
	}
	if strings.Contains(v.Tooltip, "secret-token") || strings.Contains(v.Tooltip, "rmm-api") {
		t.Fatal("tooltip leaked server or token")
	}
	if v.Hostname != "asgard" || v.Version != "0.4.0" {
		t.Fatalf("popup fields missing: %+v", v)
	}
}

func TestTooltipUpdateAvailable(t *testing.T) {
	statusLine, tip := highLevelStatus(true, true)
	if statusLine != "Update available" || tip != "NerdyRMM Agent — Update available" {
		t.Fatalf("%s / %s", statusLine, tip)
	}
	if tooltipContainsSecrets(tip) {
		t.Fatal(tip)
	}
}

func TestIconPixmapSize(t *testing.T) {
	w, h, data := iconPixmap(true, false)
	if w != iconSize || h != iconSize || len(data) != iconSize*iconSize*4 {
		t.Fatalf("icon %dx%d len=%d", w, h, len(data))
	}
}

func TestPrefsNotInAgentConfig(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	p := prefsPath()
	if strings.Contains(p, "config.json") || strings.Contains(p, "nerdyrmm-agent/config") {
		t.Fatalf("prefs path looks like agent config: %s", p)
	}
	if err := savePrefs(Prefs{NotifyTechnicianConnect: true}); err != nil {
		t.Fatal(err)
	}
	got := loadPrefs()
	if !got.NotifyTechnicianConnect {
		t.Fatal("prefs not persisted")
	}
}
