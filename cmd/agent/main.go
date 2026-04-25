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
	configPath := flag.String("config", "agent.yaml", "path to agent config file")
	flag.Parse()

	cfg, err := agent.LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("observe agent: config: %v", err)
	}

	a, err := agent.New(cfg)
	if err != nil {
		log.Fatalf("observe agent: init: %v", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	log.Printf("observe agent: starting (push interval: %s)", cfg.PushInterval)
	a.Run(ctx)
	log.Println("observe agent: stopped")
}
