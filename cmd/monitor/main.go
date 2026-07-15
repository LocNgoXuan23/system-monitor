package main

import (
	"log"

	"system-monitor/internal/collect"
	"system-monitor/internal/config"
	"system-monitor/internal/server"
)

func main() {
	cfg := config.Load()
	gpu := collect.NewGPUReader()
	defer gpu.Close()

	col := collect.New(cfg, gpu)
	srv := server.New(cfg, col)

	log.Printf("system-monitor listening on :%s (interval=%dms)", cfg.Port, cfg.IntervalMS)
	log.Fatal(srv.Run())
}
