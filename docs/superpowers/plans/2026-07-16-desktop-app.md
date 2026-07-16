# Desktop + Web System Monitor Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a native Linux desktop form factor to the existing system monitor, sharing one engine and one UI with the current Docker web app.

**Architecture:** Extract the transport-agnostic sampling/broadcast logic into `internal/engine`. The `internal/server` package becomes a thin HTTP+WebSocket transport driven by the engine. Two head binaries sit on top: `cmd/web` (Docker, public port) and `cmd/desktop` (native WebKitGTK window pointed at a loopback instance of the same server, so the frontend is byte-for-byte identical).

**Tech Stack:** Go 1.23, CGO, GTK3 + WebKitGTK 4.1 (native window), gorilla/websocket, NVIDIA/go-nvml. No new Go module dependencies.

## Global Constraints

- Platform is **Linux-only** (GNOME/X11). No Windows/macOS code paths.
- Go module path is exactly `system-monitor`.
- On this host `go` is not on PATH; every Go command must be preceded by `export PATH="/home/xuanlocserver/.local/go/bin:$PATH"`.
- `CGO_ENABLED=1` is required for any build/test that pulls in `internal/collect` (NVML) or `internal/desktop` (WebKitGTK).
- Native window uses `gtk+-3.0` and `webkit2gtk-4.1` (both installed; verified via pkg-config).
- Desktop reads native host paths `/proc`, `/sys`, and real `/` (HostRoot `""`). Web reads `/host/proc`, `/host/sys`, `/host/root`. This is configuration only — **no collector code changes**.
- The web app is unchanged in behavior: `network_mode: host`, no-auth-on-LAN (`CheckOrigin` stays permissive), all collectors and their math untouched, deployed on port 8090 via the local `.env`.
- **Never bind host port 8080** (owned by the `filebrowser` container). The desktop binds `127.0.0.1:0` (loopback, random free port). The web head keeps its configured port.
- The `web/` frontend stays **byte-for-byte identical** — no per-head JavaScript branching. Both heads serve it over the same `/` + `/ws` endpoints.
- Autostart is installed/removed only via explicit CLI flags, never silently. Closing the window quits the app.
- DRY, YAGNI, TDD, frequent commits.

---

## File Structure

**Create:**
- `internal/desktop/window.go` — CGO WebKitGTK window primitive (`RunWindow`).
- `internal/desktop/window_smoke_test.go` — env-gated GUI smoke test.
- `internal/desktop/geometry.go` — window-size load/save (`~/.config/system-monitor/window.json`).
- `internal/desktop/geometry_test.go`
- `internal/desktop/autostart.go` — autostart `.desktop` install/remove.
- `internal/desktop/autostart_test.go`
- `internal/engine/engine.go` — ticker + history ring + broadcast fan-out.
- `internal/engine/engine_test.go`
- `internal/server/server_test.go` — `Serve(listener)` HTTP test.
- `cmd/desktop/main.go` — desktop head wiring (flags, loopback server, window).
- `Makefile` — web/desktop/dev/test/install targets.
- `packaging/system-monitor.desktop` — app-menu launcher template.
- `README.md` — how to run each form factor.

**Modify:**
- `internal/server/server.go` — slim to transport; add `Serve(net.Listener)`.
- `internal/server/ws.go` — subscribe via engine instead of hub.
- `internal/config/config.go` — per-head defaults via `Load(Defaults)`.
- `internal/config/config_test.go` — adapt to new signature; add desktop-defaults test.
- `cmd/monitor/main.go` → renamed to `cmd/web/main.go`; rewire to engine + `WebDefaults`.
- `Dockerfile` — build `./cmd/web`.

**Delete:**
- `internal/server/hub.go`, `internal/server/hub_test.go` (logic moves into engine).

---

## Task 1: Desktop window primitive (WebKitGTK) — RISK GATE

This is the only real unknown: that a native WebKitGTK-4.1 window builds and runs via CGO on this host. Do it first so we fail fast. If `pkg-config webkit2gtk-4.1` linking or the C is wrong, everything downstream is blocked.

**Files:**
- Create: `internal/desktop/window.go`
- Test: `internal/desktop/window_smoke_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `type WindowConfig struct { Title, URL string; Width, Height, AutoCloseMS int; OnClose func(w, h int) }`
  - `func RunWindow(cfg WindowConfig)` — opens the window, blocks until closed, calls `OnClose(finalWidth, finalHeight)` as the window closes. `AutoCloseMS > 0` auto-closes after N ms (testing only).

- [ ] **Step 1: Write the window primitive**

Create `internal/desktop/window.go`:

```go
// Package desktop provides the native Linux (GTK3 + WebKitGTK) shell for the
// system monitor desktop app. It is Linux-only and requires CGO.
package desktop

