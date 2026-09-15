package status

import (
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
		Version:       "0.4.0",
		LastCheckinOK: true,
		ServerURL:     "https://rmm-api.nerdytech.dev",
		DeviceID:      42,
		Service:       "nerdyrmm-agent",
		ConfigPath:    cfg,
		TokenMasked:   MaskToken("super-secret"),
	}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "status.json"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	if strings.Contains(text, "super-secret") || strings.Contains(text, "enroll") {
		t.Fatalf("status.json leaked a secret field: %s", text)
	}
	if strings.Contains(text, `"token"`) || strings.Contains(text, "enrollmentToken") {
		t.Fatalf("status.json included a credential key: %s", text)
	}
	got, err := ReadSnapshot(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != "0.4.0" || got.DeviceID != 42 || !got.Running {
		t.Fatalf("snapshot = %+v", got)
	}
	if got.ServerURL != "https://rmm-api.nerdytech.dev" {
		t.Fatalf("ReadSnapshot server = %q", got.ServerURL)
	}
	if got.TokenMasked == "super-secret" || !strings.HasSuffix(got.TokenMasked, "cret") {
		t.Fatalf("tokenMasked = %q", got.TokenMasked)
	}
}

func TestHubPreservesTunnelAcrossCheckin(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.json")
	h := NewHub(cfg)
	h.SetIdentity("0.4.0", "https://example.test", "nerdyrmm-agent", cfg, "asgard", "Ubuntu", MaskToken("abcd1234ffff"), 7)
	h.SetTunnelOnline(true)
	h.SessionOpened("ssh", "sess-1", "Browser SSH")
	h.SetCheckin(true, "", "0.4.0")
	got := h.Snapshot()
	if !got.TunnelOnline || len(got.ActiveSessions) != 1 || got.ActiveSessions[0].Type != "ssh" {
		t.Fatalf("lost tunnel state: %+v", got)
	}
	if !got.LastCheckinOK || got.Hostname != "asgard" {
		t.Fatalf("checkin/identity missing: %+v", got)
	}
	h.SessionClosed("sess-1")
	got = h.Snapshot()
	if len(got.ActiveSessions) != 0 || got.LastSession == nil || got.LastSession.Kind != "close" {
		t.Fatalf("close = %+v", got)
	}
}

func TestMaskToken(t *testing.T) {
	if MaskToken("ab") != "••••" {
		t.Fatalf("short")
	}
	m := MaskToken("device-token-9999")
	if strings.Contains(m, "device-token") || !strings.HasSuffix(m, "9999") {
		t.Fatalf("masked %q", m)
	}
}

func TestRedactSecret(t *testing.T) {
	got := RedactSecret("dial ws://x?token=sekrit failed", "sekrit")
	if strings.Contains(got, "sekrit") {
		t.Fatalf("got %q", got)
	}
}
