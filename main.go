// Command myagent is a coding agent.
//
// Usage:
//
//	myagent                       enter the interactive TUI (default)
//	myagent tui                   same; explicit
//	myagent -p "prompt"           non-interactive: stream a single reply to stdout
//	myagent sessions              list persisted sessions, newest first
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
	"path/filepath"
	"strings"
	"syscall"

	"github.com/myagent/myagent/internal/agent"
	"github.com/myagent/myagent/internal/agent/compaction"
	"github.com/myagent/myagent/internal/config"
	"github.com/myagent/myagent/internal/llm"
	"github.com/myagent/myagent/internal/printmode"
	"github.com/myagent/myagent/internal/session"
	"github.com/myagent/myagent/internal/setup"
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

	interactive := forceTUI || printPrompt == ""
	var cfg *config.Config

	// First-run setup: if config.json is missing or blank, walk the user through
	// an interactive wizard before doing anything that needs credentials. The
	// wizard writes config.json so subsequent runs skip straight to the TUI.
	// Non-interactive print mode refuses to run without setup and points the
	// user at the wizard instead of silently launching a UI they can't use.
	needsSetup, err := config.NeedsSetup()
	if err != nil {
		return err
	}
	if needsSetup {
		if !interactive {
			return fmt.Errorf("no API key configured: run `myagent` once to complete setup, or set %s / create $MYAGENT_DIR/config.json", config.EnvAPIKey)
		}
		var cfg2 *config.Config
		cfg2, err = setup.RunWizard(context.Background())
		if err != nil {
			return err
		}
		// Fall through and use cfg2; the wizard leaves env overrides already
		// applied via config.Load().
		cfg = cfg2
	} else {
		cfg, err = config.Load()
		if err != nil {
			return err
		}
	}

	// Per-invocation flags override config + env after setup resolves.
	if modelFlag != "" {
		cfg.Model = modelFlag
	}
	if baseURLFlag != "" {
		cfg.BaseURL = baseURLFlag
	}

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
		Provider:           provider,
		Model:              llm.Model{ID: cfg.Model, Provider: "openai", BaseURL: cfg.BaseURL},
		Registry:           registry,
		SystemPrompt:       agent.BuildSystemPrompt(registry, cwd),
		CompactionSettings: compaction.DefaultSettings,
	}

	// Prior conversation (empty for a fresh session).
	var history []types.Message
	if sess != nil {
		history = sess.Messages()
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if interactive {
		err := tui.Run(ctx, agentCfg, sess, history, cfg.Model, cwd)
		if err != nil && ctx.Err() == nil {
			return err
		}
		fmt.Fprint(os.Stdout, resumeInstructions(sess))
		return nil
	}
	return printmode.Run(ctx, agentCfg, sess, history, printPrompt, os.Stdout, os.Stderr)
}

// resumeInstructions returns the commands needed to continue a persisted
// interactive session after the TUI restores the user's terminal.
func resumeInstructions(sess *session.Session) string {
	return fmt.Sprintf("\nResume this session:\n  myagent --resume-id %s\n  myagent --resume %s\n", sess.ID(), collapseHomePath(sess.Path()))
}

// collapseHomePath replaces the home-directory prefix with ~ when path is
// inside it, keeping paths outside the home directory unambiguous.
func collapseHomePath(path string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	rel, err := filepath.Rel(home, path)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return path
	}
	return "~" + string(filepath.Separator) + rel
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
