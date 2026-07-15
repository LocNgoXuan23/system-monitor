package server

import (
	"testing"
	"time"
)

func TestHubBroadcast(t *testing.T) {
	h := NewHub()
	ch := h.Register()
	defer h.Unregister(ch)

	h.Broadcast([]byte("hello"))
	select {
	case msg := <-ch:
		if string(msg) != "hello" {
			t.Errorf("got %q", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("no message received")
	}
}

func TestHubUnregister(t *testing.T) {
	h := NewHub()
	ch := h.Register()
	h.Unregister(ch)
	h.Broadcast([]byte("x")) // must not panic on a closed/removed client
}
