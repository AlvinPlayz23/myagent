// Command myagent is a coding agent.
//
// Usage:
//
//	myagent                       enter the interactive TUI (default)
//	myagent tui                   same; explicit
//	myagent -p "prompt"           non-interactive: stream a single reply to stdout
//	myagent sessions              list persisted sessions, newest first
//	myagent auth                  open provider setup
//	myagent serve                 run the WebSocket JSON-RPC server
//
// Flags for print/resume mode: -p / -print, --continue, --resume <path>,
// --resume-id <id>, --provider, --model, --base-url, --effort.
//
// Flags for serve mode: --host, --port, --token, --provider, --model,
// --base-url, --effort.
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
	"time"

	"github.com/AlvinPlayz23/myagent/internal/agent"
	"github.com/AlvinPlayz23/myagent/internal/agent/compaction"
	"github.com/AlvinPlayz23/myagent/internal/auth"
	"github.com/AlvinPlayz23/myagent/internal/config"
	"github.com/AlvinPlayz23/myagent/internal/llm"
	modelcatalog "github.com/AlvinPlayz23/myagent/internal/models"
	"github.com/AlvinPlayz23/myagent/internal/plugin"
	"github.com/AlvinPlayz23/myagent/internal/printmode"
	"github.com/AlvinPlayz23/myagent/internal/session"
	"github.com/AlvinPlayz23/myagent/internal/setup"
	"github.com/AlvinPlayz23/myagent/internal/titlegen"
	"github.com/AlvinPlayz23/myagent/internal/tools"
	"github.com/AlvinPlayz23/myagent/internal/tui"
	"github.com/AlvinPlayz23/myagent/internal/types"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "myagent: "+err.Error())
		os.Exit(1)
	}
}

