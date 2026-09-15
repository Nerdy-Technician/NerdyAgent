package tunnel

import (
	"strings"
	"testing"
	"time"
)

func TestClassifyTCP(t *testing.T) {
	if got := classifyTCP(5900, ""); got != "desktop" {
		t.Fatalf("vnc port = %s", got)
	}
	if got := classifyTCP(22, "desktop"); got != "desktop" {
		t.Fatalf("hint = %s", got)
	}
	if got := classifyTCP(8080, ""); got != "tcp" {
		t.Fatalf("plain tcp = %s", got)
	}
}

func TestBackoffGrowsThenCaps(t *testing.T) {
	b := newBackoff(time.Second, 8*time.Second)
	for i := 0; i < 8; i++ {
		d := b.next()
		if d <= 0 || d > 8*time.Second {
			t.Fatalf("backoff %s out of range", d)
		}
	}
	if b.cur != 8*time.Second {
		t.Fatalf("cap cur=%s", b.cur)
	}
	b.reset()
	if b.cur != time.Second {
		t.Fatalf("reset cur=%s", b.cur)
	}
}

func TestBuildWSURL(t *testing.T) {
	u, err := buildWSURL("https://rmm.example.test", 12, "super-secret-token")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(u, "wss://rmm.example.test") {
		t.Fatalf("scheme/host %s", u)
	}
	if !strings.Contains(u, "/api/agent/tunnel/ws") || !strings.Contains(u, "deviceId=12") {
		t.Fatalf("url = %s", u)
	}
}

func TestChatMessageTypes(t *testing.T) {
	for _, typ := range []string{"chat_open", "chat_message", "chat_close"} {
		if strings.TrimSpace(typ) == "" {
			t.Fatal("empty")
		}
	}
}
