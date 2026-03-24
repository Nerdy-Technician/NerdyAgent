package tunnel

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/creack/pty"
	"github.com/gorilla/websocket"

	"github.com/nerdyagent/agent/internal/config"
)

type shellMessage struct {
	Type      string `json:"type"`
	SessionID string `json:"sessionId,omitempty"`
	Data      string `json:"data,omitempty"`
	Message   string `json:"message,omitempty"`
	Host      string `json:"host,omitempty"`
	Port      int    `json:"port,omitempty"`
	Cols      int    `json:"cols,omitempty"`
	Rows      int    `json:"rows,omitempty"`
}

type shellProcess struct {
	id    string
	cmd   *exec.Cmd
	ptmx  *os.File
	write sync.Mutex
}

type manager struct {
	ws       *websocket.Conn
	writeMu  sync.Mutex
	sessions map[string]*shellProcess
	sessMu   sync.Mutex
	tcpConns map[string]net.Conn
	tcpMu    sync.Mutex
}

func Run(cfg config.Config) {
	for {
		err := runOnce(cfg)
		if err != nil {
			fmt.Printf("agent tunnel disconnected: %v\n", err)
		}
		// Keep reconnect latency low so browser SSH recovers quickly after transient websocket closes.
		time.Sleep(2 * time.Second)
	}
}

func runOnce(cfg config.Config) error {
	wsURL, err := buildWSURL(cfg.ServerURL, cfg.DeviceID, cfg.Token)
	if err != nil {
		return err
	}
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		return err
	}
	defer conn.Close()

	m := &manager{
		ws:       conn,
		sessions: map[string]*shellProcess{},
		tcpConns: map[string]net.Conn{},
	}
	stopHeartbeat := make(chan struct{})
	go m.heartbeat(stopHeartbeat, 20*time.Second)
	defer close(stopHeartbeat)
	for {
		var msg shellMessage
		if err := conn.ReadJSON(&msg); err != nil {
			m.closeAllSessions("tunnel read closed")
			return err
		}
		switch strings.ToLower(strings.TrimSpace(msg.Type)) {
		case "shell_open":
			m.openShell(msg)
		case "shell_input":
			m.inputShell(msg)
		case "shell_resize":
			m.resizeShell(msg)
		case "shell_close":
			m.closeShell(msg.SessionID, "shell closed")
		case "tcp_open":
			m.openTCP(msg)
		case "tcp_data":
			m.inputTCP(msg)
		case "tcp_close":
			m.closeTCP(msg.SessionID, "tcp closed")
		}
	}
}

func (m *manager) heartbeat(stop <-chan struct{}, every time.Duration) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			_ = m.write(shellMessage{Type: "ping"})
		}
	}
}

func buildWSURL(serverURL string, deviceID int64, token string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(serverURL))
	if err != nil {
		return "", err
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		u.Scheme = "wss"
	case "http":
		u.Scheme = "ws"
	case "ws", "wss":
	default:
		u.Scheme = "ws"
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/api/agent/tunnel/ws"
	q := u.Query()
	q.Set("deviceId", strconv.FormatInt(deviceID, 10))
	q.Set("token", token)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func (m *manager) write(msg shellMessage) error {
	m.writeMu.Lock()
	defer m.writeMu.Unlock()
	return m.ws.WriteJSON(msg)
}

func (m *manager) openShell(msg shellMessage) {
	if runtime.GOOS == "windows" {
		_ = m.write(shellMessage{Type: "shell_error", SessionID: strings.TrimSpace(msg.SessionID), Message: "shell tunnel is unsupported on Windows agent"})
		return
	}
	sessionID := strings.TrimSpace(msg.SessionID)
	if sessionID == "" {
		return
	}
	m.closeShell(sessionID, "reopen")
	cols := uint16(msg.Cols)
	rows := uint16(msg.Rows)
	if cols == 0 {
		cols = 120
	}
	if rows == 0 {
		rows = 34
	}
	cmd := exec.Command("bash", "-l")
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: cols, Rows: rows})
	if err != nil {
		_ = m.write(shellMessage{Type: "shell_error", SessionID: sessionID, Message: fmt.Sprintf("open shell failed: %v", err)})
		return
	}
	proc := &shellProcess{id: sessionID, cmd: cmd, ptmx: ptmx}
	m.sessMu.Lock()
	m.sessions[sessionID] = proc
	m.sessMu.Unlock()

	_ = m.write(shellMessage{Type: "shell_ready", SessionID: sessionID})
	go m.streamOutput(proc)
	go m.waitExit(proc)
}

func (m *manager) streamOutput(proc *shellProcess) {
	buf := make([]byte, 4096)
	for {
		n, err := proc.ptmx.Read(buf)
		if n > 0 {
			_ = m.write(shellMessage{Type: "shell_output", SessionID: proc.id, Data: string(buf[:n])})
		}
		if err != nil {
			if err != io.EOF {
				_ = m.write(shellMessage{Type: "shell_error", SessionID: proc.id, Message: err.Error()})
			}
			return
		}
	}
}

func (m *manager) waitExit(proc *shellProcess) {
	err := proc.cmd.Wait()
	msg := "shell exited"
	if err != nil {
		msg = err.Error()
	}
	_ = m.write(shellMessage{Type: "shell_exit", SessionID: proc.id, Message: msg})
	m.closeShell(proc.id, msg)
}

