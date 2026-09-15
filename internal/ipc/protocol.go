package ipc

// Request is a single JSON object from the tray to the agent.
type Request struct {
	Token     string `json:"token"`
	Op        string `json:"op"`
	SessionID string `json:"sessionId,omitempty"`
	Text      string `json:"text,omitempty"`
}

// Response is the JSON reply.
type Response struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// Handler is implemented by the live tunnel.
type Handler interface {
	SendChat(sessionID, text string) error
}

type noopHandler struct{}

func (noopHandler) SendChat(string, string) error { return nil }
