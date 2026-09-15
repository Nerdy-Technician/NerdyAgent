package tray

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func restartAgent(service string) error {
	service = strings.TrimSpace(service)
	helpers := []string{
		"/usr/libexec/nerdyrmm/nerdyrmm-agent-restart",
		"/usr/lib/nerdyrmm/nerdyrmm-agent-restart",
		"/usr/local/libexec/nerdyrmm/nerdyrmm-agent-restart",
	}
	for _, h := range helpers {
		if st, err := os.Stat(h); err == nil && !st.IsDir() && st.Mode()&0o111 != 0 {
			cmd := exec.Command("pkexec", h)
			out, err := cmd.CombinedOutput()
			if err != nil {
				return fmt.Errorf("restart helper: %s%s", strings.TrimSpace(string(out)), errSuffix(err))
			}
			return nil
		}
	}
	if service == "" {
		service = "nerdyrmm-agent"
	}
	unit := service
	if !strings.Contains(unit, ".") {
		unit += ".service"
	}
	cmd := exec.Command("pkexec", "systemctl", "restart", unit)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("pkexec systemctl restart %s: %s%s", unit, strings.TrimSpace(string(out)), errSuffix(err))
	}
	return nil
}

func errSuffix(err error) string {
	if err == nil {
		return ""
	}
	return ": " + err.Error()
}

func openURL(raw string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return
	}
	_ = exec.Command("xdg-open", raw).Start()
}

func openPopup(url string) {
	url = strings.TrimSpace(url)
	if url == "" {
		return
	}
	candidates := [][]string{
		{"google-chrome", "--app=" + url, "--window-size=460,640", "--class=NerdyRMMAgent"},
		{"chromium", "--app=" + url, "--window-size=460,640", "--class=NerdyRMMAgent"},
		{"chromium-browser", "--app=" + url, "--window-size=460,640"},
		{"microsoft-edge", "--app=" + url, "--window-size=460,640"},
		{"firefox", "--new-window", url},
		{"xdg-open", url},
	}
	for _, args := range candidates {
		if _, err := exec.LookPath(args[0]); err != nil {
			continue
		}
		if err := exec.Command(args[0], args[1:]...).Start(); err == nil {
			return
		}
	}
}

func notifyTechnician(kind string) {
	title := "NerdyRMM Agent"
	body := "A technician connected to this device."
	switch strings.ToLower(kind) {
	case "ssh":
		body = "A technician started a remote SSH session."
	case "desktop":
		body = "A technician started a remote desktop session."
	case "chat":
		body = "A technician started a chat session."
	}
	_ = exec.Command("notify-send", "-a", "NerdyRMM Agent", "-i", "nerdyrmm-agent", title, body).Start()
}

func installUserIcons() {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return
	}
	base := home + "/.local/share/icons/hicolor"
	sizes := []int{16, 22, 24, 32, 48, 64, 128, 256}
	for _, sz := range sizes {
		dir := fmt.Sprintf("%s/%dx%d/apps", base, sz, sz)
		_ = os.MkdirAll(dir, 0o755)
		path := dir + "/nerdyrmm-agent.png"
		if st, err := os.Stat(path); err == nil && st.Size() > 0 {
			continue
		}
		_ = writePNGFile(path, sz)
	}
	_ = exec.Command("gtk-update-icon-cache", "-f", base).Start()
}
