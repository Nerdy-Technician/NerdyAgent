//go:build linux

package tray

import (
	"testing"

	"github.com/godbus/dbus/v5"
)

func TestSNIPixmapDBusSignature(t *testing.T) {
	pm := sniPixmaps(true)
	if len(pm) < 2 {
		t.Fatalf("expected 22 and 32 px, got %d", len(pm))
	}
	sig := dbus.MakeVariant(pm).Signature().String()
	if sig != "a(iiay)" {
		t.Fatalf("IconPixmap signature %q, want a(iiay) for StatusNotifier", sig)
	}
	tt := toolTip{IconName: "", IconPixmap: pm, Title: "NerdyRMM Agent", Description: "ok"}
	if got := dbus.MakeVariant(tt).Signature().String(); got != "(sa(iiay)ss)" {
		t.Fatalf("ToolTip signature %q, want (sa(iiay)ss)", got)
	}
}
