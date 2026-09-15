package tray

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nerdyrmm/agent/internal/status"
)

func TestLoadViewUsesSnapshotWithoutSecrets(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.json")
	if err := os.WriteFile(cfg, []byte(`{"token":"secret-token","serverUrl":"https://rmm-api.nerdytech.dev"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NRMM_AGENT_CONFIG", cfg)
	if err := status.WriteSnapshot(cfg, status.Snapshot{
		Version:       "0.3.10",
		LastCheckinOK: true,
		LastCheckin:   "2026-09-14T12:00:00Z",
		ServerURL:     "https://rmm-api.nerdytech.dev",
		DeviceID:      7,
		Service:       "nerdyrmm-agent",
	}); err != nil {
		t.Fatal(err)
	}
	v := loadView()
	if !v.Online || !strings.Contains(v.Title, "0.3.10") {
		t.Fatalf("view = %+v", v)
	}
	blob := v.Title + v.Status + v.Detail
	if strings.Contains(blob, "secret-token") {
		t.Fatal("tray view leaked token")
	}
}

func TestIconPixmapSize(t *testing.T) {
	w, h, data := iconPixmap(true)
	if w != iconSize || h != iconSize || len(data) != iconSize*iconSize*4 {
		t.Fatalf("icon %dx%d len=%d", w, h, len(data))
	}
}