/*
#cgo pkg-config: gtk+-3.0 webkit2gtk-4.1
#include <gtk/gtk.h>
#include <webkit2/webkit2.h>
#include <stdlib.h>

extern void goWindowClosed(int width, int height);

// Capture the current size before the window is destroyed, then let the
// default handler proceed to "destroy".
static gboolean on_delete(GtkWidget *widget, GdkEvent *event, gpointer data) {
    int w = 0, h = 0;
    gtk_window_get_size(GTK_WINDOW(widget), &w, &h);
    goWindowClosed(w, h);
    return FALSE;
}

static void on_destroy(GtkWidget *widget, gpointer data) {
    gtk_main_quit();
}

// One-shot timer used only by tests to auto-close the window.
static gboolean auto_close(gpointer window) {
    gtk_window_close(GTK_WINDOW(window));
    return G_SOURCE_REMOVE;
}

static void run_window(const char *title, const char *url, int width, int height, int autoclose_ms) {
    gtk_init(0, NULL);
    GtkWidget *window = gtk_window_new(GTK_WINDOW_TOPLEVEL);
    gtk_window_set_title(GTK_WINDOW(window), title);
    gtk_window_set_default_size(GTK_WINDOW(window), width, height);
    g_signal_connect(window, "delete-event", G_CALLBACK(on_delete), NULL);
    g_signal_connect(window, "destroy", G_CALLBACK(on_destroy), NULL);

    GtkWidget *webview = webkit_web_view_new();
    gtk_container_add(GTK_CONTAINER(window), webview);
    webkit_web_view_load_uri(WEBKIT_WEB_VIEW(webview), url);

    if (autoclose_ms > 0) {
        g_timeout_add(autoclose_ms, auto_close, window);
    }
    gtk_widget_show_all(window);
    gtk_main();
}
*/
import "C"

import (
	"runtime"
	"unsafe"
)

// WindowConfig configures the native window.
type WindowConfig struct {
	Title       string
	URL         string
	Width       int
	Height      int
	AutoCloseMS int            // >0 auto-closes after N ms (testing only)
	OnClose     func(w, h int) // called with the final window size as it closes
}

// activeOnClose is the callback for the single active window. The app runs one
// window at a time, so a package-level handle is sufficient.
var activeOnClose func(w, h int)

//export goWindowClosed
func goWindowClosed(width, height C.int) {
	if activeOnClose != nil {
		activeOnClose(int(width), int(height))
	}
}

// RunWindow opens the native window and blocks until it is closed. All GTK
// calls must stay on one OS thread, so the calling goroutine is locked to it.
func RunWindow(cfg WindowConfig) {
	runtime.LockOSThread()
	activeOnClose = cfg.OnClose
	ctitle := C.CString(cfg.Title)
	curl := C.CString(cfg.URL)
	defer C.free(unsafe.Pointer(ctitle))
	defer C.free(unsafe.Pointer(curl))
	C.run_window(ctitle, curl, C.int(cfg.Width), C.int(cfg.Height), C.int(cfg.AutoCloseMS))
}
```

- [ ] **Step 2: Verify it builds (this is the gate)**

Run:
```bash
export PATH="/home/xuanlocserver/.local/go/bin:$PATH"
CGO_ENABLED=1 go build ./internal/desktop
```
Expected: exits 0 with no output. This proves `webkit2gtk-4.1` + `gtk+-3.0` link via CGO. If it fails on a missing `.pc` or header, stop and report — the whole approach depends on this.

- [ ] **Step 3: Write the env-gated smoke test**

Create `internal/desktop/window_smoke_test.go`:

```go
package desktop

import (
	"os"
	"testing"
)

