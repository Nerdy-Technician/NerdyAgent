//go:build linux

package ipc

import (
	"testing"
)

type echoHandler struct{}

func (echoHandler) SendChat(sessionID, text string) error {
	if text == "" {
		return errEmpty()
	}
	return nil
}

func errEmpty() error { return errText("empty") }

type errText string

func (e errText) Error() string { return string(e) }

func TestListenCallRoundTrip(t *testing.T) {
	srv, err := Listen("ipc-secret")
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	srv.SetHandler(echoHandler{})
	resp, err := Call(srv.Path(), "ipc-secret", Request{Op: "ping"})
	if err != nil || !resp.OK {
		t.Fatalf("ping %+v %v", resp, err)
	}
	resp, err = Call(srv.Path(), "wrong", Request{Op: "chat_send", Text: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.OK {
		t.Fatal("expected unauthorized")
	}
	resp, err = Call(srv.Path(), "ipc-secret", Request{Op: "chat_send", Text: "hello"})
	if err != nil || !resp.OK {
		t.Fatalf("chat %+v %v", resp, err)
	}
}
