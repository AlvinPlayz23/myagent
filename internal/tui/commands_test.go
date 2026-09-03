package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/AlvinPlayz23/myagent/internal/agent"
	"github.com/AlvinPlayz23/myagent/internal/llm"
	modelcatalog "github.com/AlvinPlayz23/myagent/internal/models"
	"github.com/AlvinPlayz23/myagent/internal/session"
	"github.com/AlvinPlayz23/myagent/internal/tui/engine"
	"github.com/AlvinPlayz23/myagent/internal/types"
)

// stubProvider satisfies llm.Provider without touching the network.
type stubProvider struct{}

func (stubProvider) Stream(ctx context.Context, model llm.Model, req llm.Request) (<-chan llm.StreamEvent, error) {
	events := make(chan llm.StreamEvent, 1)
	events <- llm.StreamEvent{
		Type: "error",
		Error: &types.Message{
			Role:         types.RoleAssistant,
			StopReason:   types.StopError,
			ErrorMessage: "stub provider",
		},
	}
	close(events)
	return events, nil
}

// newTestApp builds an app wired the way Run does, without a terminal. Runs
// execute synchronously against a stub provider so tests are deterministic.
func newTestApp(t *testing.T, cfg agent.Config, history []types.Message) *app {
	t.Helper()
	if cfg.Provider == nil {
		cfg.Provider = stubProvider{}
	}
	a := newApp(context.Background(), cfg, history, "model", "")
	a.r.bindApp(a)
	a.r.synchronous = true
	a.onResize()
	return a
}

