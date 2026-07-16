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
