package engine

import (
	"testing"
	"time"

	"system-monitor/internal/config"
)

func TestBroadcastReachesSubscriber(t *testing.T) {
	e := New(config.Config{HistorySec: 60}, nil)
	ch, cancel := e.Subscribe()
	defer cancel()

	e.broadcast([]byte("hello"))
	select {
	case msg := <-ch:
		if string(msg) != "hello" {
			t.Errorf("got %q, want hello", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("no message received")
	}
}

func TestCancelStopsDelivery(t *testing.T) {
	e := New(config.Config{HistorySec: 60}, nil)
	ch, cancel := e.Subscribe()
	cancel()
	if _, ok := <-ch; ok {
		t.Fatal("channel should be closed after cancel")
	}
	e.broadcast([]byte("x")) // must not panic with no subscribers
}

func TestHistoryCapsAtHistorySec(t *testing.T) {
	e := New(config.Config{HistorySec: 3}, nil)
	for _, s := range []string{"a", "b", "c", "d", "e"} {
		e.store([]byte(s))
	}
	h := e.History()
	if len(h) != 3 {
		t.Fatalf("history len = %d, want 3", len(h))
	}
	if string(h[0]) != "c" || string(h[2]) != "e" {
		t.Errorf("history = %q, want [c d e]", h)
	}
	if string(e.Last()) != "e" {
		t.Errorf("last = %q, want e", e.Last())
	}
}