// sbText renders the scrollback rows to plain text for assertions.
func sbText(sb *scrollback, width int, expanded, showThinking bool) string {
	th := currentTheme()
	var b strings.Builder
	for _, e := range sb.entries {
		if e.kind == sbThinking && !showThinking {
			continue
		}
		for _, row := range e.renderRows(width, expanded, showThinking, th) {
			for _, sp := range row {
				b.WriteString(sp.Text)
			}
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func TestParseSlashCommand(t *testing.T) {
	tests := []struct {
		input string
		kind  commandKind
		arg   string
		want  string
	}{
		{input: "/help", kind: commandHelp},
		{input: "/clear", kind: commandClear},
		{input: "/new", kind: commandNew},
		{input: "/compact", kind: commandCompact},
		{input: "/resume", kind: commandResume},
		{input: "/rename a better title", kind: commandRename, arg: "a better title"},
		{input: "/rename", want: "usage: /rename <title>"},
		{input: "/model", kind: commandModel},
		{input: "/model openrouter/openai/gpt-4.1", kind: commandModel, arg: "openrouter/openai/gpt-4.1"},
		{input: "/effort", kind: commandEffort},
		{input: "/effort xhigh", kind: commandEffort, arg: "xhigh"},
		{input: "/providers", kind: commandProviders},
		{input: "/customize", kind: commandCustomize},
		{input: "/init", kind: commandInit},
		{input: "/thinking", kind: commandThinking},
		{input: "/thinking on", kind: commandThinking, arg: "on"},
		{input: "/thinking off", kind: commandThinking, arg: "off"},
		{input: "/thinking banana", kind: commandThinking, arg: "banana"}, // rejected later by applyShowThinking
		{input: "/unknown", want: "unknown command: /unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := parseSlashCommand(tt.input)
			if tt.want != "" {
				if err == nil || !strings.Contains(err.Error(), tt.want) {
					t.Fatalf("error = %v, want %q", err, tt.want)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.kind != tt.kind || got.arg != tt.arg {
				t.Fatalf("command = %#v, want kind %d arg %q", got, tt.kind, tt.arg)
			}
		})
	}
}

func TestEffortCommandOpensOnCurrentValueAndAppliesSelection(t *testing.T) {
	a := newTestApp(t, agent.Config{Effort: llm.EffortHigh}, nil)

	a.runCommand("/effort")
	if !a.effort.active || a.effort.items[a.effort.sel].effort != llm.EffortHigh {
		t.Fatalf("effort picker = active %v selected %q, want high", a.effort.active, a.effort.items[a.effort.sel].effort)
	}
	a.effort.move(1)
	a.modalKey(engine.Key{Code: "enter"})
	if a.effort.active || a.r.cfg.Effort != llm.EffortXHigh {
		t.Fatalf("applied effort = active %v value %q, want xhigh", a.effort.active, a.r.cfg.Effort)
	}
	if !strings.Contains(a.statusMsg, "xhigh") {
		t.Fatalf("status = %q, want xhigh", a.statusMsg)
	}
}

func TestEffortCommandDirectSetAndDefault(t *testing.T) {
	a := newTestApp(t, agent.Config{}, nil)

	a.runCommand("/effort max")
	if a.r.cfg.Effort != llm.EffortMax || a.effort.active {
		t.Fatalf("direct effort = %q active %v, want max and closed", a.r.cfg.Effort, a.effort.active)
	}
	a.runCommand("/effort default")
	if a.r.cfg.Effort != "" {
		t.Fatalf("default effort = %q, want unspecified", a.r.cfg.Effort)
	}
	if !strings.Contains(a.statusMsg, "provider default") {
		t.Fatalf("status = %q, want provider default", a.statusMsg)
	}
}

func TestEffortCommandInvalidCancelAndBusy(t *testing.T) {
	a := newTestApp(t, agent.Config{Effort: llm.EffortMedium}, nil)

	a.runCommand("/effort extreme")
	if a.r.cfg.Effort != llm.EffortMedium || !strings.Contains(a.statusMsg, "invalid effort") {
		t.Fatalf("invalid effort changed value/status: %q %q", a.r.cfg.Effort, a.statusMsg)
	}
	a.runCommand("/effort")
	a.effort.move(1)
	a.modalKey(engine.Key{Code: "esc"})
	if a.effort.active || a.r.cfg.Effort != llm.EffortMedium {
		t.Fatalf("cancel = active %v effort %q", a.effort.active, a.r.cfg.Effort)
	}
	a.working = true
	a.runCommand("/effort high")
	if a.r.cfg.Effort != llm.EffortMedium || !strings.Contains(a.statusMsg, "Cancel the current run") {
		t.Fatalf("busy effort/status = %q %q", a.r.cfg.Effort, a.statusMsg)
	}
}

func TestModelSwitchNormalizesEffortForNonReasoningModel(t *testing.T) {
	a := newTestApp(t, agent.Config{Effort: llm.EffortHigh}, nil)

	nonReasoning := llm.Model{
		Provider:       "openrouter",
		ID:             "plain",
		ReasoningKnown: true,
		Reasoning:      false,
	}
	a.availableModels = func() []modelcatalog.Model {
		return []modelcatalog.Model{{Provider: "openrouter", ID: "plain"}}
	}
	a.selectModel = func(provider, id string) (llm.Provider, llm.Model, error) {
		return a.r.cfg.Provider, nonReasoning, nil
	}
	a.openModelPicker("openrouter/plain")

	if _, ok := a.models.selected(); ok {
		t.Fatal("model picker remained open after exact match")
	}
	if a.modelID != "openrouter/plain" {
		t.Fatalf("model id = %q, want openrouter/plain", a.modelID)
	}
	if a.r.cfg.Effort != "" {
		t.Fatalf("effort after model switch = %q, want unspecified", a.r.cfg.Effort)
	}
}

func TestModelSwitchKeepsEffortForPermissiveModel(t *testing.T) {
	a := newTestApp(t, agent.Config{Effort: llm.EffortHigh}, nil)

	a.availableModels = func() []modelcatalog.Model {
		return []modelcatalog.Model{{Provider: "local", ID: "unknown-model"}}
	}
	a.selectModel = func(provider, id string) (llm.Provider, llm.Model, error) {
		return a.r.cfg.Provider, llm.Model{Provider: provider, ID: id}, nil
	}
	a.openModelPicker("local/unknown-model")
	if a.r.cfg.Model.ID != "unknown-model" {
		t.Fatalf("model id = %q, want unknown-model", a.r.cfg.Model.ID)
	}
	if a.r.cfg.Effort != llm.EffortHigh {
		t.Fatalf("effort after permissive switch = %q, want high", a.r.cfg.Effort)
	}
}

func TestInitCommandStartsRunWithInitPrompt(t *testing.T) {
	a := newTestApp(t, agent.Config{}, nil)
	var sent types.Message
	a.r.onEvent = func(event types.AgentEvent) error {
		if event.Type == types.EventMessageStart && event.Message != nil && event.Message.Role == types.RoleUser {
			sent = *event.Message
		}
		return nil
	}

	a.runCommand("/init")
	if a.working {
		t.Fatal("synchronous init run did not complete")
	}
	// The agent event carries the full instruction, while scrollback retains
	// the command the user actually typed.
	if !strings.Contains(textOf(sent), "AGENTS.md") {
		t.Fatalf("sent prompt = %#v, want the init prompt", sent)
	}
	rendered := sbText(a.sb, 80, false, true)
	if !strings.Contains(rendered, "/init") {
		t.Fatalf("scrollback does not echo the command: %q", rendered)
	}
	if strings.Contains(rendered, "Analyse this repository") {
		t.Fatalf("scrollback leaked the full init prompt: %q", rendered)
	}
}

func TestWelcomeModelShortcutOpensPicker(t *testing.T) {
	a := newTestApp(t, agent.Config{}, nil)
	a.availableModels = func() []modelcatalog.Model {
		return []modelcatalog.Model{{Provider: "openrouter", ID: "model"}}
	}
	a.selectModel = func(string, string) (llm.Provider, llm.Model, error) {
		return nil, llm.Model{}, nil
	}

	a.welcomeKey(engine.Key{Code: "m", Text: "m"})

	if a.welcome || a.modalKind != modalModels || !a.models.active {
		t.Fatalf("model shortcut state = welcome %v modal %d active %v", a.welcome, a.modalKind, a.models.active)
	}
	if a.prompt.value() != "" {
		t.Fatalf("model shortcut inserted prompt text %q", a.prompt.value())
	}
}

func TestRenameCommandPersistsActiveSessionTitle(t *testing.T) {
	a := newTestApp(t, agent.Config{}, []types.Message{userMessage("prior")})
	var renamed string
	a.renameSession = func(title string) error { renamed = title; return nil }

	a.runCommand("/rename Better session title")
	if renamed != "Better session title" {
		t.Fatalf("renamed = %q", renamed)
	}
	if a.sessionTitle != "Better session title" || !a.hasSessionTitle {
		t.Fatalf("title state = %q/%v", a.sessionTitle, a.hasSessionTitle)
	}
	if len(a.r.history) != 1 {
		t.Fatalf("rename changed history length to %d", len(a.r.history))
	}
}

func TestCustomizeCommandSelectsAndSavesOrb(t *testing.T) {
	a := newTestApp(t, agent.Config{}, nil)
	var saved welcomeStyle
	a.saveWelcomeStyle = func(style welcomeStyle) error {
		saved = style
		return nil
	}

	a.runCommand("/customize")
	if a.modalKind != modalCustomize || customizeRows[a.customizeSel].welcome != welcomeDefault {
		t.Fatal("customize modal did not open on the current default style")
	}
	a.customizeMove(1)
	a.applyCustomizeSelection()

	if a.modalKind != modalNone || a.welcomeSty != welcomeOrb || saved != welcomeOrb {
		t.Fatalf("orb selection = modal %v style %q saved %q", a.modalKind, a.welcomeSty, saved)
	}
	logo := a.welcomeLogoRows(welcomeOrb, 0, a.th)
	found := false
	for _, row := range logo {
		for _, sp := range row {
			if strings.Contains(sp.Text, "●") {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("selected orb is not visible in the welcome logo rows")
	}
}

func TestProvidersCommandEditsConfiguredProviderAndSavesNewKey(t *testing.T) {
	a := newTestApp(t, agent.Config{}, nil)
	providers := []modelcatalog.Provider{
		{ID: "openrouter", Name: "OpenRouter", BaseURL: "https://openrouter.ai/api/v1"},
		{ID: "zenmux", Name: "ZenMux", BaseURL: "https://zenmux.ai/api/v1"},
	}
	a.availableProviders = func() []modelcatalog.Provider { return providers }
	a.providerConfigured = func(id string) bool { return id == "openrouter" }
	a.providerAPIKey = func(id string) string {
		if id == "openrouter" {
			return "old-key"
		}
		return ""
	}
	var savedProvider modelcatalog.Provider
	var savedKey string
	a.configureProvider = func(provider modelcatalog.Provider, key string) error {
		savedProvider, savedKey = provider, key
		return nil
	}

	a.runCommand("/providers")
	if a.modalKind != modalProviders || len(a.providers.items) != 2 {
		t.Fatalf("provider modal = %v items %d", a.modalKind, len(a.providers.items))
	}
	a.openProviderKeyEntry()
	if a.keyFor.ID != "openrouter" || a.modalInput != "old-key" {
		t.Fatal("configured provider should open an editor seeded with its stored key")
	}
	a.modalInput = "replacement-key"
	a.saveProviderKey()
	if savedProvider.ID != "openrouter" || savedKey != "replacement-key" {
		t.Fatalf("edited provider/key = %#v/%q", savedProvider, savedKey)
	}
	a.providers.move(1)
	a.openProviderKeyEntry()
	if a.keyFor.ID != "zenmux" {
		t.Fatalf("key entry provider = %q, want zenmux", a.keyFor.ID)
	}
	a.modalInput = "key-value"
	a.saveProviderKey()
	if savedProvider.ID != "zenmux" || savedKey != "key-value" {
		t.Fatalf("saved provider/key = %#v/%q", savedProvider, savedKey)
	}
	if a.keyFor.ID != "" || a.modalKind != modalProviders {
		t.Fatal("modal should return to the provider list after a successful key save")
	}
}

func TestProvidersCommandDoesNotEditCustomProviderCollision(t *testing.T) {
	a := newTestApp(t, agent.Config{}, nil)
	a.providers.open([]modelcatalog.Provider{{ID: "openrouter", Name: "OpenRouter", BaseURL: "https://openrouter.ai/api/v1"}})
	a.modalKind = modalProviders
	a.providerIsCustom = func(id string) bool { return id == "openrouter" }
	a.providerConfigured = func(string) bool { return false }
	a.providerAPIKey = func(string) string { return "" }

	a.openProviderKeyEntry()
	if a.keyFor.ID != "" || a.modalKind != modalProviders {
		t.Fatal("custom collision opened the built-in key editor")
	}
	if !strings.Contains(a.statusMsg, "managed as a custom provider") {
		t.Fatalf("status = %q", a.statusMsg)
	}
}

func TestProviderKeyPasteStaysInMaskedInput(t *testing.T) {
	a := newTestApp(t, agent.Config{}, nil)
	a.providers.open([]modelcatalog.Provider{{ID: "zenmux", Name: "ZenMux"}})
	a.modalKind = modalProviders
	a.providerConfigured = func(string) bool { return false }
	a.providerAPIKey = func(string) string { return "" }
	a.openProviderKeyEntry()
	a.prompt.setValue("keep this out of the composer")

	a.onKey(engine.Key{Code: "rune", Text: "pasted-api-key"})
	if a.modalInput != "pasted-api-key" {
		t.Fatalf("key input = %q, want pasted API key", a.modalInput)
	}
	if a.prompt.value() != "keep this out of the composer" {
		t.Fatalf("composer changed to %q", a.prompt.value())
	}
}

func TestCommandPickerFilteringAndSelection(t *testing.T) {
	p := newCommandPicker()
	p.sync("/")
	if !p.active || len(p.matched) != len(commandItems) {
		t.Fatalf("picker = active %v, matches %d; want all commands", p.active, len(p.matched))
	}
	p.sync("/cl")
	item, ok := p.selected()
	if !ok || item.name != "/clear" {
		t.Fatalf("selected = %#v, %v; want /clear", item, ok)
	}

	p.sync("/")
	p.move(1)
	item, _ = p.selected()
	if item.name != "/model" {
		t.Fatalf("selected after down = %q, want /model", item.name)
	}
	p.move(-1)
	item, _ = p.selected()
	if item.name != "/help" {
		t.Fatalf("selected after up = %q, want /help", item.name)
	}
}

func TestCommandPickerDismissesUntilInputChanges(t *testing.T) {
	p := newCommandPicker()
	p.sync("/h")
	p.dismiss("/h")
	p.sync("/h")
	if p.active {
		t.Fatal("picker reopened without an input change")
	}
	p.sync("/")
	if !p.active {
		t.Fatal("picker did not reopen after input changed")
	}
	p.sync("")
	if p.active {
		t.Fatal("picker remained active after slash was deleted")
	}
}

func TestCommandPickerCompletesRequiredArgumentCommandWithoutSubmitting(t *testing.T) {
	a := newTestApp(t, agent.Config{}, nil)
	a.prompt.setValue("/ren")
	a.picker.sync(a.prompt.value())

	a.acceptCommand(true)
	if a.working {
		t.Fatal("argument command should not submit")
	}
	if got := a.prompt.value(); got != "/rename " {
		t.Fatalf("input = %q, want %q", got, "/rename ")
	}
	if a.picker.active {
		t.Fatal("picker remained open after accepting a command")
	}
}

func TestCommandPickerSubmitsOptionalArgumentCommand(t *testing.T) {
	a := newTestApp(t, agent.Config{}, nil)
	a.availableModels = func() []modelcatalog.Model {
		return []modelcatalog.Model{{Provider: "local", ID: "test-model"}}
	}
	a.selectModel = func(provider, id string) (llm.Provider, llm.Model, error) {
		return a.r.cfg.Provider, llm.Model{Provider: provider, ID: id}, nil
	}
	a.prompt.setValue("/m")
	a.picker.sync(a.prompt.value())

	a.acceptCommand(true)
	if a.prompt.value() != "" {
		t.Fatalf("input = %q, want cleared", a.prompt.value())
	}
	if a.modalKind != modalModels || !a.models.active {
		t.Fatalf("model picker = kind %v active %v, want open", a.modalKind, a.models.active)
	}
}

func TestThinkingCommandTogglesTranscriptVisibility(t *testing.T) {
	a := newTestApp(t, agent.Config{}, nil)

	// Default is shown; /thinking off hides it.
	if !a.sb.showThinking {
		t.Fatal("thinking should default to shown")
	}
	a.prompt.setValue("/thinking off")
	a.submit(submitFollowUp)
	if a.sb.showThinking {
		t.Fatal("/thinking off did not hide thinking")
	}

	// Bare /thinking toggles back on.
	a.prompt.setValue("/thinking")
	a.submit(submitFollowUp)
	if !a.sb.showThinking {
		t.Fatal("bare /thinking did not re-show thinking")
	}

	// The toggle is retroactive: entries captured while hidden appear.
	a.sb.beginThinking()
	a.sb.appendThinkingDelta("captured while hidden")
	a.sb.endThinking()
	a.sb.setShowThinking(false)
	out := sbText(a.sb, 80, false, false)
	if strings.Contains(out, "captured while hidden") {
		t.Fatal("thinking leaked while hidden")
	}
	a.prompt.setValue("/thinking")
	a.submit(submitFollowUp)
	out = sbText(a.sb, 80, false, true)
	if !strings.Contains(out, "captured while hidden") {
		t.Fatalf("thinking not revealed after toggle:\n%s", out)
	}

	// An invalid argument is rejected without changing state.
	a.prompt.setValue("/thinking banana")
	a.submit(submitFollowUp)
	if !a.sb.showThinking || a.statusMsg != "usage: /thinking [on|off]" {
		t.Fatalf("bad arg: show=%v statusMsg=%q", a.sb.showThinking, a.statusMsg)
	}
}

func TestLocalCommandsDoNotBecomeMessages(t *testing.T) {
	a := newTestApp(t, agent.Config{}, []types.Message{userMessage("prior")})

	a.availableModels = func() []modelcatalog.Model { return []modelcatalog.Model{{Provider: "local", ID: "new-model"}} }
	a.selectModel = func(provider, id string) (llm.Provider, llm.Model, error) {
		return a.r.cfg.Provider, llm.Model{Provider: provider, ID: id}, nil
	}
	a.prompt.setValue("/model local/new-model")
	a.submit(submitFollowUp)
	if a.r.cfg.Model.ID != "new-model" || a.modelID != "local/new-model" {
		t.Fatalf("model ids = %q/%q, want local/new-model", a.r.cfg.Model.ID, a.modelID)
	}
	if len(a.r.history) != 1 {
		t.Fatalf("history length = %d, want unchanged history", len(a.r.history))
	}

	a.prompt.setValue("/clear")
	a.submit(submitFollowUp)
	if len(a.sb.entries) != 0 {
		t.Fatalf("clear left %d scrollback entries", len(a.sb.entries))
	}
	if len(a.r.history) != 1 {
		t.Fatalf("clear changed history length to %d", len(a.r.history))
	}
}

func TestNewCommandResetsConversation(t *testing.T) {
	a := newTestApp(t, agent.Config{}, []types.Message{userMessage("prior")})
	created := false
	a.newSession = func() error {
		created = true
		a.r.reset()
		return nil
	}
	a.sb.addUser("prior")
	a.usage.Input = 10
	a.prompt.setValue("/new")
	a.submit(submitFollowUp)

	if !created {
		t.Fatal("new-session callback was not called")
	}
	if len(a.r.history) != 0 || len(a.sb.entries) != 0 || a.usage.Input != 0 {
		t.Fatalf("/new did not reset conversation: history=%d entries=%d input=%d", len(a.r.history), len(a.sb.entries), a.usage.Input)
	}
}

func TestResumeCommandSelectsAndLoadsSession(t *testing.T) {
	a := newTestApp(t, agent.Config{}, []types.Message{userMessage("current")})
	info := session.Info{ID: "session-2", Modified: time.Now(), Preview: "resumed prompt"}
	a.listSessions = func() ([]session.Info, error) {
		return []session.Info{info}, nil
	}
	resumedHistory := []types.Message{userMessage("resumed prompt")}
	var resumedID string
	a.resumeSession = func(id string) ([]types.Message, error) {
		resumedID = id
		return resumedHistory, nil
	}

	a.runCommand("/resume")
	if a.modalKind != modalSessions || len(a.sessions.items) != 1 {
		t.Fatalf("session modal = %v, items %d", a.modalKind, len(a.sessions.items))
	}
	a.resumeSelected()
	if resumedID != info.ID {
		t.Fatalf("resumed id = %q, want %q", resumedID, info.ID)
	}
	if a.modalKind == modalSessions {
		t.Fatal("session modal remained open")
	}
	if len(a.r.history) != 1 || textOf(a.r.history[0]) != "resumed prompt" {
		t.Fatalf("runner history = %#v, want resumed history", a.r.history)
	}
	if len(a.sb.entries) != 1 || a.sb.entries[0].text != "resumed prompt" {
		t.Fatalf("scrollback was not replaced with resumed history: %#v", a.sb.entries)
	}
}