// TestRunWindowSmoke opens a real window briefly. It needs a display, so it is
// skipped unless RUN_GUI_SMOKE is set. Run it manually with:
//   RUN_GUI_SMOKE=1 CGO_ENABLED=1 go test ./internal/desktop -run Smoke -v
func TestRunWindowSmoke(t *testing.T) {
	if os.Getenv("RUN_GUI_SMOKE") == "" {
		t.Skip("set RUN_GUI_SMOKE=1 (needs a display) to run the webview smoke test")
	}
	var gotW, gotH int
	RunWindow(WindowConfig{
		Title:       "smoke",
		URL:         "data:text/html,<h1>smoke</h1>",
		Width:       800,
		Height:      600,
		AutoCloseMS: 700,
		OnClose:     func(w, h int) { gotW, gotH = w, h },
	})
	if gotW <= 0 || gotH <= 0 {
		t.Fatalf("OnClose got size %dx%d, want positive dimensions", gotW, gotH)
	}
}
```

- [ ] **Step 4: Verify the smoke test is skipped in the default suite**

Run:
```bash
export PATH="/home/xuanlocserver/.local/go/bin:$PATH"
CGO_ENABLED=1 go test ./internal/desktop -run Smoke -v
```
Expected: `--- SKIP: TestRunWindowSmoke` and `ok`.

- [ ] **Step 5: Verify the smoke test actually opens a window (manual, needs display)**

Run:
```bash
export PATH="/home/xuanlocserver/.local/go/bin:$PATH"
RUN_GUI_SMOKE=1 CGO_ENABLED=1 go test ./internal/desktop -run Smoke -v
```
Expected: a small window flashes for ~0.7s showing "smoke", then `--- PASS: TestRunWindowSmoke` and `ok`. (If run headless, this hangs/fails — that is expected; it requires the user's X11 session.)

- [ ] **Step 6: Commit**

```bash
git add internal/desktop/window.go internal/desktop/window_smoke_test.go
git commit -m "feat(desktop): native WebKitGTK window primitive"
```

---

## Task 2: Extract engine, slim server to transport

Move the ticker + history ring + broadcast out of `server.go`/`hub.go` into `internal/engine`. Rewire `server` and the web `main` to use it. The web app must build and behave identically at the end.

Note: the slimmed `server` no longer holds a `config.Config` (it needs nothing from it), so `Run` takes an explicit `addr string` instead of reading `cfg.Port`. This is a deliberate, self-contained improvement over the spec's literal "Run keeps its current signature" — both callers are ours (`cmd/web` passes `":"+cfg.Port`, `cmd/desktop` uses `Serve` directly), and it keeps the transport free of configuration concerns.

**Files:**
- Create: `internal/engine/engine.go`, `internal/engine/engine_test.go`
- Modify: `internal/server/server.go`, `internal/server/ws.go`, `cmd/monitor/main.go`
- Delete: `internal/server/hub.go`, `internal/server/hub_test.go`

**Interfaces:**
- Consumes: `collect.New(cfg, gpu) *collect.Collector`, `(*collect.Collector).Tick(time.Time) model.Snapshot`, `config.Config` (fields `IntervalMS`, `HistorySec`).
- Produces:
  - `engine.New(cfg config.Config, col *collect.Collector) *engine.Engine`
  - `(*Engine).Start()` — launches the sampling loop goroutine.
  - `(*Engine).Subscribe() (<-chan json.RawMessage, func())` — returns a raw-snapshot channel and a cancel func.
  - `(*Engine).History() []json.RawMessage`
  - `(*Engine).Last() json.RawMessage`
  - `server.New(eng *engine.Engine) *server.Server`
  - `(*Server).Run(addr string) error`

- [ ] **Step 1: Write the engine test**

Create `internal/engine/engine_test.go`:

```go
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
```

- [ ] **Step 2: Run it to verify it fails**

Run:
```bash
export PATH="/home/xuanlocserver/.local/go/bin:$PATH"
CGO_ENABLED=1 go test ./internal/engine
```
Expected: FAIL — package `engine` does not exist yet.

- [ ] **Step 3: Write the engine**

Create `internal/engine/engine.go`:

```go
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
```

- [ ] **Step 4: Run the engine test to verify it passes**

Run:
```bash
export PATH="/home/xuanlocserver/.local/go/bin:$PATH"
CGO_ENABLED=1 go test ./internal/engine
```
Expected: `ok  system-monitor/internal/engine`.

- [ ] **Step 5: Delete the hub and rewrite the server transport**

Delete the old hub:
```bash
git rm internal/server/hub.go internal/server/hub_test.go
```

Replace `internal/server/server.go` with:

```go
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
```

Replace `internal/server/ws.go` with:

```go
package server

import (
	"net/http"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true }, // internal tool, no auth
}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	sub, cancel := s.eng.Subscribe()
	defer cancel()

	if err := conn.WriteMessage(websocket.TextMessage, s.initMessage()); err != nil {
		return
	}

	// Reader goroutine: detect disconnect and drain control frames.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	for {
		select {
		case raw, ok := <-sub:
			if !ok {
				return
			}
			if err := conn.WriteMessage(websocket.TextMessage, s.wrapTick(raw)); err != nil {
				return
			}
		case <-done:
			return
		}
	}
}
```

- [ ] **Step 6: Rewire the web main to use the engine**

Replace `cmd/monitor/main.go` with:

```go
package main

import (
	"log"

	"system-monitor/internal/collect"
	"system-monitor/internal/config"
	"system-monitor/internal/engine"
	"system-monitor/internal/server"
)

