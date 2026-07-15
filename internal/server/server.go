package server

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"system-monitor/internal/collect"
	"system-monitor/internal/config"
	"system-monitor/internal/model"
	webassets "system-monitor/web"
)

type Server struct {
	cfg  config.Config
	col  *collect.Collector
	hub  *Hub
	mu   sync.Mutex
	ring []json.RawMessage // last HistorySec snapshots
	last json.RawMessage
}

func New(cfg config.Config, c *collect.Collector) *Server {
	return &Server{cfg: cfg, col: c, hub: NewHub()}
}

func (s *Server) Run() error {
	go s.loop()

	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.FS(webassets.FS)))
	mux.HandleFunc("/ws", s.handleWS)
	mux.HandleFunc("/api/snapshot", s.handleSnapshot)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })

	return http.ListenAndServe(":"+s.cfg.Port, mux)
}

func (s *Server) loop() {
	ticker := time.NewTicker(time.Duration(s.cfg.IntervalMS) * time.Millisecond)
	defer ticker.Stop()
	for now := range ticker.C {
		snap := s.col.Tick(now)
		raw, err := json.Marshal(snap)
		if err != nil {
			continue
		}
		s.store(raw)
		s.hub.Broadcast(s.wrap("tick", raw))
	}
}

func (s *Server) store(raw json.RawMessage) {
	s.mu.Lock()
	s.last = raw
	s.ring = append(s.ring, raw)
	if len(s.ring) > s.cfg.HistorySec {
		s.ring = s.ring[len(s.ring)-s.cfg.HistorySec:]
	}
	s.mu.Unlock()
}

func (s *Server) wrap(kind string, snap json.RawMessage) []byte {
	b, _ := json.Marshal(struct {
		Type string          `json:"type"`
		Snap json.RawMessage `json:"snap"`
	}{kind, snap})
	return b
}

func (s *Server) initMessage() []byte {
	s.mu.Lock()
	hist := make([]json.RawMessage, len(s.ring))
	copy(hist, s.ring)
	s.mu.Unlock()
	b, _ := json.Marshal(struct {
		Type    string            `json:"type"`
		History []json.RawMessage `json:"history"`
	}{"init", hist})
	return b
}

func (s *Server) handleSnapshot(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	last := s.last
	s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if last == nil {
		w.Write([]byte(`{}`))
		return
	}
	w.Write(last)
}

var _ = model.Snapshot{} // keep model import if unused elsewhere
