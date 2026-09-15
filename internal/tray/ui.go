package tray

import (
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/nerdyrmm/agent/internal/ipc"
)

//go:embed web/*
var webFiles embed.FS

var (
	uiOnce sync.Once
	uiURL  string
	uiErr  error
)

func startUI() (string, error) {
	uiOnce.Do(func() {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			uiErr = err
			return
		}
		mux := http.NewServeMux()
		mux.HandleFunc("/", serveIndex)
		mux.HandleFunc("/connection", serveConnection)
		mux.HandleFunc("/favicon.png", serveFavicon)
		mux.HandleFunc("/api/status", serveStatus)
		mux.HandleFunc("/api/prefs", servePrefs)
		mux.HandleFunc("/api/chat", serveChat)
		mux.HandleFunc("/api/restart", serveRestart)
		srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
		go func() { _ = srv.Serve(ln) }()
		uiURL = "http://" + ln.Addr().String()
	})
	return uiURL, uiErr
}

// RunUIOnly serves the localhost popup without a StatusNotifier icon.
func RunUIOnly() error {
	installUserIcons()
	u, err := startUI()
	if err != nil {
		return err
	}
	fmt.Println("NerdyRMM tray UI:", u)
	select {}
}

func serveIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	serveWeb(w, "web/index.html", "text/html; charset=utf-8")
}

func serveConnection(w http.ResponseWriter, r *http.Request) {
	serveWeb(w, "web/connection.html", "text/html; charset=utf-8")
}

func serveFavicon(w http.ResponseWriter, r *http.Request) {
	serveWeb(w, "web/favicon.png", "image/png")
}

func serveWeb(w http.ResponseWriter, name, ctype string) {
	b, err := fs.ReadFile(webFiles, name)
	if err != nil {
		http.Error(w, "missing "+name, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(b)
}

func serveStatus(w http.ResponseWriter, r *http.Request) {
	v := loadView()
	out := map[string]any{
		"title":                   v.Title,
		"status":                  v.Status,
		"tooltip":                 v.Tooltip,
		"online":                  v.Online,
		"updateAvailable":         v.UpdateAvail,
		"tunnelOnline":            v.TunnelOnline,
		"version":                 v.Version,
		"hostname":                v.Hostname,
		"os":                      v.OS,
		"lastCheckin":             v.LastCheckin,
		"lastCheckinRel":          v.LastCheckinRel,
		"serverURL":               v.ServerURL,
		"deviceId":                v.DeviceID,
		"service":                 v.Service,
		"configPath":              v.ConfigPath,
		"tokenMasked":             v.TokenMasked,
		"sessions":                v.Sessions,
		"tickets":                 v.Tickets,
		"ticketsNote":             v.TicketsNote,
		"chat":                    v.Chat,
		"webURL":                  v.WebURL,
		"docsURL":                 v.DocsURL,
		"notifyTechnicianConnect": v.NotifyPref,
		"ipcAvailable":            strings.TrimSpace(v.IPCSocket) != "",
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

func servePrefs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		NotifyTechnicianConnect bool `json:"notifyTechnicianConnect"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	p := loadPrefs()
	p.NotifyTechnicianConnect = body.NotifyTechnicianConnect
	if err := savePrefs(p); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(p)
}

func serveChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		SessionID string `json:"sessionId"`
		Text      string `json:"text"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 8192)).Decode(&body); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	v := loadView()
	resp, err := ipc.Call(v.IPCSocket, v.IPCToken, ipc.Request{
		Op:        "chat_send",
		SessionID: body.SessionID,
		Text:      body.Text,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func serveRestart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method", http.StatusMethodNotAllowed)
		return
	}
	v := loadView()
	if err := restartAgent(v.Service); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"ok":true}`))
}

func popupURL(path string) string {
	if uiURL == "" {
		return ""
	}
	return strings.TrimRight(uiURL, "/") + path
}

func printUIURL() {
	if uiURL != "" {
		fmt.Fprintln(os.Stderr, "NerdyRMM tray popup:", uiURL)
	}
}