func run(argv []string) error {
	// Subcommand routing. `sessions` lists persisted sessions; `auth` opens
	// provider setup; `serve` runs the WebSocket server; `tui` forces the
	// interactive UI; `plugin` inspects plugins.json.
	if len(argv) > 0 && argv[0] == "sessions" {
		return runSessions(argv[1:])
	}
	if len(argv) > 0 && argv[0] == "auth" {
		return runAuth(argv[1:])
	}
	if len(argv) > 0 && argv[0] == "plugin" {
		return runPlugin(argv[1:])
	}
	if len(argv) > 0 && argv[0] == "serve" {
		return runServe(argv[1:])
	}
	forceTUI := false
	if len(argv) > 0 && argv[0] == "tui" {
		forceTUI = true
		argv = argv[1:]
	}

	fs := flag.NewFlagSet("myagent", flag.ContinueOnError)
	var (
		printPrompt  string
		doContinue   bool
		resumePath   string
		resumeID     string
		providerFlag string
		modelFlag    string
		baseURLFlag  string
		effortFlag   string
		profileFlag  string
		noPlugins    bool
	)
	fs.StringVar(&printPrompt, "p", "", "run a single prompt non-interactively and print the result")
	fs.StringVar(&printPrompt, "print", "", "run a single prompt non-interactively and print the result")
	fs.BoolVar(&doContinue, "continue", false, "resume the most recent session")
	fs.StringVar(&resumePath, "resume", "", "resume the session at the given .jsonl path")
	fs.StringVar(&resumeID, "resume-id", "", "resume the session with the given id")
	fs.StringVar(&providerFlag, "provider", "", "configured provider name (overrides default_model provider)")
	fs.StringVar(&modelFlag, "model", "", "model id (overrides default_model and MYAGENT_MODEL)")
	fs.StringVar(&baseURLFlag, "base-url", "", "provider base URL (overrides configured endpoint)")
	fs.StringVar(&effortFlag, "effort", "", "reasoning effort: "+llm.EffortList())
	fs.StringVar(&profileFlag, "profile", "", "plugin profile to activate")
	fs.BoolVar(&noPlugins, "no-plugins", false, "disable all plugins")
	if err := fs.Parse(argv); err != nil {
		return err
	}
	effort, err := llm.ParseEffort(effortFlag)
	if err != nil {
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
			return fmt.Errorf("no provider configured: run `myagent` once to complete setup or create $MYAGENT_DIR/config.json")
		}
		if err := refreshModelCatalog(context.Background()); err != nil {
			return err
		}
		var cfg2 *config.Config
		cfg2, err = setup.RunWizard(context.Background())
		if err != nil {
			return err
		}
		// Fall through and resolve the wizard's new configuration below.
		cfg = cfg2
	} else {
		cfg, err = config.Load()
		if err != nil {
			return err
		}
	}

	dir, err := config.Dir()
	if err != nil {
		return err
	}
	authStore, err := auth.Load(dir)
	if err != nil {
		return fmt.Errorf("load auth store: %w", err)
	}
	provider, model, err := cfg.ResolveWithAuth(authStore, providerFlag, modelFlag, baseURLFlag)
	if err != nil {
		return err
	}
	catalog := modelcatalog.New(dir)
	if err := catalog.Load(); err != nil {
		if interactive {
			return fmt.Errorf("load model catalog: %w", err)
		}
		catalog = modelcatalog.New(dir)
	}
	model = catalog.Enrich(model)
	effort, err = llm.NormalizeEffort(model, effort)
	if err != nil {
		return err
	}
	modelID := model.Provider + "/" + model.ID

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

	registry := tools.DefaultRegistry(cwd)
	// Plugins: merge ShellTools (per-session cwd + session id), then apply
	// --profile if pinned. Unknown --profile fails fast (§10.3).
	bundle := plugin.Load(cwd, noPlugins)
	sessID := ""
	if sess != nil {
		sessID = sess.ID()
	}
	for _, st := range plugin.ShellTools(bundle.Tools, cwd, sessID) {
		registry.Add(st)
	}
	basePrompt := agent.BuildSystemPrompt(registry, cwd)
	// Keep the unfiltered base: the interactive TUI applies --profile itself
	// (so /profile reset can restore the full tool set), while print mode
	// uses the filtered registry below.
	baseRegistry := registry
	baseEffort := effort
	systemPrompt := basePrompt
	if profileFlag != "" {
		applied, perr := plugin.Apply(bundle, registry, basePrompt, cwd, profileFlag)
		if perr != nil {
			return perr
		}
		if applied.Registry != nil {
			registry = applied.Registry
			systemPrompt = applied.SystemPrompt
		}
		if applied.Effort != "" {
			effort = applied.Effort
		}
	}
	agentCfg := agent.Config{
		Provider:           provider,
		Model:              model,
		Registry:           registry,
		SystemPrompt:       systemPrompt,
		CompactionSettings: compaction.DefaultSettings,
		Effort:             effort,
	}
	// Stable per-conversation ID for provider session-affinity headers
	// (e.g. x-opencode-session on Zen). Sent only to the Zen host.
	if sess != nil {
		agentCfg.Model.SessionID = sess.ID()
	}

	// Prior conversation (empty for a fresh session).
	var history []types.Message
	if sess != nil {
		history = sess.Messages()
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if interactive {
		if catalog.NeedsRefresh(time.Now()) {
			refreshCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
			_ = catalog.Refresh(refreshCtx, nil)
			cancel()
		}
		model = catalog.Enrich(model)
		effort, err = llm.NormalizeEffort(model, baseEffort)
		if err != nil {
			return err
		}
		agentCfg.Model = model
		// Hand the TUI the unfiltered base config: it applies --profile
		// itself after capturing baseRegistry/basePrompt/baseEffort, so
		// /profile reset restores the full tools instead of a filtered set.
		agentCfg.Registry = baseRegistry
		agentCfg.SystemPrompt = basePrompt
		agentCfg.Effort = effort
		// Enrich returns a fresh copy; restore the session-affinity ID.
		if sess != nil {
			agentCfg.Model.SessionID = sess.ID()
		}
		sess, err = tui.Run(ctx, agentCfg, cfg, authStore, catalog, sess, history, modelID, cwd, bundle, profileFlag)
		if sess != nil {
			defer sess.Close()
		}
		if err != nil && ctx.Err() == nil {
			return err
		}
		fmt.Fprint(os.Stdout, resumeInstructions(sess))
		return nil
	}
	defer sess.Close()
	if bundle.Summary() != "Loaded plugins: none" {
		fmt.Fprintln(os.Stderr, bundle.Summary())
	}
	for _, w := range bundle.Warnings {
		fmt.Fprintln(os.Stderr, "plugin warning: "+w)
	}
	if len(history) == 0 && sess.Title() == "new" {
		titleCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
		if title, titleErr := titlegen.Generate(titleCtx, provider, model, printPrompt); titleErr == nil {
			_ = sess.SetGeneratedTitle(title)
		}
		cancel()
	}
	return printmode.Run(ctx, agentCfg, sess, history, printPrompt, os.Stdout, os.Stderr)
}

// runAuth opens the provider setup wizard independently of first-run state.
func runAuth(argv []string) error {
	if len(argv) > 0 {
		return fmt.Errorf("auth does not accept arguments")
	}
	if err := refreshModelCatalog(context.Background()); err != nil {
		return err
	}
	_, err := setup.RunWizard(context.Background())
	return err
}

// refreshModelCatalog warms the shared cache before the auth flows that need
// built-in provider and model choices. A network failure leaves any cache in
// place and never prevents custom-provider setup.
func refreshModelCatalog(ctx context.Context) error {
	dir, err := config.Dir()
	if err != nil {
		return err
	}
	catalog := modelcatalog.New(dir)
	if err := catalog.Load(); err != nil {
		return fmt.Errorf("load model catalog: %w", err)
	}
	if catalog.NeedsRefresh(time.Now()) {
		refreshCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		_ = catalog.Refresh(refreshCtx, nil)
		cancel()
	}
	return nil
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
		preview := info.Title
		if preview == "" {
			preview = info.Preview
		}
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
