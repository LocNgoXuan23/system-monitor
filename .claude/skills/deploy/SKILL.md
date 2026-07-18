---
name: deploy
description: Use when a change to the system-monitor app needs to take effect — after editing Go code, web/ assets, or packaging and it is time to ship. Keywords deploy, redeploy, ship it, apply changes, make it live. system-monitor-service repo only.
---

# Deploy (system-monitor)

## Overview
The user runs the **desktop app** as their real app. The **web head** (`:8090`,
or `:8091` via `make dev`) exists ONLY for Claude to debug the UI with the
browser-use skill. So a deploy means: make the change live in the desktop app,
and clean up the debug web head.

**Deploy = rebuild + reinstall the desktop binary → restart its window → stop the web head.**

## When to use
- After ANY change to the app (`cmd/`, `internal/`, `web/` assets, `packaging/`)
  when you want it live in the desktop app.
- When the user says "deploy", "redeploy", "ship it", "apply the changes".
- NOT while you are still debugging a change in the browser — deploy stops the web head.

## How to run
One idempotent command — run it whether or not the web/desktop are currently up:

```bash
bash .claude/skills/deploy/deploy.sh
```

It runs, in this order (build first, so a broken build never kills the running app):
1. `make desktop` — build the binary to `bin/`. **Aborts here if the build fails**, leaving the running app untouched.
2. Read the live `DISPLAY`/X-session env from the running window, then stop it.
3. `make install-desktop` — install binary + icon + launcher into `~/.local`.
4. Relaunch the desktop window detached (survives this shell); verify its pid.
5. Stop the web head on `:8090` and `:8091`, and `docker compose down` any web container.

Read the summary it prints at the end: `desktop` should be **running**, both web ports **off**.

## Why the rebuild is mandatory (web/ changes)
`web/` assets are `go:embed`'d into the desktop binary. Editing HTML/CSS/JS alone
changes **nothing** in the desktop app until you rebuild — the restart in step 4
is what makes a `web/` change visible. This is the #1 reason to deploy.

## Hard constraints
- **NEVER bind or kill anything on `:8080`** — filebrowser owns it. The script only ever touches `:8090`/`:8091`.
- `go` is not on the default PATH and CGO is required — the `Makefile` exports both, so always go through `make`, never a bare `go build`.
- Relaunching the window needs a live X11 session (`DISPLAY=:1` on this host).

## If the script fails
- **Build broke** → fix the Go/asset error; the running app is still up, nothing lost. Re-run.
- **Window didn't relaunch** → check `/tmp/sm-desktop.log`, then relaunch manually:
  `setsid env DISPLAY=:1 ~/.local/bin/system-monitor-desktop >/tmp/sm-desktop.log 2>&1 </dev/null &`
- **Port still held** → `ss -ltnp "sport = :8090"` to find the pid, then `kill` it.

## Common mistakes
- **Editing `web/` and expecting the desktop app to change without deploying.** It won't — assets are embedded. Deploy.
- **Using `make web` to serve the UI for debugging.** That is Docker with `network_mode: host` and collides on `:8090`. For browser debugging use `make dev` (:8091) or the native web binary, and let deploy stop it.
- **Killing by `:8080`.** Never — that is filebrowser.
