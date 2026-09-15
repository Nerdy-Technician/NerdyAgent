package tunnel

import (
	"strings"

	"github.com/nerdyrmm/agent/internal/status"
)

func classifyTCP(port int, kindHint string) string {
	hint := strings.ToLower(strings.TrimSpace(kindHint))
	switch hint {
	case "desktop", "vnc", "rdp", "novnc", "guac", "guacamole":
		return "desktop"
	case "ssh", "shell", "terminal":
		return "ssh"
	case "chat":
		return "chat"
	}
	if port >= 5900 && port <= 5999 {
		return "desktop"
	}
	if port == 3389 {
		return "desktop"
	}
	return "tcp"
}

func labelFor(kind string) string {
	switch kind {
	case "ssh":
		return "Remote SSH"
	case "desktop":
		return "Remote desktop"
	case "chat":
		return "Chat"
	case "tcp":
		return "TCP tunnel"
	default:
		return kind
	}
}

func reportOpen(rep status.Reporter, kind, id string) {
	if rep == nil {
		return
	}
	rep.SessionOpened(kind, id, labelFor(kind))
}

func reportClose(rep status.Reporter, id string) {
	if rep == nil {
		return
	}
	rep.SessionClosed(id)
}
