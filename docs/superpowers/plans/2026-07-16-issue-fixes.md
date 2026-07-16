# Issue-Fixes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix every issue surfaced by the final whole-project review — one real correctness bug (CPU double-counts guest), one robustness/security bug (unescaped HTML), one operational footgun (default port 8080), plus a set of smaller cleanups — without changing any behavior the web app depends on beyond these fixes.

**Architecture:** Small, surgical edits across the Go backend (`internal/collect`, `internal/engine`, `internal/server`, `internal/config`) and the vanilla-JS frontend (`web/`), plus deploy hygiene (compose/Dockerfile/.env.example). No new dependencies, no restructuring.

**Tech Stack:** Go 1.23 (CGO_ENABLED=1, NVML/webkit), vanilla JS/HTML/CSS, Docker Compose.

## Global Constraints

- **Never bind host port 8080** — it is permanently owned by the `filebrowser` container. After this plan, no code path (default or compose) may resolve to 8080; the safe default becomes **8090**.
- **Go is not on PATH.** Prefix every Go command: `export PATH="/home/xuanlocserver/.local/go/bin:$PATH"`. `CGO_ENABLED=1` is required to build/test (NVML dlopen + webkit link).
- **Do not run any `docker` command or `make web`** — the production `system-monitor` container is running and must not be disturbed. Fixes take effect only after the user rebuilds/redeploys later.
- **Do not start work on `master`.** Create a feature branch first (e.g. `feat/review-fixes`).
- **CPU semantics (decided):** report honest *total* system CPU like htop — count guest exactly once (it is already folded into `user`/`nice`), and treat `iowait` as **idle/not-busy** (this host is a 12TB storage box with very large iowait; counting it as busy would be misleading). The only CPU change is to stop double-counting guest/guest_nice.
- **Frontend must stay identical for both heads** (web + desktop) — no per-head JS branching.
- The no-auth-on-LAN posture and permissive WS `CheckOrigin` are deliberate and documented — do not "fix" them.
- `ProcIntervalMS` is a documented future-work knob (spec 2026-07-15 design line 257), **not** dead code — keep it.

---

### Task 1: CPU — stop double-counting guest/guest_nice

**Files:**
- Modify: `internal/collect/cpu.go` (the `ParseCPUStat` summation loop)
- Test: `internal/collect/cpu_test.go` (add a guest>0 case)

**Interfaces:**
- Consumes: nothing new.
- Produces: `ParseCPUStat` unchanged signature; `CPUTimes.Total` now excludes guest/guest_nice.

**Background:** `/proc/stat` columns are `user nice system idle iowait irq softirq steal guest guest_nice` (f[1]..f[10]). The kernel already includes `guest` inside `user` and `guest_nice` inside `nice`. Summing all ten counts guest time twice. On this host the aggregate line shows `guest=1,847,967` sitting inside `user=11,525,837`, so CPU% is inflated whenever the VM runs.

- [ ] **Step 1: Add the failing test**

Add to `internal/collect/cpu_test.go`:

```go
func TestParseCPUStatExcludesGuest(t *testing.T) {
	// guest (col 9) and guest_nice (col 10) are already included in user/nice,
	// so they must NOT be added to Total again.
	in := "cpu  100 0 50 800 50 0 0 0 30 5\n"
	agg, _, err := ParseCPUStat(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	// Total = user+nice+system+idle+iowait+irq+softirq+steal = 100+0+50+800+50 = 1000
	// (guest=30, guest_nice=5 excluded); Idle = idle+iowait = 850.
	if agg.Total != 1000 || agg.Idle != 850 {
		t.Fatalf("agg = %+v, want {1000 850}", agg)
	}
}
```

- [ ] **Step 2: Run it — expect FAIL**

Run:
```bash
export PATH="/home/xuanlocserver/.local/go/bin:$PATH"
CGO_ENABLED=1 go test ./internal/collect -run TestParseCPUStatExcludesGuest -v
```
Expected: FAIL — `agg = {Total:1035 Idle:850}, want {1000 850}` (current code adds guest 30 + guest_nice 5).

- [ ] **Step 3: Fix the summation loop**

In `internal/collect/cpu.go`, replace the loop in `ParseCPUStat`:

