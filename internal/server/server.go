package server

import (
	"encoding/json"
	"net/http"

	"system-monitor/internal/engine"
	webassets "system-monitor/web"
)

type Server struct {
	eng *engine.Engine
}

func New(eng *engine.Engine) *Server {
	return &Server{eng: eng}
}

func (s *Server) mux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.FS(webassets.FS)))
	mux.HandleFunc("/ws", s.handleWS)
	mux.HandleFunc("/api/snapshot", s.handleSnapshot)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	return mux
}

// Run binds and serves on the given TCP address, e.g. ":8080".
func (s *Server) Run(addr string) error {
	return http.ListenAndServe(addr, s.mux())
}

func (s *Server) wrapTick(snap json.RawMessage) []byte {
	b, _ := json.Marshal(struct {
		Type string          `json:"type"`
		Snap json.RawMessage `json:"snap"`
	}{"tick", snap})
	return b
}

func (s *Server) initMessage() []byte {
	b, _ := json.Marshal(struct {
		Type    string            `json:"type"`
		History []json.RawMessage `json:"history"`
	}{"init", s.eng.History()})
	return b
}

func (s *Server) handleSnapshot(w http.ResponseWriter, r *http.Request) {
	last := s.eng.Last()
	w.Header().Set("Content-Type", "application/json")
	if last == nil {
		w.Write([]byte(`{}`))
		return
	}
	w.Write(last)
}
