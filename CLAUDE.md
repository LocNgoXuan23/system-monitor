# system_monitor_service

## UI/UX proposals: always mock up in the browser

When proposing **any** UI or UX option — layouts, card anatomy, component variants,
visual hierarchy, spacing, color, chart form, where a piece of data lives — do not
describe the options in prose in the terminal. **Render them as real mockups in the
browser** (Superpowers brainstorming visual companion, `--project-dir` = repo root)
and let the user look at them side by side.

This applies even when the choice feels small enough to explain in a sentence, and
even when the options seem obvious to describe. If the user's answer is a *visual
preference*, they must see it before answering. Text-only UI options are a defect.

The terminal stays correct for genuinely non-visual questions: scope, requirements,
data modeling, tradeoff discussion, technical approach.

## UI copy is English-only

Every string the app renders — labels, headers, card titles, subtitles, empty
states, truncation notes, tooltips, error text — is written in English. This holds
regardless of the language the request was made in; a Vietnamese conversation still
produces English UI copy.

Comments, commit messages, specs and plans are unaffected by this rule.

## Build

`go` is not on the default PATH. Export it and enable CGO (NVML + WebKitGTK need it):

```bash
export PATH=$PATH:/home/xuanlocserver/.local/go/bin
export CGO_ENABLED=1
```

## Form factors

One Go binary, two shells: a web server (`:8090`) and a desktop app (GTK3 + WebKitGTK
pointed at loopback). Both serve the same `web/` assets, so any frontend change must
work in both. Never bind host port 8080 — filebrowser owns it.
