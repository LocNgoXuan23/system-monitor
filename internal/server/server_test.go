package server

import (
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"system-monitor/internal/config"
	"system-monitor/internal/engine"
)

func TestServeHealthzOnLoopback(t *testing.T) {
	eng := engine.New(config.Config{HistorySec: 60}, nil)
	srv := New(eng)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go srv.Serve(ln)

	url := "http://" + ln.Addr().String() + "/healthz"
	var resp *http.Response
	for i := 0; i < 50; i++ { // wait for the goroutine to start serving
		if resp, err = http.Get(url); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if string(b) != "ok" {
		t.Errorf("healthz body = %q, want ok", b)
	}
}
