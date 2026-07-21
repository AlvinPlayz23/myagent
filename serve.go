package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/myagent/myagent/internal/agent/compaction"
	"github.com/myagent/myagent/internal/auth"
	"github.com/myagent/myagent/internal/config"
	"github.com/myagent/myagent/internal/llm"
	"github.com/myagent/myagent/internal/server/core"
	"github.com/myagent/myagent/internal/server/ws"
)

// serveVersion is reported to clients in the server.hello notification.
const serveVersion = "0.1.0"

// runServe implements the `myagent serve` subcommand: a WebSocket JSON-RPC
// server hosting multiple concurrent agent sessions for external clients
// (desktop apps, scripts).
func runServe(argv []string) error {
	fs := flag.NewFlagSet("myagent serve", flag.ContinueOnError)
	var (
		host         string
		port         int
		token        string
		providerFlag string
		modelFlag    string
		baseURLFlag  string
	)
	fs.StringVar(&host, "host", "127.0.0.1", "listen address")
	fs.IntVar(&port, "port", 8765, "listen port")
	fs.StringVar(&token, "token", "", "shared bearer token (auto-generated when empty)")
	fs.StringVar(&providerFlag, "provider", "", "default provider for new sessions")
	fs.StringVar(&modelFlag, "model", "", "default model id for new sessions")
	fs.StringVar(&baseURLFlag, "base-url", "", "provider base URL (overrides configured endpoint)")
	if err := fs.Parse(argv); err != nil {
		return err
	}

	// Serve mode is non-interactive: refuse to run without provider setup,
	// same policy as print mode.
	needsSetup, err := config.NeedsSetup()
	if err != nil {
		return err
	}
	if needsSetup {
		return fmt.Errorf("no provider configured: run `myagent` once to complete setup or create $MYAGENT_DIR/config.json")
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	dir, err := config.Dir()
	if err != nil {
		return err
	}
	authStore, err := auth.Load(dir)
	if err != nil {
		return fmt.Errorf("load auth store: %w", err)
	}
	// Validate the default resolution now so misconfiguration fails at
	// startup, not on the first session.create.
	if _, _, err := cfg.ResolveWithAuth(authStore, providerFlag, modelFlag, baseURLFlag); err != nil {
		return err
	}

	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	// Refuse to expose the agent beyond loopback without an explicit token:
	// the server executes tools with the local user's privileges.
	loopback := false
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		loopback = true
	} else if host == "localhost" {
		loopback = true
	}
	if !loopback && token == "" {
		return fmt.Errorf("binding to non-loopback %q requires an explicit --token", host)
	}
	if token == "" {
		token = ws.NewToken()
		fmt.Printf("token: %s\n", token)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	manager := core.NewManager(ctx, core.Options{
		Resolve: func(providerName, modelID string) (llm.Provider, llm.Model, error) {
			// Per-session overrides fall back to the serve-level flags, then
			// to the configured default_model.
			if providerName == "" {
				providerName = providerFlag
			}
			if modelID == "" {
				modelID = modelFlag
			}
			return cfg.ResolveWithAuth(authStore, providerName, modelID, baseURLFlag)
		},
		DefaultCwd:         cwd,
		CompactionSettings: compaction.DefaultSettings,
	})
	defer manager.Shutdown()

	addr := fmt.Sprintf("%s:%d", host, port)
	return ws.Serve(ctx, manager, ws.Options{
		Addr:    addr,
		Token:   token,
		Version: serveVersion,
		OnListen: func(addr net.Addr) {
			fmt.Printf("connect: ws://%s/ws?token=%s\n", addr, token)
		},
	})
}
