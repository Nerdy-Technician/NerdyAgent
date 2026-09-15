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

	"github.com/nerdyrmm/agent/internal/config"
	"github.com/nerdyrmm/agent/internal/ipc"
	"github.com/nerdyrmm/agent/internal/status"
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
	Kind      string `json:"kind,omitempty"`
	From      string `json:"from,omitempty"`
}

type shellProcess struct {
	id     string
	cmd    *exec.Cmd
	ptmx   *os.File       // Linux PTY
	stdin  io.WriteCloser // Windows pipe stdin
	stdout io.ReadCloser  // Windows pipe stdout
	write  sync.Mutex
}

type manager struct {
	ws       *websocket.Conn
	writeMu  sync.Mutex
	sessions map[string]*shellProcess
	sessMu   sync.Mutex
	tcpConns map[string]net.Conn
	tcpMu    sync.Mutex
	chats    map[string]struct{}
	chatMu   sync.Mutex
	rep      status.Reporter
}

type liveTunnel struct {
	mu    sync.Mutex
	m     *manager
	rep   status.Reporter
	token string
}

func (t *liveTunnel) setManager(m *manager) {
	t.mu.Lock()
	t.m = m
	t.mu.Unlock()
}

func (t *liveTunnel) SendChat(sessionID, text string) error {
	if t == nil {
		return fmt.Errorf("tunnel offline")
	}
	t.mu.Lock()
	m := t.m
	t.mu.Unlock()
	if m == nil {
		return fmt.Errorf("tunnel offline")
	}
	return m.sendChatFromUser(sessionID, text)
}

func Run(cfg config.Config, rep status.Reporter) {
	live := &liveTunnel{rep: rep, token: cfg.Token}
	ipcToken := status.NewIPCToken()
	if srv, err := ipc.Listen(ipcToken); err == nil {
		srv.SetHandler(live)
		if rep != nil {
			rep.SetIPC(srv.Path(), srv.Token())
		}
		defer srv.Close()
	}

	bo := newBackoff(time.Second, 30*time.Second)
	for {
		started := time.Now()
		err := runOnce(cfg, live)
		live.setManager(nil)
		if rep != nil {
			rep.SetTunnelOnline(false)
			rep.ClearSessions()
		}
		if err != nil {
			fmt.Printf("agent tunnel disconnected: %s\n", status.RedactSecret(err.Error(), cfg.Token))
		}
		if time.Since(started) > 20*time.Second {
			bo.reset()
		}
		time.Sleep(bo.next())
	}
}

