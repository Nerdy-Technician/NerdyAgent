package updater

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompareVersionsFourPart(t *testing.T) {
	if CompareVersions("0.3.9.5", "0.3.10") >= 0 {
		t.Fatalf("0.3.9.5 should be older than 0.3.10")
	}
	if CompareVersions("0.3.10", "0.3.10") != 0 {
		t.Fatalf("0.3.10 should equal itself")
	}
	if CompareVersions("0.3.10", "0.3.10.1") >= 0 {
		t.Fatalf("0.3.10 should be older than 0.3.10.1")
	}
	if CompareVersions("0.3.10.1", "0.3.10.2") >= 0 {
		t.Fatalf("0.3.10.1 should be older than 0.3.10.2")
	}
}

func TestParseReleaseJSONFlexible(t *testing.T) {
	body := []byte(`{"latestVersion":"v0.3.10","binaryUrl":"https://example.test/a","sha256":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`)
	r, err := parseReleaseJSON(body)
	if err != nil {
		t.Fatal(err)
	}
	if r.Version != "0.3.10" || r.BinaryURL == "" || r.SHA256 == "" {
		t.Fatalf("parsed %+v", r)
	}
}

func TestParseSHA256SUMS(t *testing.T) {
	m := parseSHA256SUMS("deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef  nerdyrmm-agent-linux-amd64\n")
	if m["nerdyrmm-agent-linux-amd64"] != "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef" {
		t.Fatalf("map = %#v", m)
	}
}

func TestDiscoverPicksNewestOfServerAndGitHub(t *testing.T) {
	wantName := AgentBinaryFilename()
	elf := fakeELF(70 * 1024)
	sum := sha256.Sum256(elf)
	digest := "sha256:" + hex.EncodeToString(sum[:])

	mux := http.NewServeMux()
	mux.HandleFunc("/api/agent/latest-version", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			http.Error(w, `{"error":"missing token"}`, http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"version": "0.3.7", "binaryUrl": "http://ignored/old"})
	})
	mux.HandleFunc("/repos/Nerdy-Technician/NerdyAgent/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"tag_name": "v0.3.10",
			"assets": []map[string]string{{
				"name":                 wantName,
				"browser_download_url": "http://example.invalid/" + wantName,
				"digest":               digest,
			}},
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	prev := githubLatestURL
	githubLatestURL = func(string) string { return srv.URL + "/repos/Nerdy-Technician/NerdyAgent/releases/latest" }
	t.Cleanup(func() { githubLatestURL = prev })

	rel, err := Discover(context.Background(), "0.3.9.5", DiscoverConfig{
		ServerURL:  srv.URL,
		Token:      "tok",
		GitHubRepo: "Nerdy-Technician/NerdyAgent",
		HTTPClient: srv.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if rel.Version != "0.3.10" {
		t.Fatalf("version = %q source=%q", rel.Version, rel.Source)
	}
	if rel.Source != "github" {
		t.Fatalf("expected github to win over stale server 0.3.7, got %q", rel.Source)
	}
}

func TestBumpAgentVersionPreservesAuth(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	orig := `{
  "serverUrl": "https://rmm-api.nerdytech.dev",
  "deviceId": 99,
  "token": "keep-me",
  "enrollmentToken": "",
  "checkinEvery": "60s",
  "agentVersion": "0.3.9.5"
}
`
	if err := os.WriteFile(path, []byte(orig), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := bumpAgentVersion(path, "0.3.10"); err != nil {
		t.Fatal(err)
	}
	var m map[string]interface{}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if m["token"] != "keep-me" || m["serverUrl"] != "https://rmm-api.nerdytech.dev" {
		t.Fatalf("auth fields changed: %s", b)
	}
	if int(m["deviceId"].(float64)) != 99 {
		t.Fatalf("deviceId changed: %v", m["deviceId"])
	}
	if m["agentVersion"] != "0.3.10" {
		t.Fatalf("agentVersion = %v", m["agentVersion"])
	}
}

func TestVerifyBinaryRejectsHTMLAndBadChecksum(t *testing.T) {
	if err := verifyBinary([]byte("<!doctype html>not-an-agent"), ""); err == nil {
		t.Fatal("expected HTML reject")
	}
	elf := fakeELF(70 * 1024)
	if err := verifyBinary(elf, strings.Repeat("ab", 32)); err == nil {
		t.Fatal("expected checksum mismatch")
	}
	sum := sha256.Sum256(elf)
	if err := verifyBinary(elf, hex.EncodeToString(sum[:])); err != nil {
		t.Fatal(err)
	}
}

func TestFailedDownloadDoesNotTouchBinary(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "nerdyrmm-agent")
	if err := os.WriteFile(exe, fakeELF(70*1024), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(dir, "config.json")
	if err := os.WriteFile(cfg, []byte(`{"token":"keep","deviceId":1,"agentVersion":"0.3.9.5"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusBadGateway)
	}))
	t.Cleanup(srv.Close)

	// Apply uses os.Executable(), so we only assert download/verify helpers here.
	_, _, err := downloadBytes(context.Background(), srv.Client(), []string{srv.URL + "/missing"})
	if err == nil {
		t.Fatal("expected download failure")
	}
	b, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	if !hasExecMagic(b) {
		t.Fatal("binary was damaged")
	}
	cfgBytes, _ := os.ReadFile(cfg)
	if !strings.Contains(string(cfgBytes), `"token":"keep"`) {
		t.Fatalf("config changed: %s", cfgBytes)
	}
}

func fakeELF(n int) []byte {
	b := make([]byte, n)
	copy(b, []byte{0x7f, 'E', 'L', 'F', 2, 1, 1})
	for i := 8; i < n; i++ {
		b[i] = byte(i)
	}
	return b
}
