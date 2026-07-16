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
