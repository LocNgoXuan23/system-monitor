package server

import (
	"encoding/json"
	"net"
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

// Run binds and serves on the given TCP address, e.g. ":8090".
func (s *Server) Run(addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	return s.Serve(ln)
}

// Serve serves HTTP on an already-bound listener. The desktop head passes a
// loopback listener bound to 127.0.0.1:0.
func (s *Server) Serve(ln net.Listener) error {
	return http.Serve(ln, s.mux())
}

func (s *Server) wrapTick(snap json.RawMessage) []byte {
	b, _ := json.Marshal(struct {
		Type string          `json:"type"`
		Snap json.RawMessage `json:"snap"`
	}{"tick", snap})
	return b
}

func (s *Server) initMessage(history []json.RawMessage) []byte {
	b, _ := json.Marshal(struct {
		Type    string            `json:"type"`
		History []json.RawMessage `json:"history"`
	}{"init", history})
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
