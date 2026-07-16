// Package engine samples the collector on a fixed cadence, retains a bounded
// history of snapshots, and fans the latest snapshot out to subscribers. It is
// transport-agnostic: heads (web server, desktop) consume it without knowing
// about HTTP or WebSockets.
package engine

import (
	"encoding/json"
	"sync"
	"time"

	"system-monitor/internal/collect"
	"system-monitor/internal/config"
)

type Engine struct {
	cfg config.Config
	col *collect.Collector

	mu   sync.Mutex
	ring []json.RawMessage // last HistorySec snapshots, oldest first
	last json.RawMessage
	subs map[chan json.RawMessage]struct{}
}

func New(cfg config.Config, col *collect.Collector) *Engine {
	return &Engine{cfg: cfg, col: col, subs: make(map[chan json.RawMessage]struct{})}
}

// Start launches the sampling loop in a background goroutine.
func (e *Engine) Start() { go e.loop() }

func (e *Engine) loop() {
	ticker := time.NewTicker(time.Duration(e.cfg.IntervalMS) * time.Millisecond)
	defer ticker.Stop()
	for now := range ticker.C {
		raw, err := json.Marshal(e.col.Tick(now))
		if err != nil {
			continue
		}
		e.store(raw)
		e.broadcast(raw)
	}
}

func (e *Engine) store(raw json.RawMessage) {
	e.mu.Lock()
	e.last = raw
	e.ring = append(e.ring, raw)
	if len(e.ring) > e.cfg.HistorySec {
		e.ring = e.ring[len(e.ring)-e.cfg.HistorySec:]
	}
	e.mu.Unlock()
}

func (e *Engine) broadcast(raw json.RawMessage) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for ch := range e.subs {
		select {
		case ch <- raw:
		default: // slow subscriber: drop this frame
		}
	}
}

// Subscribe registers a subscriber and returns its channel plus a cancel func.
func (e *Engine) Subscribe() (<-chan json.RawMessage, func()) {
	ch := make(chan json.RawMessage, 4)
	e.mu.Lock()
	e.subs[ch] = struct{}{}
	e.mu.Unlock()
	var once sync.Once
	cancel := func() {
		once.Do(func() {
			e.mu.Lock()
			delete(e.subs, ch)
			close(ch)
			e.mu.Unlock()
		})
	}
	return ch, cancel
}

// History returns a copy of the retained snapshots (oldest first).
func (e *Engine) History() []json.RawMessage {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]json.RawMessage, len(e.ring))
	copy(out, e.ring)
	return out
}

// Last returns the most recent snapshot, or nil if none yet.
func (e *Engine) Last() json.RawMessage {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.last
}