```go
		var t CPUTimes
		// Sum only user..steal (cols 1-8). Columns 9-10 (guest, guest_nice) are
		// already folded into user/nice by the kernel; adding them again would
		// double-count guest time and inflate CPU%.
		for i := 1; i < len(f) && i <= 8; i++ {
			v, e := strconv.ParseUint(f[i], 10, 64)
			if e != nil {
				continue
			}
			t.Total += v
			if i == 4 || i == 5 { // idle, iowait
				t.Idle += v
			}
		}
```

- [ ] **Step 4: Run the whole collect suite — expect PASS**

Run:
```bash
export PATH="/home/xuanlocserver/.local/go/bin:$PATH"
CGO_ENABLED=1 go test ./internal/collect -v
```
Expected: PASS, including the existing `TestParseCPUStat` (its fixture has guest=0, so Total stays 1000) and the new `TestParseCPUStatExcludesGuest`.

- [ ] **Step 5: Commit**

```bash
git add internal/collect/cpu.go internal/collect/cpu_test.go
git commit -m "fix(cpu): stop double-counting guest/guest_nice in CPU total"
```

---

### Task 2: Frontend — escape untrusted strings + fix chart double-seed and 1×1 canvas

**Files:**
- Modify: `web/app.js` (add `esc()`, apply to name/mount/model; fix `seedHistory`)
- Modify: `web/chart.js` (use `ResizeObserver` so hidden→shown tiles re-size)
- Modify: `web/style.css` (make long lists scroll instead of clipping)

**Interfaces:** none (frontend only). No JS test harness exists; verify by `go build` (the assets are embedded) and by reasoning about the exact edits.

**Background:** `d.model`, `f.mount`, `p.name` come from OS sources a local process or USB device can influence, and are interpolated into `.innerHTML` unescaped — a broken-render bug and a DOM-injection path. Separately, `seedHistory` pushes the newest sample twice into every chart, and a hidden GPU tile can leave its canvas stuck at 1×1.

- [ ] **Step 1: Add an HTML-escape helper**

In `web/app.js`, immediately after the `const css = ...` line (line 2), add:

```js
const esc = s => String(s).replace(/[&<>"']/g, c =>
  ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
```

- [ ] **Step 2: Apply `esc()` to the three unescaped `innerHTML` sites**

In `web/app.js`, the disk-util row (currently lines 60-63) — wrap `d.model` and `d.name`:

```js
  $('diskUtil').innerHTML = s.disk.devs.map(d =>
    `<div class="util-row"><span class="name" title="${esc(d.model || '')}">${esc(d.name)}</span>` +
    `<span class="track"><i style="width:${d.util.toFixed(0)}%"></i></span>` +
    `<span class="n">${d.util.toFixed(0)}%</span></div>`).join('');
```

The FS list (currently lines 81-84) — wrap `f.mount`:

```js
  $('fsList').innerHTML = s.fs.map(f =>
    `<div class="row"><span class="name">${esc(f.mount)}</span>` +
    `<span class="n">${fmtBytes(f.used)}/${fmtBytes(f.total)}</span>` +
    `<span class="n">${f.pct.toFixed(0)}%</span></div>`).join('');
```

The process list (currently lines 86-89) — wrap `p.name`:

```js
  $('procList').innerHTML = s.proc.map(p =>
    `<div class="row"><span class="name">${esc(p.name)}</span>` +
    `<span class="n">${p.cpu.toFixed(0)}%</span>` +
    `<span class="n">${fmtBytes(p.rss)}</span></div>`).join('');
```

- [ ] **Step 3: Fix the `seedHistory` double-push**

In `web/app.js`, the four `.seed(...)` calls (currently lines 101-104) seed the full history including the last element, then `applySnap(history[last])` pushes that last element again. Seed all-but-last so the trailing point is added exactly once by `applySnap`:

```js
  cpuChart.seed(history.slice(0, -1).map(s => s.cpu.cores || []));
  netChart.seed(history.slice(0, -1).map(s => [s.net.rx, s.net.tx]));
  diskChart.seed(history.slice(0, -1).map(s => [s.disk.read, s.disk.write]));
  gpuChart.seed(history.slice(0, -1).map(s => [s.gpu && s.gpu[0] ? s.gpu[0].util : 0]));
  applySnap(history[history.length - 1]);
```

(The `if (!history.length) return;` guard above still covers the empty case; a 1-element history seeds `[]` then applies the single sample once.)

- [ ] **Step 4: Make charts observe their own size (fixes stuck 1×1 canvas)**

