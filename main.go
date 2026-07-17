// Command myagent is a coding agent.
//
// Usage:
//   myagent                       enter the interactive TUI (default)
//   myagent tui                   same; explicit
//   myagent -p "prompt"           non-interactive: stream a single reply to stdout
//   myagent sessions              list persisted sessions, newest first
//
// Flags for print/resume mode: -p / -print, --continue, --resume <path>,
// --resume-id <id>, --model, --base-url.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/myagent/myagent/internal/agent"
	"github.com/myagent/myagent/internal/config"
	"github.com/myagent/myagent/internal/llm"
	"github.com/myagent/myagent/internal/printmode"
	"github.com/myagent/myagent/internal/session"
	"github.com/myagent/myagent/internal/tools"
	"github.com/myagent/myagent/internal/tui"
	"github.com/myagent/myagent/internal/types"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "myagent: "+err.Error())
		os.Exit(1)
	}
}

func run(argv []string) error {
	// Subcommand routing. `sessions` lists persisted sessions; `tui` forces
	// the interactive UI (also the default when no -p prompt is given).
	if len(argv) > 0 && argv[0] == "sessions" {
		return runSessions(argv[1:])
	}
	forceTUI := false
	if len(argv) > 0 && argv[0] == "tui" {
		forceTUI = true
		argv = argv[1:]
	}

	fs := flag.NewFlagSet("myagent", flag.ContinueOnError)
	var (
		printPrompt string
		doContinue  bool
		resumePath  string
		resumeID    string
		modelFlag   string
		baseURLFlag string
	)
	fs.StringVar(&printPrompt, "p", "", "run a single prompt non-interactively and print the result")
	fs.StringVar(&printPrompt, "print", "", "run a single prompt non-interactively and print the result")
	fs.BoolVar(&doContinue, "continue", false, "resume the most recent session")
	fs.StringVar(&resumePath, "resume", "", "resume the session at the given .jsonl path")
	fs.StringVar(&resumeID, "resume-id", "", "resume the session with the given id")
	fs.StringVar(&modelFlag, "model", "", "model id (overrides config and MYAGENT_MODEL)")
	fs.StringVar(&baseURLFlag, "base-url", "", "OpenAI-compatible base URL (overrides config)")
	if err := fs.Parse(argv); err != nil {
		return err
	}

	// A trailing positional argument is also accepted as the prompt.
	if printPrompt == "" && fs.NArg() > 0 {
		printPrompt = strings.Join(fs.Args(), " ")
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if modelFlag != "" {
		cfg.Model = modelFlag
	}
	if baseURLFlag != "" {
		cfg.BaseURL = baseURLFlag
	}

	interactive := forceTUI || printPrompt == ""
	if cfg.APIKey == "" {
		return fmt.Errorf("no API key: set %s (or apiKey in config.json)", config.EnvAPIKey)
	}

	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	// Session: resume, continue, or create.
	var sess *session.Session
	switch {
	case resumeID != "":
		sess, err = session.ResumeByID(resumeID)
	case resumePath != "":
		sess, err = session.Open(resumePath)
	case doContinue:
		recent, rerr := session.MostRecent()
		if rerr != nil {
			return rerr
		}
		if recent == "" {
			sess, err = session.Create(cwd)
		} else {
			sess, err = session.Open(recent)
		}
	default:
		sess, err = session.Create(cwd)
	}
	if err != nil {
		return err
	}
	defer sess.Close()

	registry := tools.DefaultRegistry(cwd)
	provider := llm.NewOpenAIProvider(cfg.APIKey)
	agentCfg := agent.Config{
		Provider:     provider,
		Model:        llm.Model{ID: cfg.Model, Provider: "openai", BaseURL: cfg.BaseURL},
		Registry:     registry,
		SystemPrompt: agent.BuildSystemPrompt(registry, cwd),
	}

	// Prior conversation (empty for a fresh session).
	var history []types.Message
	if sess != nil {
		history = sess.Messages()
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if interactive {
		return tui.Run(ctx, agentCfg, sess, history, cfg.Model, cwd)
	}
	return printmode.Run(ctx, agentCfg, sess, history, printPrompt, os.Stdout, os.Stderr)
}

// runSessions implements the `myagent sessions` subcommand: it prints the
// persisted sessions, newest first.
func runSessions(argv []string) error {
	_ = argv // no flags yet
	infos, err := session.List()
	if err != nil {
		return err
	}
	if len(infos) == 0 {
		fmt.Println("No sessions found.")
		return nil
	}
	fmt.Printf("%-36s  %5s  %-19s  %s\n", "ID", "MSGS", "MODIFIED", "PREVIEW")
	for _, info := range infos {
		preview := info.Preview
		if preview == "" {
			preview = "(no messages)"
		}
		fmt.Printf("%-36s  %5d  %-19s  %s\n",
			info.ID,
			info.MessageCount,
			info.Modified.Local().Format("2006-01-02 15:04:05"),
			preview,
		)
	}
	return nil
}