func runOnce(cfg config.Config, live *liveTunnel) error {
	wsURL, err := buildWSURL(cfg.ServerURL, cfg.DeviceID, cfg.Token)
	if err != nil {
		return err
	}
	dialer := websocket.Dialer{HandshakeTimeout: 15 * time.Second}
	conn, resp, err := dialer.Dial(wsURL, nil)
	if resp != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		if resp != nil && (resp.StatusCode == 401 || resp.StatusCode == 403) {
			time.Sleep(20 * time.Second)
		}
		return fmt.Errorf("dial failed: %s", status.RedactSecret(err.Error(), cfg.Token))
	}
	defer conn.Close()
	conn.SetReadLimit(1 << 20)
	_ = conn.SetReadDeadline(time.Now().Add(75 * time.Second))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(75 * time.Second))
	})

	m := &manager{
		ws:       conn,
		sessions: map[string]*shellProcess{},
		tcpConns: map[string]net.Conn{},
		chats:    map[string]struct{}{},
		rep:      nil,
	}
	if live != nil {
		m.rep = live.rep
		live.setManager(m)
	}
	if m.rep != nil {
		m.rep.SetTunnelOnline(true)
	}

	stopHeartbeat := make(chan struct{})
	go m.heartbeat(stopHeartbeat, 20*time.Second)
	defer close(stopHeartbeat)
	for {
		var msg shellMessage
		if err := conn.ReadJSON(&msg); err != nil {
			m.closeAllSessions("tunnel read closed")
			return fmt.Errorf("read closed: %s", status.RedactSecret(err.Error(), cfg.Token))
		}
		_ = conn.SetReadDeadline(time.Now().Add(75 * time.Second))
		switch strings.ToLower(strings.TrimSpace(msg.Type)) {
		case "shell_open":
			m.openShell(msg)
		case "shell_input":
			m.inputShell(msg)
		case "shell_resize":
			m.resizeShell(msg)
		case "shell_close":
			m.closeShell(msg.SessionID, "shell closed")
		case "tcp_open", "desktop_open":
			if strings.EqualFold(msg.Type, "desktop_open") && strings.TrimSpace(msg.Kind) == "" {
				msg.Kind = "desktop"
			}
			m.openTCP(msg)
		case "tcp_data":
			m.inputTCP(msg)
		case "tcp_close", "desktop_close":
			m.closeTCP(msg.SessionID, "tcp closed")
		case "chat_open":
			m.openChat(msg)
		case "chat_message":
			m.recvChat(msg)
		case "chat_close":
			m.closeChat(msg.SessionID)
		case "ping":
			_ = m.write(shellMessage{Type: "pong"})
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
			m.writeMu.Lock()
			_ = m.ws.WriteControl(websocket.PingMessage, []byte("ping"), time.Now().Add(5*time.Second))
			m.writeMu.Unlock()
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
	_ = m.ws.SetWriteDeadline(time.Now().Add(15 * time.Second))
	return m.ws.WriteJSON(msg)
}

func (m *manager) openShell(msg shellMessage) {
	sessionID := strings.TrimSpace(msg.SessionID)
	if sessionID == "" {
		return
	}
	m.closeShell(sessionID, "reopen")

	if runtime.GOOS == "windows" {
		cmd := exec.Command("powershell.exe", "-NoLogo", "-NoProfile", "-ExecutionPolicy", "Bypass")
		stdin, err := cmd.StdinPipe()
		if err != nil {
			_ = m.write(shellMessage{Type: "shell_error", SessionID: sessionID, Message: fmt.Sprintf("open shell failed: %v", err)})
			return
		}
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			_ = m.write(shellMessage{Type: "shell_error", SessionID: sessionID, Message: fmt.Sprintf("open shell failed: %v", err)})
			return
		}
		cmd.Stderr = cmd.Stdout
		if err := cmd.Start(); err != nil {
			_ = m.write(shellMessage{Type: "shell_error", SessionID: sessionID, Message: fmt.Sprintf("open shell failed: %v", err)})
			return
		}
		proc := &shellProcess{id: sessionID, cmd: cmd, stdin: stdin, stdout: stdout}
		m.sessMu.Lock()
		m.sessions[sessionID] = proc
		m.sessMu.Unlock()
		reportOpen(m.rep, "ssh", sessionID)
		go m.readPipe(proc)
		go m.waitExit(proc)
		return
	}

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
	reportOpen(m.rep, "ssh", sessionID)

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

func (m *manager) readPipe(proc *shellProcess) {
	buf := make([]byte, 4096)
	for {
		n, err := proc.stdout.Read(buf)
		if n > 0 {
			_ = m.write(shellMessage{Type: "shell_output", SessionID: proc.id, Data: string(buf[:n])})
		}
		if err != nil {
			return
		}
	}
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
	if proc.stdin != nil {
		_, _ = proc.stdin.Write([]byte(msg.Data))
	} else {
		_, _ = proc.ptmx.Write([]byte(msg.Data))
	}
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
	reportClose(m.rep, id)
	if proc.stdin != nil {
		_ = proc.stdin.Close()
	}
	if proc.stdout != nil {
		_ = proc.stdout.Close()
	}
	if proc.ptmx != nil {
		_ = proc.ptmx.Close()
	}
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
	m.chatMu.Lock()
	chatIDs := make([]string, 0, len(m.chats))
	for id := range m.chats {
		chatIDs = append(chatIDs, id)
	}
	m.chatMu.Unlock()
	for _, id := range chatIDs {
		m.closeChat(id)
	}
}

func (m *manager) dumpState() string {
	m.sessMu.Lock()
	shellCount := len(m.sessions)
	m.sessMu.Unlock()
	m.tcpMu.Lock()
	tcpCount := len(m.tcpConns)
	m.tcpMu.Unlock()
	m.chatMu.Lock()
	chatCount := len(m.chats)
	m.chatMu.Unlock()
	b, _ := json.Marshal(map[string]interface{}{"sessions": shellCount, "tcpSessions": tcpCount, "chatSessions": chatCount})
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
	kind := classifyTCP(port, msg.Kind)
	reportOpen(m.rep, kind, sessionID)
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
	if !ok {
		return
	}
	reportClose(m.rep, id)
	if conn != nil {
		_ = conn.Close()
	}
}

func (m *manager) openChat(msg shellMessage) {
	sessionID := strings.TrimSpace(msg.SessionID)
	if sessionID == "" {
		return
	}
	m.chatMu.Lock()
	m.chats[sessionID] = struct{}{}
	m.chatMu.Unlock()
	reportOpen(m.rep, "chat", sessionID)
	_ = m.write(shellMessage{Type: "chat_ready", SessionID: sessionID})
}

func (m *manager) recvChat(msg shellMessage) {
	sessionID := strings.TrimSpace(msg.SessionID)
	text := strings.TrimSpace(msg.Data)
	if text == "" {
		text = strings.TrimSpace(msg.Message)
	}
	if sessionID == "" || text == "" {
		return
	}
	m.chatMu.Lock()
	_, ok := m.chats[sessionID]
	m.chatMu.Unlock()
	if !ok {
		m.openChat(shellMessage{SessionID: sessionID})
	}
	from := strings.ToLower(strings.TrimSpace(msg.From))
	if from == "" {
		from = "technician"
	}
	if m.rep != nil {
		m.rep.AppendChat(sessionID, from, text)
	}
}

func (m *manager) sendChatFromUser(sessionID, text string) error {
	sessionID = strings.TrimSpace(sessionID)
	text = strings.TrimSpace(text)
	if text == "" {
		return fmt.Errorf("empty message")
	}
	if sessionID == "" {
		m.chatMu.Lock()
		for id := range m.chats {
			sessionID = id
			break
		}
		m.chatMu.Unlock()
	}
	if sessionID == "" {
		return fmt.Errorf("no active chat")
	}
	if m.rep != nil {
		m.rep.AppendChat(sessionID, "user", text)
	}
	return m.write(shellMessage{Type: "chat_message", SessionID: sessionID, Data: text, From: "user"})
}

func (m *manager) closeChat(sessionID string) {
	id := strings.TrimSpace(sessionID)
	if id == "" {
		return
	}
	m.chatMu.Lock()
	_, ok := m.chats[id]
	if ok {
		delete(m.chats, id)
	}
	m.chatMu.Unlock()
	if !ok {
		return
	}
	reportClose(m.rep, id)
	_ = m.write(shellMessage{Type: "chat_close", SessionID: id})
}
