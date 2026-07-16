package engine

import (
	"encoding/json"
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

func TestSubscribeWithHistoryReturnsSnapshot(t *testing.T) {
	e := New(config.Config{HistorySec: 60}, nil)
	e.store(json.RawMessage(`{"n":1}`))
	e.store(json.RawMessage(`{"n":2}`))

	hist, ch, cancel := e.SubscribeWithHistory()
	defer cancel()

	if len(hist) != 2 || string(hist[1]) != `{"n":2}` {
		t.Fatalf("history = %v, want the two stored snapshots", hist)
	}
	e.broadcast(json.RawMessage(`{"n":3}`))
	select {
	case got := <-ch:
		if string(got) != `{"n":3}` {
			t.Fatalf("live frame = %s, want {\"n\":3}", got)
		}
	default:
		t.Fatal("expected the post-subscribe broadcast on the channel")
	}
}

func TestPublishDeliversEachTickExactlyOnce(t *testing.T) {
	e := New(config.Config{HistorySec: 60}, nil)
	e.publish(json.RawMessage(`{"n":1}`)) // before subscribe -> history only

	hist, ch, cancel := e.SubscribeWithHistory()
	defer cancel()

	if len(hist) != 1 || string(hist[0]) != `{"n":1}` {
		t.Fatalf("history = %v, want [{n:1}]", hist)
	}
	// n:1 was published before subscribing, so it must NOT also arrive live.
	select {
	case got := <-ch:
		t.Fatalf("unexpected live frame %s; n:1 should only be in history", got)
	default:
	}

	e.publish(json.RawMessage(`{"n":2}`)) // after subscribe -> live only, not in hist
	select {
	case got := <-ch:
		if string(got) != `{"n":2}` {
			t.Fatalf("live frame = %s, want {\"n\":2}", got)
		}
	default:
		t.Fatal("expected n:2 as a live frame")
	}
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
