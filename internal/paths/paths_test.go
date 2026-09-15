package paths

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestConfigCandidatesRespectsEnv(t *testing.T) {
	t.Setenv("NRMM_AGENT_CONFIG", "/tmp/custom-agent.json")
	got := ConfigCandidates()
	if len(got) != 1 || got[0] != "/tmp/custom-agent.json" {
		t.Fatalf("ConfigCandidates() = %v", got)
	}
}

func TestConfigFilePrefersExisting(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "config.json")
	if err := os.WriteFile(existing, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NRMM_AGENT_CONFIG", existing)
	if got := ConfigFile(); got != existing {
		t.Fatalf("ConfigFile() = %q, want %q", got, existing)
	}
}

func TestStatusFileBesideConfig(t *testing.T) {
	got := StatusFile("/etc/nerdyrmm-agent/config.json")
	want := "/etc/nerdyrmm-agent/status.json"
	if runtime.GOOS == "windows" {
		want = filepath.Join(`\etc\nerdyrmm-agent`, "status.json")
		got = StatusFile(filepath.Join(`\etc\nerdyrmm-agent`, "config.json"))
	}
	if got != want {
		t.Fatalf("StatusFile() = %q, want %q", got, want)
	}
}

func TestDetectServiceNameHeuristics(t *testing.T) {
	if runtime.GOOS == "windows" {
		if got := DetectServiceName(`C:\Program Files\NerdyAgent\nerdyagent.exe`); got != "NerdyAgent" {
			t.Fatalf("windows service = %q", got)
		}
		return
	}
	if got := DetectServiceName("/opt/nerdyrmm/nerdyrmm-agent"); got != LegacyService {
		t.Fatalf("legacy service = %q, want %q", got, LegacyService)
	}
	if got := DetectServiceName("/usr/local/bin/nerdyagent"); got != CurrentService {
		t.Fatalf("current service = %q, want %q", got, CurrentService)
	}
}
