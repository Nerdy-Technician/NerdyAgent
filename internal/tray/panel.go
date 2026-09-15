package tray

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// OpenStatusPanel is left-click / `nerdyrmm-agent --tray-status`.
func OpenStatusPanel() { openStatusPanel() }

func openStatusPanel() {
	fmt.Fprintln(os.Stderr, "NerdyRMM tray: opening status panel")
	if openRichPopup("/") {
		return
	}
	openZenityBox("NerdyRMM Agent", statusPanelText(loadView()))
}

func openConnectionProperties() {
	fmt.Fprintln(os.Stderr, "NerdyRMM tray: opening connection properties")
	if openRichPopup("/connection") {
		return
	}
	openZenityBox("Connection properties", connectionPanelText(loadView()))
}

func openAboutPanel() {
	if openRichPopup("/#about") {
		return
	}
	v := loadView()
	ver := strings.TrimSpace(v.Version)
	if ver == "" {
		ver = "unknown"
	}
	openZenityBox("About NerdyRMM Agent", "NerdyAgent "+ver+"\nStandalone RMM agent for NerdyRMM.\n© Nerdy Technician")
}

// openRichPopup launches the localhost Datto-style UI in a known browser.
// xdg-open is intentionally not treated as success: on Cinnamon it can
// return without showing a window (Asgard 0.3.10.3 left-click no-op).
func openRichPopup(path string) bool {
	if _, err := startUI(); err != nil || strings.TrimSpace(uiURL) == "" {
		return false
	}
	return launchBrowserApp(popupURL(path))
}

func launchBrowserApp(url string) bool {
	url = strings.TrimSpace(url)
	if url == "" {
		return false
	}
	candidates := [][]string{
		{"google-chrome", "--app=" + url, "--window-size=460,640", "--class=NerdyRMMAgent"},
		{"google-chrome-stable", "--app=" + url, "--window-size=460,640", "--class=NerdyRMMAgent"},
		{"chromium", "--app=" + url, "--window-size=460,640", "--class=NerdyRMMAgent"},
		{"chromium-browser", "--app=" + url, "--window-size=460,640"},
		{"microsoft-edge", "--app=" + url, "--window-size=460,640"},
		{"firefox", "--new-window", url},
		{"firefox-esr", "--new-window", url},
	}
	for _, args := range candidates {
		if _, err := exec.LookPath(args[0]); err != nil {
			continue
		}
		if err := exec.Command(args[0], args[1:]...).Start(); err == nil {
			return true
		}
	}
	return false
}

func statusPanelText(v view) string {
	lines := []string{
		"NerdyRMM Agent — " + nz(v.Status, "Offline"),
		"Last check-in: " + nz(v.LastCheckinRel, "never"),
		"Version: " + nz(v.Version, "unknown"),
		"Hostname: " + nz(v.Hostname, "—"),
		"OS: " + nz(v.OS, "—"),
	}
	if v.TunnelOnline {
		lines = append(lines, "Tunnel: Online")
	} else {
		lines = append(lines, "Tunnel: Offline")
	}
	if n := len(v.Sessions); n > 0 {
		lines = append(lines, fmt.Sprintf("Active sessions: %d", n))
	}
	return strings.Join(lines, "\n")
}

func connectionPanelText(v view) string {
	dev := "—"
	if v.DeviceID > 0 {
		dev = fmt.Sprintf("%d", v.DeviceID)
	}
	tok := strings.TrimSpace(v.TokenMasked)
	if tok == "" {
		tok = "••••"
	}
	return strings.Join([]string{
		"Server URL: " + nz(v.ServerURL, "—"),
		"Device ID: " + dev,
		"Config path: " + nz(v.ConfigPath, "—"),
		"Service: " + nz(v.Service, "—"),
		"Device token: " + tok,
		"",
		"The device token is never shown in cleartext.",
	}, "\n")
}

func openZenityBox(title, body string) {
	title = strings.TrimSpace(title)
	body = strings.TrimSpace(body)
	if body == "" {
		return
	}
	for _, bin := range []string{"zenity", "yad"} {
		if _, err := exec.LookPath(bin); err != nil {
			continue
		}
		args := []string{"--info", "--title=" + title, "--text=" + pangoEscape(body), "--width=420"}
		if err := exec.Command(bin, args...).Start(); err == nil {
			return
		}
	}
	// Last resort: one notify, high-level text only (no connection details).
	_ = exec.Command("notify-send", "-a", "NerdyRMM Agent", "-i", "nerdyrmm-agent", title, firstLine(body)).Start()
}

func nz(s, fallback string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return fallback
	}
	return s
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func pangoEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}