func (m *manager) inputShell(msg shellMessage) {
	proc := m.getSession(msg.SessionID)
	if proc == nil {
		return
	}
	if msg.Data == "" {
		return
	}
	proc.write.Lock()
	_, _ = proc.ptmx.Write([]byte(msg.Data))
	proc.write.Unlock()
}

func (m *manager) resizeShell(msg shellMessage) {
	proc := m.getSession(msg.SessionID)
	if proc == nil {
		return
	}
	cols := uint16(msg.Cols)
	rows := uint16(msg.Rows)
	if cols == 0 || rows == 0 {
		return
	}
	_ = pty.Setsize(proc.ptmx, &pty.Winsize{Cols: cols, Rows: rows})
}

func (m *manager) getSession(sessionID string) *shellProcess {
	id := strings.TrimSpace(sessionID)
	if id == "" {
		return nil
	}
	m.sessMu.Lock()
	defer m.sessMu.Unlock()
	return m.sessions[id]
}

func (m *manager) closeShell(sessionID, _ string) {
	id := strings.TrimSpace(sessionID)
	if id == "" {
		return
	}
	m.sessMu.Lock()
	proc, ok := m.sessions[id]
	if ok {
		delete(m.sessions, id)
	}
	m.sessMu.Unlock()
	if !ok || proc == nil {
		return
	}
	_ = proc.ptmx.Close()
	if proc.cmd.Process != nil {
		_ = proc.cmd.Process.Kill()
	}
}

func (m *manager) closeAllSessions(reason string) {
	m.sessMu.Lock()
	ids := make([]string, 0, len(m.sessions))
	for id := range m.sessions {
		ids = append(ids, id)
	}
	m.sessMu.Unlock()
	for _, id := range ids {
		m.closeShell(id, reason)
	}
	m.tcpMu.Lock()
	tcpIDs := make([]string, 0, len(m.tcpConns))
	for id := range m.tcpConns {
		tcpIDs = append(tcpIDs, id)
	}
	m.tcpMu.Unlock()
	for _, id := range tcpIDs {
		m.closeTCP(id, reason)
	}
}

func (m *manager) dumpState() string {
	m.sessMu.Lock()
	shellCount := len(m.sessions)
	m.sessMu.Unlock()
	m.tcpMu.Lock()
	tcpCount := len(m.tcpConns)
	m.tcpMu.Unlock()
	b, _ := json.Marshal(map[string]interface{}{"sessions": shellCount, "tcpSessions": tcpCount})
	return string(b)
}

func (m *manager) openTCP(msg shellMessage) {
	sessionID := strings.TrimSpace(msg.SessionID)
	if sessionID == "" {
		return
	}
	m.closeTCP(sessionID, "reopen")

	host := strings.TrimSpace(msg.Host)
	if host == "" {
		host = "127.0.0.1"
	}
	port := msg.Port
	if port <= 0 || port > 65535 {
		_ = m.write(shellMessage{Type: "tcp_error", SessionID: sessionID, Message: "invalid port"})
		return
	}
	target := net.JoinHostPort(host, strconv.Itoa(port))
	conn, err := net.DialTimeout("tcp", target, 10*time.Second)
	if err != nil {
		_ = m.write(shellMessage{Type: "tcp_error", SessionID: sessionID, Message: fmt.Sprintf("connect failed: %v", err)})
		return
	}
	m.tcpMu.Lock()
	m.tcpConns[sessionID] = conn
	m.tcpMu.Unlock()
	_ = m.write(shellMessage{Type: "tcp_ready", SessionID: sessionID})
	go m.streamTCP(sessionID, conn)
}

func (m *manager) streamTCP(sessionID string, conn net.Conn) {
	buf := make([]byte, 8192)
	for {
		n, err := conn.Read(buf)
		if n > 0 {
			payload := base64.StdEncoding.EncodeToString(buf[:n])
			_ = m.write(shellMessage{Type: "tcp_data", SessionID: sessionID, Data: payload})
		}
		if err != nil {
			if err != io.EOF {
				_ = m.write(shellMessage{Type: "tcp_error", SessionID: sessionID, Message: err.Error()})
			}
			_ = m.write(shellMessage{Type: "tcp_close", SessionID: sessionID})
			m.closeTCP(sessionID, "stream closed")
			return
		}
	}
}

func (m *manager) inputTCP(msg shellMessage) {
	sessionID := strings.TrimSpace(msg.SessionID)
	if sessionID == "" {
		return
	}
	m.tcpMu.Lock()
	conn := m.tcpConns[sessionID]
	m.tcpMu.Unlock()
	if conn == nil || strings.TrimSpace(msg.Data) == "" {
		return
	}
	data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(msg.Data))
	if err != nil || len(data) == 0 {
		return
	}
	_, _ = conn.Write(data)
}

func (m *manager) closeTCP(sessionID, _ string) {
	id := strings.TrimSpace(sessionID)
	if id == "" {
		return
	}
	m.tcpMu.Lock()
	conn, ok := m.tcpConns[id]
	if ok {
		delete(m.tcpConns, id)
	}
	m.tcpMu.Unlock()
	if ok && conn != nil {
		_ = conn.Close()
	}
}
