//go:build linux

package tray

import (
	"testing"

	"github.com/godbus/dbus/v5"
)

func TestSNIPixmapDBusSignature(t *testing.T) {
	w, h, pix := iconPixmap(true, false)
	if w <= 0 || h <= 0 || len(pix) == 0 {
		t.Fatalf("iconPixmap returned empty icon w=%d h=%d len=%d", w, h, len(pix))
	}
	pm := []pixmap{{Width: w, Height: h, Data: pix}}
	sig := dbus.MakeVariant(pm).Signature().String()
	if sig != "a(iiay)" {
		t.Fatalf("IconPixmap signature %q, want a(iiay) for StatusNotifier", sig)
	}
	tt := toolTip{IconName: "nerdyrmm-agent", IconPixmap: pm, Title: "NerdyRMM Agent", Description: "Online"}
	if got := dbus.MakeVariant(tt).Signature().String(); got != "(sa(iiay)ss)" {
		t.Fatalf("ToolTip signature %q, want (sa(iiay)ss)", got)
	}
}
