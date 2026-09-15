package status

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteSnapshotOmitsSecrets(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.json")
	if err := os.WriteFile(cfg, []byte(`{"token":"super-secret","enrollmentToken":"enroll"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteSnapshot(cfg, Snapshot{
		Version:       "0.3.10",
		LastCheckinOK: true,
		ServerURL:     "https://rmm-api.nerdytech.dev",
		DeviceID:      42,
		Service:       "nerdyrmm-agent",
		ConfigPath:    cfg,
	}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "status.json"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	if strings.Contains(text, "super-secret") || strings.Contains(text, "enroll") || strings.Contains(text, "token") {
		t.Fatalf("status.json leaked a secret field: %s", text)
	}
	var snap Snapshot
	if err := json.Unmarshal(b, &snap); err != nil {
		t.Fatal(err)
	}
	if snap.Version != "0.3.10" || snap.DeviceID != 42 || !snap.Running {
		t.Fatalf("snapshot = %+v", snap)
	}
	got, err := ReadSnapshot(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got.ServerURL != "https://rmm-api.nerdytech.dev" {
		t.Fatalf("ReadSnapshot server = %q", got.ServerURL)
	}
}