In `web/chart.js`, in the constructor, replace the window-resize listener (currently lines 11-12):

```js
    this._resize();
    if (typeof ResizeObserver !== 'undefined') {
      new ResizeObserver(() => this._resize()).observe(canvas);
    } else {
      window.addEventListener('resize', () => this._resize());
    }
```

`ResizeObserver` fires when the canvas's box changes — including when a `display:none` GPU tile becomes visible again — so a hidden-then-shown chart re-sizes correctly instead of staying 1×1.

- [ ] **Step 5: Make long lists scroll inside their tile instead of clipping**

In `web/style.css`, the `.list` rule (line 57) uses `overflow: hidden` with no flex sizing, so a process/filesystem list longer than the tile is silently cut off. Replace it:

```css
.list { flex: 1; min-height: 0; overflow-y: auto; display: flex; flex-direction: column; gap: 2px; }
```

`flex: 1; min-height: 0` lets the list fill the remaining tile height, and `overflow-y: auto` adds an internal scrollbar only when the rows exceed that height (short lists look unchanged).

- [ ] **Step 6: Verify the binary still builds with the embedded assets**

Run:
```bash
export PATH="/home/xuanlocserver/.local/go/bin:$PATH"
CGO_ENABLED=1 go build ./... && echo BUILD OK
```
Expected: `BUILD OK`. (Manual visual check of the running UI happens later, after the user redeploys.)

- [ ] **Step 7: Commit**

```bash
git add web/app.js web/chart.js web/style.css
git commit -m "fix(web): escape untrusted names, de-dup chart seed, observe resize, scroll long lists"
```

---

### Task 3: WS — deliver the init history and first live frame without duplication

**Files:**
- Modify: `internal/engine/engine.go` (add `SubscribeWithHistory`, make `Subscribe` delegate)
- Modify: `internal/server/server.go` (`initMessage` takes history)
- Modify: `internal/server/ws.go` (use the atomic subscribe)
- Test: `internal/engine/engine_test.go` (add a case)

**Interfaces:**
- Produces: `func (e *Engine) SubscribeWithHistory() ([]json.RawMessage, <-chan json.RawMessage, func())`
- `Subscribe()` keeps its existing signature (used by tests), now delegating.

**Background:** `handleWS` calls `Subscribe()` (registers the channel) and *then* `initMessage()` (reads `History()`). A tick landing between the two is both appended to history and pushed to the new channel → the same snapshot is sent twice. Capturing history and registering the subscriber under one lock closes the gap.

- [ ] **Step 1: Add `SubscribeWithHistory` and delegate `Subscribe`**

In `internal/engine/engine.go`, replace the existing `Subscribe` method with:

```go
// SubscribeWithHistory atomically captures the retained history and registers a
// subscriber under one lock, so no tick can land between the two and be
// delivered twice (once in history, once as the first live frame).
func (e *Engine) SubscribeWithHistory() ([]json.RawMessage, <-chan json.RawMessage, func()) {
	ch := make(chan json.RawMessage, 4)
	e.mu.Lock()
	hist := make([]json.RawMessage, len(e.ring))
	copy(hist, e.ring)
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
	return hist, ch, cancel
}

// Subscribe registers a subscriber and returns its channel plus a cancel func.
func (e *Engine) Subscribe() (<-chan json.RawMessage, func()) {
	_, ch, cancel := e.SubscribeWithHistory()
	return ch, cancel
}
```

- [ ] **Step 2: Thread history through `initMessage`**

In `internal/server/server.go`, change `initMessage` to accept the captured history:

```go
func (s *Server) initMessage(history []json.RawMessage) []byte {
	b, _ := json.Marshal(struct {
		Type    string            `json:"type"`
		History []json.RawMessage `json:"history"`
	}{"init", history})
	return b
}
```

- [ ] **Step 3: Use the atomic subscribe in the WS handler**

In `internal/server/ws.go`, replace the subscribe + init block (currently lines 20-25):

```go
	history, sub, cancel := s.eng.SubscribeWithHistory()
	defer cancel()

	if err := conn.WriteMessage(websocket.TextMessage, s.initMessage(history)); err != nil {
		return
	}
```

- [ ] **Step 4: Add an engine test for the atomic contract**

Add `"encoding/json"` to the imports of `internal/engine/engine_test.go` (it currently imports only `testing`, `time`, and the config package), then add:

