package tray

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nerdyrmm/agent/internal/status"
)

func TestServeStatusOmitsClearToken(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.json")
	if err := os.WriteFile(cfg, []byte(`{"token":"clear-secret-token"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NRMM_AGENT_CONFIG", cfg)
	if err := status.WriteSnapshot(cfg, status.Snapshot{
		Version:       "0.4.0",
		LastCheckinOK: true,
		ServerURL:     "https://rmm-api.nerdytech.dev",
		DeviceID:      3,
		TokenMasked:   status.MaskToken("clear-secret-token"),
		TunnelOnline:  true,
		ActiveSessions: []status.Session{{
			ID: "s1", Type: "chat", StartedAt: "2026-09-15T00:00:00Z", Label: "Chat",
		}},
	}); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	serveStatus(rec, httptest.NewRequest(http.MethodGet, "/api/status", nil))
	if rec.Code != 200 {
		t.Fatalf("code %d body %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, "clear-secret-token") {
		t.Fatalf("api leaked token: %s", body)
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["tooltip"] != "Online" {
		t.Fatalf("tooltip %v", payload["tooltip"])
	}
	if payload["tunnelOnline"] != true {
		t.Fatalf("tunnel %v", payload["tunnelOnline"])
	}
}

func TestRelativeTimeNever(t *testing.T) {
	if relativeTime("") != "never" {
		t.Fatal(relativeTime(""))
	}
}
