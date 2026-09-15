package tray

import (
	"bytes"
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
		Version:       "0.3.10.2",
		LastCheckinOK: true,
		LastCheckin:   "2026-09-14T12:00:00Z",
		ServerURL:     "https://rmm-api.nerdytech.dev",
		DeviceID:      7,
		Service:       "nerdyrmm-agent",
	}); err != nil {
		t.Fatal(err)
	}
	v := loadView()
	if !v.Online || !strings.Contains(v.Title, "0.3.10.2") {
		t.Fatalf("view = %+v", v)
	}
	blob := v.Title + v.Status + v.Detail
	if strings.Contains(blob, "secret-token") {
		t.Fatal("tray view leaked token")
	}
}

func TestIconPixmapFaviconSizesAndARGB(t *testing.T) {
	if len(faviconPNG) < 100 || !bytes.HasPrefix(faviconPNG, []byte{0x89, 'P', 'N', 'G'}) {
		t.Fatalf("embedded favicon.png missing or not PNG (len=%d)", len(faviconPNG))
	}
	pms := iconPixmaps(true)
	if len(pms) < 2 {
		t.Fatalf("expected 22 and 32 px pixmaps, got %d", len(pms))
	}
	seen := map[int32]bool{}
	for _, p := range pms {
		seen[p.Width] = true
		if p.Width != p.Height {
			t.Fatalf("pixmap not square: %dx%d", p.Width, p.Height)
		}
		want := int(p.Width * p.Height * 4)
		if len(p.ARGB) != want {
			t.Fatalf("ARGB length %d want %d", len(p.ARGB), want)
		}
	}
	if !seen[22] || !seen[32] {
		t.Fatalf("missing sizes: %v", seen)
	}

	_, _, on := iconPixmap(true)
	_, _, off := iconPixmap(false)
	if bytes.Equal(on, off) {
		t.Fatal("online/offline pixmaps should differ by the status badge")
	}

	// Network-endian ARGB32: fully transparent pixels are 00 00 00 00, not RGBA.
	// After square-crop + backdrop punch, corners of the 32px icon should be transparent.
	w, h, data := iconPixmap(true)
	if w != 32 || h != 32 {
		t.Fatalf("default pixmap %dx%d", w, h)
	}
	cornerA := data[0]
	if cornerA != 0 {
		t.Fatalf("top-left alpha=%d, want 0 (transparent padding after crop)", cornerA)
	}

	// A red-ish pixel from the R should have A high and R > B if format is A,R,G,B
	// (not B,G,R,A which would put blue in the R slot).
	var foundNR bool
	for i := 0; i < len(on); i += 4 {
		a, r, g, b := on[i], on[i+1], on[i+2], on[i+3]
		if a > 200 && r > 150 && r > g && r > b {
			foundNR = true
			break
		}
	}
	if !foundNR {
		t.Fatal("did not find a red ARGB pixel from the NR mark; byte order may be wrong")
	}
}

func TestInstallThemeIconsWritesPNG(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_DATA_HOME", filepath.Join(dir, "share"))
	path, theme, err := InstallThemeIcons()
	if err != nil {
		t.Fatal(err)
	}
	if path == "" || !strings.Contains(path, "nerdyrmm-agent") {
		t.Fatalf("path=%q theme=%q", path, theme)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(b, []byte{0x89, 'P', 'N', 'G'}) {
		t.Fatal("installed icon is not PNG")
	}
}

func TestFingerprintIgnoresIdenticalViews(t *testing.T) {
	a := view{Title: "NerdyRMM Agent", Status: "Online — last check-in ok", Detail: "Server: x", Online: true}
	b := a
	if a.fingerprint() != b.fingerprint() {
		t.Fatal("identical views must fingerprint equal")
	}
	b.Online = false
	if a.fingerprint() == b.fingerprint() {
		t.Fatal("online change must change fingerprint")
	}
}