```go
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
```

(The `nil` collector is fine — this test drives `store`/`broadcast` directly and never starts the sampling loop, exactly like the existing tests.)

- [ ] **Step 5: Run engine + server tests (with -race)**

Run:
```bash
export PATH="/home/xuanlocserver/.local/go/bin:$PATH"
CGO_ENABLED=1 go test ./internal/engine ./internal/server -race -count=1 -v
```
Expected: PASS, no race. Existing `Subscribe`-based tests still pass (delegation preserved).

- [ ] **Step 6: Commit**

```bash
git add internal/engine/engine.go internal/engine/engine_test.go internal/server/server.go internal/server/ws.go
git commit -m "fix(ws): atomically capture history+subscription to avoid duplicate first frame"
```

---

### Task 4: Embed only the real UI assets (stop serving `/fs.go`)

**Files:**
- Modify: `web/fs.go`

**Background:** `//go:embed all:*` embeds every file in `web/`, including `fs.go` itself, so `GET /fs.go` returns Go source. List the actual assets instead.

- [ ] **Step 1: Replace the embed directive**

In `web/fs.go`, replace `//go:embed all:*` with an explicit list:

```go
//go:embed index.html style.css app.js chart.js
var FS embed.FS
```

(Update the surrounding comment: it no longer needs the "recursively, including future subdirectories" note. If a future subdirectory of assets is added, extend this list.)

- [ ] **Step 2: Verify the assets still serve and `/fs.go` no longer exists**

Run (loopback high port only — never 8080, never docker):
```bash
export PATH="/home/xuanlocserver/.local/go/bin:$PATH"
CGO_ENABLED=1 go build ./... && echo BUILD OK
```
Expected: `BUILD OK`. The embed compiles only when all four names exist, so a successful build confirms the asset list is correct. (`fs.go` is now excluded, so it is no longer reachable.)

- [ ] **Step 3: Commit**

```bash
git add web/fs.go
git commit -m "fix(web): embed only UI assets, not fs.go source"
```

---

### Task 5: Port hardening — default to 8090 everywhere, add `.env.example`

**Files:**
- Modify: `internal/config/config.go` (`WebDefaults().Port`)
- Modify: `internal/config/config_test.go` (assertion)
- Modify: `docker-compose.yml` (compose default)
- Modify: `Dockerfile` (`EXPOSE`)
- Modify: `.dockerignore` (exclude `.env`)
- Create: `.env.example`
- Modify: `README.md` (configuration note)

**Background:** `WebDefaults().Port="8080"` + compose `${PORT:-8080}`, with the only 8090 override in the gitignored `.env`. A fresh clone or lost `.env` would bind forbidden 8080 under `network_mode: host`. Make 8090 the default so no path resolves to 8080.

- [ ] **Step 1: Change the web default port**

In `internal/config/config.go`, in `WebDefaults()`:

```go
func WebDefaults() Defaults {
	return Defaults{Port: "8090", HostProc: "/host/proc", HostSys: "/host/sys", HostRoot: "/host/root"}
}
```

- [ ] **Step 2: Update the config test assertion**

In `internal/config/config_test.go`, change the `WebDefaults` port expectation from `"8080"` to `"8090"` (find the assertion comparing the web default port and update the literal). Leave every other assertion unchanged.

- [ ] **Step 3: Change the compose default**

In `docker-compose.yml` line 10:

```yaml
      - PORT=${PORT:-8090}
```

- [ ] **Step 4: Update the Dockerfile EXPOSE**

In `Dockerfile`, change `EXPOSE 8080` to `EXPOSE 8090` (cosmetic under `network_mode: host`, but keeps it consistent).

- [ ] **Step 5: Exclude `.env` from the Docker build context**

Append `.env` to `.dockerignore` (new line):

```
.env
```

- [ ] **Step 6: Create `.env.example`**

Create `.env.example`:

```
# Copy to .env for local deploy overrides (docker compose reads .env automatically).
# Host port 8080 is permanently owned by the filebrowser container; keep this off 8080.
PORT=8090
```

Confirm it is committable: `git check-ignore .env.example` should print nothing. If it is ignored (a `.env*` rule), add `!.env.example` to `.gitignore`.

- [ ] **Step 7: Add a configuration note to the README**

In `README.md`, under the Web app section, add:

