package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/a-digi/coco-observe/agent"
)

func main() {
	configPath := flag.String("config", "agent.yaml", "path to config file (ignored when config is embedded in the binary)")
	flag.Parse()

	// Prefer config embedded in the binary (downloaded from the UI).
	// Fall back to the config file for manual / dev usage.
	cfg, err := agent.LoadEmbeddedConfig()
	if err != nil {
		log.Printf("observe agent: no embedded config (%v), loading from %s", err, *configPath)
		cfg, err = agent.LoadConfig(*configPath)
		if err != nil {
			log.Fatalf("observe agent: config: %v", err)
		}
	} else {
		log.Println("observe agent: using embedded config")
	}

	a, err := agent.New(cfg)
	if err != nil {
		log.Fatalf("observe agent: init: %v", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	log.Printf("observe agent: starting (push interval: %s, aggregator: %s)", cfg.PushInterval, cfg.AggregatorURL)
	a.Run(ctx)
	log.Println("observe agent: stopped")
}
