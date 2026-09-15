package tickets

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/nerdyrmm/agent/internal/status"
)

// Fetch tries a few agent ticket-summary endpoints. Missing APIs return empty + a note.
func Fetch(serverURL, token string, deviceID int64) ([]status.Ticket, string) {
	base := strings.TrimRight(strings.TrimSpace(serverURL), "/")
	if base == "" || strings.TrimSpace(token) == "" || deviceID <= 0 {
		return nil, "open in web"
	}
	client := &http.Client{Timeout: 8 * time.Second}
	paths := []string{
		"/api/agent/tickets",
		fmt.Sprintf("/api/agent/tickets?deviceId=%d", deviceID),
		fmt.Sprintf("/api/agent/devices/%d/tickets", deviceID),
	}
	var lastStatus int
	for _, p := range paths {
		req, err := http.NewRequest(http.MethodGet, base+p, nil)
		if err != nil {
			continue
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		_ = resp.Body.Close()
		lastStatus = resp.StatusCode
		if resp.StatusCode == http.StatusNotFound {
			continue
		}
		if resp.StatusCode >= 300 {
			continue
		}
		tickets, ok := parse(body)
		if ok {
			return tickets, ""
		}
	}
	if lastStatus == http.StatusNotFound || lastStatus == 0 {
		return nil, "Ticket summary API is not available on this NerdyRMM server yet. Open tickets in the web UI."
	}
	return nil, "Could not load tickets. Open the web UI to view them."
}

func parse(body []byte) ([]status.Ticket, bool) {
	var arr []status.Ticket
	if err := json.Unmarshal(body, &arr); err == nil {
		return sanitize(arr), true
	}
	var wrap struct {
		Tickets []status.Ticket `json:"tickets"`
		Items   []status.Ticket `json:"items"`
		Data    []status.Ticket `json:"data"`
	}
	if err := json.Unmarshal(body, &wrap); err == nil {
		switch {
		case wrap.Tickets != nil:
			return sanitize(wrap.Tickets), true
		case wrap.Items != nil:
			return sanitize(wrap.Items), true
		case wrap.Data != nil:
			return sanitize(wrap.Data), true
		}
		return []status.Ticket{}, true
	}
	return nil, false
}

func sanitize(in []status.Ticket) []status.Ticket {
	out := make([]status.Ticket, 0, len(in))
	for _, t := range in {
		t.Title = strings.TrimSpace(t.Title)
		t.Status = strings.TrimSpace(t.Status)
		if t.ID == 0 && t.Title == "" {
			continue
		}
		if len(t.Title) > 200 {
			t.Title = t.Title[:200]
		}
		out = append(out, t)
		if len(out) >= 25 {
			break
		}
	}
	return out
}
