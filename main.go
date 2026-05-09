package main

import (
	"cct/config"
	"cct/limiter"
	"cct/proxy"
	"cct/sync"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
)

func main() {
	configPath := flag.String("config", "config.yaml", "Path to config file")
	port := flag.Int("port", 0, "Port to listen on (overrides config)")
	syncTool := flag.String("sync", "", "Sync configuration to specified tool (e.g., 'claude')")
	flag.Parse()

	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	if *port != 0 {
		cfg.Server.Port = *port
	}

	if *syncTool == "claude" {
		if err := sync.SyncClaudeConfig(cfg); err != nil {
			log.Fatalf("Failed to sync Claude config: %v", err)
		}
		log.Println("Successfully synced configuration to Claude Code")
		os.Exit(0)
	}

	// Initialize components
	l := limiter.NewManager(cfg.Providers)
	p := proxy.NewProxy(cfg, l)

	// Register protocols
	if proto, ok := cfg.Protocols["anthropic-messages"]; ok && proto.Enabled {
		http.HandleFunc("/v1/messages", p.HandleMessages)
		log.Printf("Protocol enabled: anthropic-messages -> /v1/messages")
	}

	// Add other protocols here as needed, e.g., openai-chat
	if proto, ok := cfg.Protocols["openai-chat"]; ok && proto.Enabled {
		http.HandleFunc("/v1/chat/completions", p.HandleOpenAIChat)
		log.Printf("Protocol enabled: openai-chat -> /v1/chat/completions")
	}

	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	log.Printf("Starting CCT proxy on %s", addr)

	// Register models list
	http.HandleFunc("/v1/models", p.HandleListModels)

	log.Fatal(http.ListenAndServe(addr, nil))
}