```markdown
## Configuration

Copy `.env.example` to `.env` and set `PORT` (defaults to **8090**; do not use
8080 — it belongs to the filebrowser container). Docker Compose reads `.env`
automatically. All other settings have safe defaults.
```

- [ ] **Step 8: Verify build + config tests**

Run:
```bash
export PATH="/home/xuanlocserver/.local/go/bin:$PATH"
CGO_ENABLED=1 go test ./internal/config -v && CGO_ENABLED=1 go build ./... && echo OK
```
Expected: config tests PASS, `OK`.

- [ ] **Step 9: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go docker-compose.yml Dockerfile .dockerignore .env.example README.md
git commit -m "fix(deploy): default port 8090 everywhere; add .env.example"
```

---

### Task 6: Small cleanups — page size from stdlib, clarifying comments

**Files:**
- Modify: `internal/collect/proc.go` (page size)
- Modify: `internal/config/config.go` (comment on `ProcIntervalMS`)
- Modify: `cmd/desktop/main.go` (comment on unused `cfg.Port`)

**Background:** RSS uses a hardcoded 4096 page size (correct on x86_64, wrong on 16K/64K-page ARM). `os.Getpagesize()` (stdlib, no cgo) is correct on any arch and matches the running kernel for both heads. Two config values are correct-but-confusing and only need a comment.

- [ ] **Step 1: Use the real page size for RSS**

In `internal/collect/proc.go`, replace the constant (line 10):

```go
// pageSize is the kernel page size; RSS is reported in pages. Read from the
// running kernel (same kernel for the container and desktop heads).
var pageSize = uint64(os.Getpagesize())
```

(`os` is already imported. `rss * pageSize` stays valid — both `uint64`.)

- [ ] **Step 2: Clarify `ProcIntervalMS` is reserved, not dead**

In `internal/config/config.go`, annotate the struct field:

```go
	ProcIntervalMS int // reserved: optional slower cadence for process scans (not yet wired)
```

- [ ] **Step 3: Note the desktop head ignores `cfg.Port`**

In `cmd/desktop/main.go`, on the line that loads config, add a trailing comment (the desktop head always binds a random loopback port, so `cfg.Port` is intentionally unused):

```go
	cfg := config.Load(config.DesktopDefaults()) // Port unused: desktop binds 127.0.0.1:0
```

- [ ] **Step 4: Build + full suite**

Run:
```bash
export PATH="/home/xuanlocserver/.local/go/bin:$PATH"
CGO_ENABLED=1 go build ./... && CGO_ENABLED=1 go test ./... && echo ALL GREEN
```
Expected: `ALL GREEN`.

- [ ] **Step 5: Commit**

```bash
git add internal/collect/proc.go internal/config/config.go cmd/desktop/main.go
git commit -m "chore: page size from os.Getpagesize; clarify reserved/ignored config"
```

---

## Deliberately NOT changing

- **`iowait` stays counted as idle** (htop convention) — decided; honest for this storage host.
- **No-auth on LAN / permissive `CheckOrigin`** — deliberate, documented.
- **`ProcIntervalMS` kept** — documented future-work knob, not dead code.
- **USER_HZ=100 assumption in `collector.go`** — universal on Linux; making it dynamic needs cgo `sysconf(_SC_CLK_TCK)`, not worth it. Existing comment already documents it.
- **fs %-used uses Bfree** — a defensible definition (matches `used` bytes); left as-is.
- **`ws://` (not `wss://`) in app.js** — correct for an HTTP-only LAN tool and the desktop loopback server; `wss` requires TLS this tool deliberately does not run, so switching would break both heads.
- **Core-cell grid (`repeat(16,1fr)`, fixed height) and "stale core cell"** — `coreCells` is rebuilt once in `initCharts` and the core count is constant per boot, so the per-tick `forEach` never leaves a stale cell; the grid is production-tuned and visually verified. No functional defect, so the tuned CSS is left untouched.
- **No Docker rebuild/redeploy** — code-only; the user redeploys.

## Post-Implementation Verification (whole branch)

```bash
export PATH="/home/xuanlocserver/.local/go/bin:$PATH"
CGO_ENABLED=1 go test ./... -race -count=1
CGO_ENABLED=1 go vet ./...
CGO_ENABLED=1 go build ./...
```
Expected: all tests pass (incl. new CPU + engine cases), no race, vet clean, build OK. No process bound 8080 at any point.
