//go:build linux

package ipc

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Server accepts local tray chat/send commands.
type Server struct {
	ln      net.Listener
	path    string
	token   string
	mu      sync.Mutex
	handler Handler
}

func (s *Server) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}

func (s *Server) Token() string {
	if s == nil {
		return ""
	}
	return s.token
}

func (s *Server) SetHandler(h Handler) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.handler = h
	s.mu.Unlock()
}

func (s *Server) handlerOrNoop() Handler {
	s.mu.Lock()
	h := s.handler
	s.mu.Unlock()
	if h == nil {
		return noopHandler{}
	}
	return h
}

// Listen opens a world-readable unix socket for the user-session tray.
func Listen(token string) (*Server, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, fmt.Errorf("ipc token required")
	}
	dir := "/run/nerdyrmm-agent"
	if err := os.MkdirAll(dir, 0o755); err != nil {
		dir = os.TempDir()
	}
	path := filepath.Join(dir, "ipc.sock")
	_ = os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		path = filepath.Join(os.TempDir(), "nerdyrmm-agent-ipc.sock")
		_ = os.Remove(path)
		ln, err = net.Listen("unix", path)
		if err != nil {
			return nil, err
		}
	}
	_ = os.Chmod(path, 0o666)
	s := &Server{ln: ln, path: path, token: token}
	go s.serve()
	return s, nil
}

func (s *Server) Close() {
	if s == nil {
		return
	}
	_ = s.ln.Close()
	_ = os.Remove(s.path)
}

func (s *Server) serve() {
	for {
		c, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.handle(c)
	}
}

func (s *Server) handle(c net.Conn) {
	defer c.Close()
	dec := json.NewDecoder(c)
	var req Request
	if err := dec.Decode(&req); err != nil {
		_ = json.NewEncoder(c).Encode(Response{OK: false, Error: "bad request"})
		return
	}
	if strings.TrimSpace(req.Token) != s.token {
		_ = json.NewEncoder(c).Encode(Response{OK: false, Error: "unauthorized"})
		return
	}
	switch strings.ToLower(strings.TrimSpace(req.Op)) {
	case "ping":
		_ = json.NewEncoder(c).Encode(Response{OK: true})
	case "chat_send":
		if err := s.handlerOrNoop().SendChat(req.SessionID, req.Text); err != nil {
			_ = json.NewEncoder(c).Encode(Response{OK: false, Error: err.Error()})
			return
		}
		_ = json.NewEncoder(c).Encode(Response{OK: true})
	default:
		_ = json.NewEncoder(c).Encode(Response{OK: false, Error: "unknown op"})
	}
}

// Call sends one request to the agent IPC socket.
func Call(socket, token string, req Request) (Response, error) {
	var resp Response
	if strings.TrimSpace(socket) == "" {
		return resp, fmt.Errorf("ipc unavailable")
	}
	req.Token = token
	c, err := net.Dial("unix", socket)
	if err != nil {
		return resp, err
	}
	defer c.Close()
	if err := json.NewEncoder(c).Encode(req); err != nil {
		return resp, err
	}
	if err := json.NewDecoder(c).Decode(&resp); err != nil {
		return resp, err
	}
	return resp, nil
}
