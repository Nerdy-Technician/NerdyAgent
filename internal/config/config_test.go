package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadAcceptsStringDuration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{
  "serverUrl": "https://example.test",
  "checkinEvery": "60s"
}`), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.CheckinEvery != time.Minute {
		t.Fatalf("CheckinEvery = %s, want 1m0s", cfg.CheckinEvery)
	}
}

func TestLoadAcceptsNumericDuration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{
  "serverUrl": "https://example.test",
  "checkinEvery": 60000000000
}`), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.CheckinEvery != time.Minute {
		t.Fatalf("CheckinEvery = %s, want 1m0s", cfg.CheckinEvery)
	}
}
