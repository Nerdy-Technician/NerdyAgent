package status

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
	"sync"
	"time"

	"github.com/nerdyrmm/agent/internal/updater"
)

const maxChatLines = 50

// Reporter is the live presence surface the tunnel writes into.
type Reporter interface {
	SetTunnelOnline(online bool)
	SetIPC(socket, token string)
	SessionOpened(kind, id, label string)
	SessionClosed(id string)
	ClearSessions()
	AppendChat(sessionID, from, text string)
}

// Hub merges check-in, tunnel, chat, and ticket updates into one status.json.
type Hub struct {
	mu   sync.Mutex
	path string
	snap Snapshot
}

// NewHub starts a live snapshot writer for configPath's sibling status.json.
func NewHub(configPath string) *Hub {
	h := &Hub{path: configPath}
	if prev, err := ReadSnapshot(configPath); err == nil {
		h.snap = prev
	}
	h.snap.Running = true
	h.snap.TunnelOnline = false
	h.snap.ActiveSessions = nil
	h.snap.LastSession = nil
	return h
}

func (h *Hub) Path() string {
	if h == nil {
		return ""
	}
	return h.path
}

func (h *Hub) Snapshot() Snapshot {
	if h == nil {
		return Snapshot{}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return cloneSnap(h.snap)
}

// SetIdentity writes host/install fields that rarely change.
func (h *Hub) SetIdentity(version, serverURL, service, configPath, hostname, osName, tokenMasked string, deviceID int64) {
	if h == nil {
		return
	}
	h.mutate(func(s *Snapshot) {
		s.Version = version
		s.ServerURL = serverURL
		s.Service = service
		s.ConfigPath = configPath
		s.Hostname = hostname
		s.OS = osName
		s.TokenMasked = tokenMasked
		s.DeviceID = deviceID
	})
}

// SetCheckin records the latest agent-to-server poll (does not clobber tunnel sessions).
func (h *Hub) SetCheckin(ok bool, errMsg, version string) {
	if h == nil {
		return
	}
	h.mutate(func(s *Snapshot) {
		s.LastCheckin = time.Now().UTC().Format(time.RFC3339)
		s.LastCheckinOK = ok
		s.LastError = strings.TrimSpace(errMsg)
		if strings.TrimSpace(version) != "" {
			s.Version = version
		}
		s.UpdateAvailable = updateAvailable(s.Version, s.LatestVersion)
	})
}

func (h *Hub) SetTickets(tickets []Ticket, note string) {
	if h == nil {
		return
	}
	h.mutate(func(s *Snapshot) {
		s.Tickets = tickets
		s.TicketsNote = note
	})
}

func (h *Hub) SetLatestVersion(latest string) {
	if h == nil {
		return
	}
	h.mutate(func(s *Snapshot) {
		s.LatestVersion = strings.TrimPrefix(strings.TrimSpace(latest), "v")
		s.UpdateAvailable = updateAvailable(s.Version, s.LatestVersion)
	})
}

func (h *Hub) SetTunnelOnline(online bool) {
	if h == nil {
		return
	}
	h.mutate(func(s *Snapshot) {
		s.TunnelOnline = online
		if !online {
			s.ActiveSessions = nil
		}
	})
}

func (h *Hub) SetIPC(socket, token string) {
	if h == nil {
		return
	}
	h.mutate(func(s *Snapshot) {
		s.IPCSocket = socket
		s.IPCToken = token
	})
}

func (h *Hub) SessionOpened(kind, id, label string) {
	if h == nil {
		return
	}
	kind = normalizeKind(kind)
	id = strings.TrimSpace(id)
	if id == "" {
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	h.mutate(func(s *Snapshot) {
		found := false
		for i := range s.ActiveSessions {
			if s.ActiveSessions[i].ID == id {
				s.ActiveSessions[i].Type = kind
				if label != "" {
					s.ActiveSessions[i].Label = label
				}
				found = true
				break
			}
		}
		if !found {
			s.ActiveSessions = append(s.ActiveSessions, Session{
				ID:        id,
				Type:      kind,
				StartedAt: now,
				Label:     label,
			})
		}
		s.LastSession = &SessionEvt{Kind: "open", Type: kind, ID: id, At: now}
	})
}

func (h *Hub) SessionClosed(id string) {
	if h == nil {
		return
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	h.mutate(func(s *Snapshot) {
		kind := ""
		out := s.ActiveSessions[:0]
		for _, sess := range s.ActiveSessions {
			if sess.ID == id {
				kind = sess.Type
				continue
			}
			out = append(out, sess)
		}
		s.ActiveSessions = out
		s.LastSession = &SessionEvt{Kind: "close", Type: kind, ID: id, At: now}
	})
}

func (h *Hub) ClearSessions() {
	if h == nil {
		return
	}
	h.mutate(func(s *Snapshot) {
		s.ActiveSessions = nil
		s.TunnelOnline = false
	})
}

func (h *Hub) AppendChat(sessionID, from, text string) {
	if h == nil {
		return
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	if len(text) > 4000 {
		text = text[:4000]
	}
	from = strings.TrimSpace(from)
	if from == "" {
		from = "technician"
	}
	line := ChatLine{
		SessionID: strings.TrimSpace(sessionID),
		From:      from,
		Text:      text,
		At:        time.Now().UTC().Format(time.RFC3339),
	}
	h.mutate(func(s *Snapshot) {
		s.Chat = append(s.Chat, line)
		if len(s.Chat) > maxChatLines {
			s.Chat = s.Chat[len(s.Chat)-maxChatLines:]
		}
	})
}

func (h *Hub) mutate(fn func(*Snapshot)) {
	h.mu.Lock()
	fn(&h.snap)
	h.snap.Running = true
	h.snap.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	snap := cloneSnap(h.snap)
	path := h.path
	h.mu.Unlock()
	_ = WriteSnapshot(path, snap)
}

func cloneSnap(s Snapshot) Snapshot {
	out := s
	if s.ActiveSessions != nil {
		out.ActiveSessions = append([]Session(nil), s.ActiveSessions...)
	}
	if s.Tickets != nil {
		out.Tickets = append([]Ticket(nil), s.Tickets...)
	}
	if s.Chat != nil {
		out.Chat = append([]ChatLine(nil), s.Chat...)
	}
	if s.LastSession != nil {
		cp := *s.LastSession
		out.LastSession = &cp
	}
	return out
}

func updateAvailable(current, latest string) bool {
	current = strings.TrimSpace(current)
	latest = strings.TrimSpace(latest)
	if current == "" || latest == "" {
		return false
	}
	return updater.CompareVersions(current, latest) < 0
}

func normalizeKind(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "ssh", "shell", "terminal":
		return "ssh"
	case "desktop", "vnc", "rdp", "novnc":
		return "desktop"
	case "chat":
		return "chat"
	case "tcp":
		return "tcp"
	default:
		if kind == "" {
			return "ssh"
		}
		return strings.ToLower(kind)
	}
}

func NewIPCToken() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return hex.EncodeToString([]byte(time.Now().UTC().Format(time.RFC3339Nano)))
	}
	return hex.EncodeToString(b)
}
