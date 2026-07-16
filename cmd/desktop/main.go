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

	cfg := config.Load(config.DesktopDefaults()) // Port unused: desktop binds 127.0.0.1:0 below
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
