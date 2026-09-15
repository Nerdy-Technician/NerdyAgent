package tray

import (
	"bytes"
	"encoding/binary"
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
		Version:       "0.3.10.1",
		LastCheckinOK: true,
		LastCheckin:   "2026-09-14T12:00:00Z",
		ServerURL:     "https://rmm-api.nerdytech.dev",
		DeviceID:      7,
		Service:       "nerdyrmm-agent",
	}); err != nil {
		t.Fatal(err)
	}
	v := loadView()
	if !v.Online || !strings.Contains(v.Title, "0.3.10.1") {
		t.Fatalf("view = %+v", v)
	}
	blob := v.Title + v.Status + v.Detail
	if strings.Contains(blob, "secret-token") {
		t.Fatal("tray view leaked token")
	}
	if v.fingerprint() == "" || v.fingerprint() != v.fingerprint() {
		t.Fatal("fingerprint unstable")
	}
}

func TestIconPixmapUsesEmbeddedFavicon(t *testing.T) {
	if len(faviconPNG) < 100 || !bytes.HasPrefix(faviconPNG, []byte{0x89, 'P', 'N', 'G'}) {
		t.Fatalf("embedded favicon.png missing or not PNG (len=%d)", len(faviconPNG))
	}
	w, h, on := iconPixmap(true)
	_, _, off := iconPixmap(false)
	if w != iconSize || h != iconSize || len(on) != iconSize*iconSize*4 {
		t.Fatalf("icon %dx%d len=%d", w, h, len(on))
	}
	if bytes.Equal(on, off) {
		t.Fatal("online/offline pixmaps should differ by the status badge")
	}
	// Favicon should not be a solid generated circle: several distinct colors.
	colors := map[uint32]int{}
	for i := 0; i < len(on); i += 4 {
		colors[binary.BigEndian.Uint32(on[i:i+4])]++
	}
	if len(colors) < 8 {
		t.Fatalf("pixmap looks too uniform (%d colors); expected scaled favicon", len(colors))
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