func main() {
	cfg := config.Load()
	gpu := collect.NewGPUReader()
	defer gpu.Close()

	col := collect.New(cfg, gpu)
	eng := engine.New(cfg, col)
	eng.Start()
	srv := server.New(eng)

	log.Printf("system-monitor listening on :%s (interval=%dms)", cfg.Port, cfg.IntervalMS)
	log.Fatal(srv.Run(":" + cfg.Port))
}
```

- [ ] **Step 7: Build and test the whole tree**

Run:
```bash
export PATH="/home/xuanlocserver/.local/go/bin:$PATH"
CGO_ENABLED=1 go build ./... && CGO_ENABLED=1 go test ./...
```
Expected: build succeeds; all packages pass (`internal/collect`, `internal/config`, `internal/engine`, `internal/server`, `internal/desktop` skipped-smoke).

- [ ] **Step 8: Commit**

```bash
git add internal/engine cmd/monitor/main.go internal/server/server.go internal/server/ws.go
git commit -m "refactor: extract transport-agnostic engine; slim server to HTTP/WS"
```

---

## Task 3: Add `Serve(net.Listener)` seam to the server

The desktop head binds a loopback listener itself and needs the server to serve on it. Add `Serve(ln)`, delegate `Run` through it, and test it end-to-end over a real loopback socket.

**Files:**
- Modify: `internal/server/server.go`
- Test: `internal/server/server_test.go`

**Interfaces:**
- Consumes: `server.New(eng)`, `engine.New(cfg, col)`.
- Produces: `(*Server).Serve(ln net.Listener) error`.

- [ ] **Step 1: Write the Serve test**

Create `internal/server/server_test.go`:

```go
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
```

- [ ] **Step 2: Run it to verify it fails**

Run:
```bash
export PATH="/home/xuanlocserver/.local/go/bin:$PATH"
CGO_ENABLED=1 go test ./internal/server -run Serve
```
Expected: FAIL — `srv.Serve` undefined.

- [ ] **Step 3: Add Serve and route Run through it**

In `internal/server/server.go`, add the `net` import and replace the `Run` method:

```go
import (
	"encoding/json"
	"net"
	"net/http"

	"system-monitor/internal/engine"
	webassets "system-monitor/web"
)
```

```go
// Run binds and serves on the given TCP address, e.g. ":8080".
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
```

- [ ] **Step 4: Run the test to verify it passes**

Run:
```bash
export PATH="/home/xuanlocserver/.local/go/bin:$PATH"
CGO_ENABLED=1 go test ./internal/server
```
Expected: `ok  system-monitor/internal/server`.

- [ ] **Step 5: Commit**

```bash
git add internal/server/server.go internal/server/server_test.go
git commit -m "feat(server): Serve(net.Listener) seam for loopback desktop head"
```

---

## Task 4: Per-head config defaults

The web head defaults to `/host/*`; the desktop head defaults to native `/proc`, `/sys`, `/`. Make `Load` take a defaults struct so each head supplies its own, while env vars still override everything.

**Files:**
- Modify: `internal/config/config.go`, `cmd/monitor/main.go`
- Test: `internal/config/config_test.go`

**Interfaces:**
- Produces:
  - `type Defaults struct { Port, HostProc, HostSys, HostRoot string }`
  - `func WebDefaults() Defaults`
  - `func DesktopDefaults() Defaults`
  - `func Load(d Defaults) Config`
- Consumes: existing `Config` struct and `env`/`envInt` helpers.

- [ ] **Step 1: Update the config tests**

Replace `internal/config/config_test.go` with:

```go
package config

import (
	"os"
	"testing"
)

func TestWebDefaults(t *testing.T) {
	for _, k := range []string{"PORT", "INTERVAL_MS", "HISTORY_SECONDS", "PROC_TOP_N", "HOST_PROC", "HOST_ROOT"} {
		os.Unsetenv(k)
	}
	c := Load(WebDefaults())
	if c.Port != "8080" {
		t.Errorf("Port = %q, want 8080", c.Port)
	}
	if c.IntervalMS != 1000 {
		t.Errorf("IntervalMS = %d, want 1000", c.IntervalMS)
	}
	if c.HostProc != "/host/proc" {
		t.Errorf("HostProc = %q, want /host/proc", c.HostProc)
	}
	if c.HostRoot != "/host/root" {
		t.Errorf("HostRoot = %q, want /host/root", c.HostRoot)
	}
	if len(c.DiskExclude) != 4 {
		t.Errorf("DiskExclude len = %d, want 4", len(c.DiskExclude))
	}
}

func TestDesktopDefaults(t *testing.T) {
	for _, k := range []string{"HOST_PROC", "HOST_SYS", "HOST_ROOT"} {
		os.Unsetenv(k)
	}
	c := Load(DesktopDefaults())
	if c.HostProc != "/proc" {
		t.Errorf("HostProc = %q, want /proc", c.HostProc)
	}
	if c.HostSys != "/sys" {
		t.Errorf("HostSys = %q, want /sys", c.HostSys)
	}
	if c.HostRoot != "" {
		t.Errorf("HostRoot = %q, want empty", c.HostRoot)
	}
}

func TestEnvOverridesDefaults(t *testing.T) {
	t.Setenv("HOST_PROC", "/custom/proc")
	t.Setenv("INTERVAL_MS", "500")
	c := Load(DesktopDefaults())
	if c.HostProc != "/custom/proc" {
		t.Errorf("HostProc = %q, want /custom/proc", c.HostProc)
	}
	if c.IntervalMS != 500 {
		t.Errorf("IntervalMS = %d, want 500", c.IntervalMS)
	}
}

func TestNonPositiveIntervalFallsBackToDefault(t *testing.T) {
	for _, v := range []string{"0", "-5", "abc"} {
		t.Setenv("INTERVAL_MS", v)
		if got := Load(WebDefaults()).IntervalMS; got != 1000 {
			t.Errorf("INTERVAL_MS=%q: IntervalMS = %d, want default 1000", v, got)
		}
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run:
```bash
export PATH="/home/xuanlocserver/.local/go/bin:$PATH"
CGO_ENABLED=1 go test ./internal/config
```
Expected: FAIL — `Load` takes no args; `WebDefaults`/`DesktopDefaults` undefined.

- [ ] **Step 3: Implement per-head defaults**

In `internal/config/config.go`, replace the `Load` function with:

```go
// Defaults holds the per-head default values that differ between the web and
// desktop form factors. Environment variables still override these.
type Defaults struct {
	Port     string
	HostProc string
	HostSys  string
	HostRoot string
}

// WebDefaults are the defaults for the Docker web head (host paths bind-mounted
// under /host).
func WebDefaults() Defaults {
	return Defaults{Port: "8080", HostProc: "/host/proc", HostSys: "/host/sys", HostRoot: "/host/root"}
}

// DesktopDefaults are the defaults for the native desktop head, which reads the
// real host filesystem directly. HostRoot is "" so filesystem mount paths are
// used as-is (filepath.Join("", "/x") == "/x").
func DesktopDefaults() Defaults {
	return Defaults{Port: "0", HostProc: "/proc", HostSys: "/sys", HostRoot: ""}
}

func Load(d Defaults) Config {
	interval := envInt("INTERVAL_MS", 1000)
	return Config{
		Port:           env("PORT", d.Port),
		IntervalMS:     interval,
		HistorySec:     envInt("HISTORY_SECONDS", 60),
		ProcTopN:       envInt("PROC_TOP_N", 8),
		ProcIntervalMS: envInt("PROC_INTERVAL_MS", interval),
		HostProc:       env("HOST_PROC", d.HostProc),
		HostSys:        env("HOST_SYS", d.HostSys),
		HostRoot:       env("HOST_ROOT", d.HostRoot),
		DiskExclude:    strings.Split(env("DISK_EXCLUDE", "loop,ram,zram,dm-"), ","),
	}
}
```

Note: `HostRoot` default `""` means `env("HOST_ROOT", "")` returns `""` when the var is unset, which is the intended "no prefix" behavior.

- [ ] **Step 4: Update the web main to pass WebDefaults**

In `cmd/monitor/main.go`, change the config load line:

```go
	cfg := config.Load(config.WebDefaults())
```

- [ ] **Step 5: Build and test the whole tree**

Run:
```bash
export PATH="/home/xuanlocserver/.local/go/bin:$PATH"
CGO_ENABLED=1 go build ./... && CGO_ENABLED=1 go test ./...
```
Expected: build succeeds; all tests pass.

- [ ] **Step 6: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go cmd/monitor/main.go
git commit -m "feat(config): per-head defaults (web /host/* vs desktop native paths)"
```

---

## Task 5: Rename `cmd/monitor` → `cmd/web`

Make the two heads symmetric under `cmd/`. Pure rename plus the Docker build path.

**Files:**
- Rename: `cmd/monitor/main.go` → `cmd/web/main.go`
- Modify: `Dockerfile`

**Interfaces:** none changed (same `package main`).

- [ ] **Step 1: Rename the directory**

Run:
```bash
git mv cmd/monitor cmd/web
```

- [ ] **Step 2: Update the Dockerfile build path**

In `Dockerfile`, change the build line (line 8) from `./cmd/monitor` to `./cmd/web`:

```dockerfile
RUN CGO_ENABLED=1 GOOS=linux go build -ldflags="-s -w" -o /out/monitor ./cmd/web
```

(The output binary name `/out/monitor` and the `ENTRYPOINT ["/monitor"]` stay the same, so `docker-compose.yml` needs no change.)

- [ ] **Step 3: Verify the web head still builds**

Run:
```bash
export PATH="/home/xuanlocserver/.local/go/bin:$PATH"
CGO_ENABLED=1 go build ./cmd/web && echo OK
```
Expected: `OK`.

- [ ] **Step 4: Commit**

```bash
git add -A cmd Dockerfile
git commit -m "refactor: rename cmd/monitor to cmd/web for symmetric heads"
```

---

## Task 6: Window-size persistence and autostart (pure Go)

Two small desktop-support files with no GUI dependency: save/restore the window size under `~/.config/system-monitor/`, and install/remove an autostart `.desktop` entry. Both are unit-testable with a temp `XDG_CONFIG_HOME`.

**Files:**
- Create: `internal/desktop/geometry.go`, `internal/desktop/geometry_test.go`
- Create: `internal/desktop/autostart.go`, `internal/desktop/autostart_test.go`

**Interfaces:**
- Produces:
  - `type WindowSize struct { Width, Height int }`
  - `func LoadWindowSize() WindowSize` (default 1440×900 when missing/invalid)
  - `func SaveWindowSize(ws WindowSize) error`
  - `func InstallAutostart(execPath string) (string, error)`
  - `func RemoveAutostart() (string, error)`

- [ ] **Step 1: Write the geometry test**

Create `internal/desktop/geometry_test.go`:

```go
package desktop

import "testing"

func TestWindowSizeRoundTrip(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := SaveWindowSize(WindowSize{Width: 1000, Height: 700}); err != nil {
		t.Fatal(err)
	}
	got := LoadWindowSize()
	if got.Width != 1000 || got.Height != 700 {
		t.Errorf("got %+v, want {1000 700}", got)
	}
}

func TestLoadDefaultWhenMissing(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	got := LoadWindowSize()
	if got.Width != defaultWidth || got.Height != defaultHeight {
		t.Errorf("got %+v, want default {%d %d}", got, defaultWidth, defaultHeight)
	}
}

func TestSaveRejectsNonPositive(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := SaveWindowSize(WindowSize{Width: 0, Height: -1}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := LoadWindowSize() // nothing was written; default returned
	if got.Width != defaultWidth {
		t.Errorf("got %+v, want default", got)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run:
```bash
export PATH="/home/xuanlocserver/.local/go/bin:$PATH"
CGO_ENABLED=1 go test ./internal/desktop -run WindowSize
```
Expected: FAIL — `SaveWindowSize`/`LoadWindowSize`/`WindowSize`/`defaultWidth` undefined.

- [ ] **Step 3: Implement geometry**

Create `internal/desktop/geometry.go`:

```go
package desktop

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// WindowSize is the persisted native-window size.
type WindowSize struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

const (
	defaultWidth  = 1440
	defaultHeight = 900
)

func configDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "system-monitor"), nil
}

// LoadWindowSize returns the saved size, or the default if missing or invalid.
func LoadWindowSize() WindowSize {
	def := WindowSize{Width: defaultWidth, Height: defaultHeight}
	dir, err := configDir()
	if err != nil {
		return def
	}
	b, err := os.ReadFile(filepath.Join(dir, "window.json"))
	if err != nil {
		return def
	}
	var ws WindowSize
	if json.Unmarshal(b, &ws) != nil || ws.Width <= 0 || ws.Height <= 0 {
		return def
	}
	return ws
}

// SaveWindowSize persists the size, creating the config dir if needed. A
// non-positive size is ignored (returns nil) so a bad close reading can't
// clobber a good saved value.
func SaveWindowSize(ws WindowSize) error {
	if ws.Width <= 0 || ws.Height <= 0 {
		return nil
	}
	dir, err := configDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	b, _ := json.Marshal(ws)
	return os.WriteFile(filepath.Join(dir, "window.json"), b, 0o644)
}
```

- [ ] **Step 4: Run to verify geometry passes**

Run:
```bash
export PATH="/home/xuanlocserver/.local/go/bin:$PATH"
CGO_ENABLED=1 go test ./internal/desktop -run WindowSize
```
Expected: `ok`.

- [ ] **Step 5: Write the autostart test**

Create `internal/desktop/autostart_test.go`:

```go
package desktop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDesktopEntryContainsExec(t *testing.T) {
	entry := desktopEntry("/home/u/.local/bin/system-monitor-desktop")
	if !strings.Contains(entry, "[Desktop Entry]") {
		t.Error("missing [Desktop Entry] header")
	}
	if !strings.Contains(entry, "Exec=/home/u/.local/bin/system-monitor-desktop") {
		t.Errorf("missing Exec line, got:\n%s", entry)
	}
}

func TestInstallThenRemoveAutostart(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)

	p, err := InstallAutostart("/opt/sm")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(cfg, "autostart", "system-monitor.desktop")
	if p != want {
		t.Errorf("path = %q, want %q", p, want)
	}
	b, err := os.ReadFile(p)
	if err != nil || !strings.Contains(string(b), "Exec=/opt/sm") {
		t.Fatalf("bad autostart file: %v / %q", err, b)
	}

	if _, err := RemoveAutostart(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Error("file should be gone after remove")
	}
	if _, err := RemoveAutostart(); err != nil { // idempotent
		t.Errorf("second remove should be a no-op, got %v", err)
	}
}
```

- [ ] **Step 6: Run to verify it fails**

Run:
```bash
export PATH="/home/xuanlocserver/.local/go/bin:$PATH"
CGO_ENABLED=1 go test ./internal/desktop -run Autostart
```
Expected: FAIL — `desktopEntry`/`InstallAutostart`/`RemoveAutostart` undefined.

- [ ] **Step 7: Implement autostart**

Create `internal/desktop/autostart.go`:

```go
package desktop

import (
	"os"
	"path/filepath"
)

const autostartFilename = "system-monitor.desktop"

func autostartDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "autostart"), nil
}

// desktopEntry renders an autostart .desktop entry launching execPath.
func desktopEntry(execPath string) string {
	return "[Desktop Entry]\n" +
		"Type=Application\n" +
		"Name=System Monitor\n" +
		"Comment=Live system resource monitor\n" +
		"Exec=" + execPath + "\n" +
		"Terminal=false\n" +
		"X-GNOME-Autostart-enabled=true\n"
}

// InstallAutostart writes the autostart entry pointing at execPath and returns
// the file path written.
func InstallAutostart(execPath string) (string, error) {
	dir, err := autostartDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	p := filepath.Join(dir, autostartFilename)
	if err := os.WriteFile(p, []byte(desktopEntry(execPath)), 0o644); err != nil {
		return "", err
	}
	return p, nil
}

// RemoveAutostart deletes the autostart entry if present. It is idempotent.
func RemoveAutostart() (string, error) {
	dir, err := autostartDir()
	if err != nil {
		return "", err
	}
	p := filepath.Join(dir, autostartFilename)
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return "", err
	}
	return p, nil
}
```

- [ ] **Step 8: Run all desktop tests**

Run:
```bash
export PATH="/home/xuanlocserver/.local/go/bin:$PATH"
CGO_ENABLED=1 go test ./internal/desktop
```
Expected: `ok` (geometry + autostart pass; smoke test skipped).

- [ ] **Step 9: Commit**

```bash
git add internal/desktop/geometry.go internal/desktop/geometry_test.go internal/desktop/autostart.go internal/desktop/autostart_test.go
git commit -m "feat(desktop): window-size persistence and flag-driven autostart"
```

---

## Task 7: Desktop head wiring (`cmd/desktop`)

Tie it together: flags for autostart, a loopback server instance of the shared engine, and the native window pointed at it. Restore window size on open, save on close, quit on close.

**Files:**
- Create: `cmd/desktop/main.go`

**Interfaces:**
- Consumes: `config.Load(config.DesktopDefaults())`, `collect.New`, `engine.New`/`Start`, `server.New`/`Serve`, `desktop.LoadWindowSize`/`SaveWindowSize`/`RunWindow`/`WindowConfig`/`InstallAutostart`/`RemoveAutostart`.
- Produces: the `system-monitor-desktop` binary.

- [ ] **Step 1: Write the desktop main**

Create `cmd/desktop/main.go`:

```go
// Command desktop is the native Linux form factor of the system monitor. It
// runs the shared engine + HTTP/WS server on a private loopback port and opens
// a WebKitGTK window pointed at it, so the UI is identical to the web app.
package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"

	"system-monitor/internal/collect"
	"system-monitor/internal/config"
	"system-monitor/internal/desktop"
	"system-monitor/internal/engine"
	"system-monitor/internal/server"
)

func main() {
	install := flag.Bool("install-autostart", false, "install the autostart entry and exit")
	remove := flag.Bool("remove-autostart", false, "remove the autostart entry and exit")
	flag.Parse()

	if *install {
		exe, err := os.Executable()
		if err != nil {
			log.Fatal(err)
		}
		p, err := desktop.InstallAutostart(exe)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println("autostart installed:", p)
		return
	}
	if *remove {
		p, err := desktop.RemoveAutostart()
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println("autostart removed:", p)
		return
	}

	cfg := config.Load(config.DesktopDefaults())
	gpu := collect.NewGPUReader()
	defer gpu.Close()

	col := collect.New(cfg, gpu)
	eng := engine.New(cfg, col)
	eng.Start()

	// Bind a private loopback port and serve the shared UI + WebSocket on it.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatal(err)
	}
	url := "http://" + ln.Addr().String()
	srv := server.New(eng)
	go func() {
		if err := srv.Serve(ln); err != nil {
			log.Println("server stopped:", err)
		}
	}()

	// SM_AUTOCLOSE_MS lets an automated smoke run open and close the window.
	autoclose := 0
	if v := os.Getenv("SM_AUTOCLOSE_MS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			autoclose = n
		}
	}

	ws := desktop.LoadWindowSize()
	log.Printf("system-monitor desktop on %s (interval=%dms)", url, cfg.IntervalMS)
	desktop.RunWindow(desktop.WindowConfig{
		Title:       "System Monitor",
		URL:         url,
		Width:       ws.Width,
		Height:      ws.Height,
		AutoCloseMS: autoclose,
		OnClose: func(w, h int) {
			if err := desktop.SaveWindowSize(desktop.WindowSize{Width: w, Height: h}); err != nil {
				log.Println("save window size:", err)
			}
		},
	})
}
```

- [ ] **Step 2: Build the desktop head**

Run:
```bash
export PATH="/home/xuanlocserver/.local/go/bin:$PATH"
CGO_ENABLED=1 go build -o bin/system-monitor-desktop ./cmd/desktop && echo OK
```
Expected: `OK`, binary at `bin/system-monitor-desktop`.

- [ ] **Step 3: Verify the autostart flags work (no GUI)**

Run:
```bash
export XDG_CONFIG_HOME="$(mktemp -d)"
./bin/system-monitor-desktop --install-autostart
test -f "$XDG_CONFIG_HOME/autostart/system-monitor.desktop" && echo "INSTALL OK"
./bin/system-monitor-desktop --remove-autostart
test ! -f "$XDG_CONFIG_HOME/autostart/system-monitor.desktop" && echo "REMOVE OK"
```
Expected: `autostart installed: ...`, `INSTALL OK`, `autostart removed: ...`, `REMOVE OK`.

- [ ] **Step 4: Full desktop smoke — live window then auto-close (manual, needs display)**

Run:
```bash
export PATH="/home/xuanlocserver/.local/go/bin:$PATH"
export XDG_CONFIG_HOME="$(mktemp -d)"
SM_AUTOCLOSE_MS=2000 ./bin/system-monitor-desktop
echo "exit=$?"
cat "$XDG_CONFIG_HOME/system-monitor/window.json"
```
Expected: a window opens for ~2s showing the **live system monitor UI** (real CPU/mem/disk/GPU of this host, read natively — no Docker), auto-closes, `exit=0`, and `window.json` contains a positive width/height. This exercises the whole path: engine → loopback server → webview → live UI → close → size saved.

- [ ] **Step 5: Commit**

```bash
git add cmd/desktop/main.go
git commit -m "feat(desktop): cmd/desktop head — loopback server + native window"
```

---

## Task 8: Makefile, app-menu launcher, and README

Make both form factors easy to build, run, and switch, and document it.

**Files:**
- Create: `Makefile`, `packaging/system-monitor.desktop`, `README.md`

**Interfaces:** none (tooling/docs).

- [ ] **Step 1: Write the Makefile**

Create `Makefile`:

```makefile
# Go is not on the default PATH on this host; put it there for every target.
GOBIN_PATH ?= /home/xuanlocserver/.local/go/bin
export PATH := $(GOBIN_PATH):$(PATH)
export CGO_ENABLED := 1

GO ?= go
BINDIR := bin
DESKTOP_BIN := $(BINDIR)/system-monitor-desktop
DEV_PORT ?= 8091

.PHONY: web desktop run-desktop dev test install-desktop clean

## web: build and run the Docker web app (reads .env for PORT, default 8090)
web:
	docker compose up -d --build

## desktop: build the native desktop binary
desktop:
	$(GO) build -ldflags="-s -w" -o $(DESKTOP_BIN) ./cmd/desktop

## run-desktop: build and launch the desktop window
run-desktop: desktop
	./$(DESKTOP_BIN)

## dev: run the web head locally on native paths for fast browser UI iteration
dev:
	PORT=$(DEV_PORT) HOST_PROC=/proc HOST_SYS=/sys HOST_ROOT= $(GO) run ./cmd/web

## test: run the full test suite
test:
	$(GO) test ./...

## install-desktop: install the binary and an app-menu launcher into ~/.local
install-desktop: desktop
	install -Dm755 $(DESKTOP_BIN) $(HOME)/.local/bin/system-monitor-desktop
	install -Dm644 packaging/system-monitor.desktop $(HOME)/.local/share/applications/system-monitor.desktop
	@echo "Installed. Launch from the app menu, or run: system-monitor-desktop"
	@echo "Enable start-on-login with: system-monitor-desktop --install-autostart"

## clean: remove build artifacts
clean:
	rm -rf $(BINDIR)
```

- [ ] **Step 2: Write the app-menu launcher template**

Create `packaging/system-monitor.desktop`:

```ini
[Desktop Entry]
Type=Application
Name=System Monitor
Comment=Live system resource monitor
Exec=system-monitor-desktop
Terminal=false
Categories=System;Monitor;
```

(This app-menu launcher relies on `~/.local/bin` being on PATH, which `install-desktop` populates. The autostart entry written by `--install-autostart` uses an absolute path instead.)

- [ ] **Step 3: Write the README**

Create `README.md`:

```markdown
# System Monitor

A lightweight live system resource monitor (CPU, memory, network, per-disk
%util, GPU via NVML, filesystems, top processes) with one shared UI available
as two form factors:

- **Web app** — runs in Docker, reachable from any device on the LAN.
- **Desktop app** — a native Linux (WebKitGTK) window on the host being monitored.

Both share one engine and one frontend, so the UI is identical.

## Web app (Docker)

```bash
make web            # docker compose up -d --build; serves on the .env PORT (8090)
```

Open `http://<host>:8090`.

## Desktop app (native Linux)

Requires `gtk+-3.0` and `webkit2gtk-4.1` (and CGO). Build and run:

```bash
make run-desktop            # build + open the window
make install-desktop        # install to ~/.local (binary + app-menu launcher)
system-monitor-desktop --install-autostart   # start on login
system-monitor-desktop --remove-autostart    # stop starting on login
```

The desktop app reads the host's `/proc`, `/sys`, and `/` directly (no Docker),
serves the UI on a private loopback port, and shows it in a native window. Close
the window to quit. Window size is remembered under
`~/.config/system-monitor/window.json`.

## Development

```bash
make dev            # run the web head locally on :8091 with native paths,
                    # for fast browser-based iteration on web/
make test           # full test suite
```

Editing anything in `web/` updates both form factors.
```

- [ ] **Step 4: Verify the tooling builds and tests pass**

Run:
```bash
export PATH="/home/xuanlocserver/.local/go/bin:$PATH"
make desktop && test -x bin/system-monitor-desktop && echo "BUILD OK"
make test
```
Expected: `BUILD OK`; all tests pass.

- [ ] **Step 5: Commit**

```bash
git add Makefile packaging/system-monitor.desktop README.md
git commit -m "chore: Makefile, app-menu launcher, and README for dual form factors"
```

---

## Post-Implementation Verification (whole branch)

After all tasks, confirm both form factors from one tree:

1. Web unchanged: `make web`, then `curl -s localhost:8090/healthz` → `ok`, UI loads at `http://localhost:8090` with live metrics. No other container disturbed; port 8080 (filebrowser) untouched.
2. Desktop: `make run-desktop` opens a native window with the same UI showing live native metrics; resize, close, reopen → size restored.
3. `CGO_ENABLED=1 go test ./...` all green; `go vet ./...` clean.
